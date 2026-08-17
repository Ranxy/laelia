// Package supervisor implements the machine's background supervisor process
// (`laelia-machine daemon`). It spawns and watches the business process
// (`laelia-machine run`), serves a loopback control endpoint the business
// process uses to trigger self-upgrades, and performs the download/replace/
// restart dance when the manager asks the machine to upgrade.
package supervisor

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/version"
)

// AddrFile is the file (under the Laelia data root) holding the supervisor's
// loopback listen address. A TCP loopback listener is used instead of a unix
// socket so Windows works without extra dependencies.
var AddrFile = "supervisor.addr"

// UpgradeRequest is the control payload the business process forwards when
// the manager pushes an UpgradeRequest over the MachineChannel.
type UpgradeRequest struct {
	Version    string `json:"version"`
	Target     string `json:"target"`
	Sha256     string `json:"sha256"` // expected sha256 of the gzipped binary
	ManagerURL string `json:"manager_url"`
}

// Status is the live self-upgrade state, polled by the business process and
// reported to the manager as UpgradeProgress.
type Status struct {
	Version string `json:"version"`
	Stage   string `json:"stage"` // "", "downloading", "installing", "restarting", "done", "failed"
	Error   string `json:"error,omitempty"`
}

// Terminal reports whether the upgrade has reached a terminal stage.
func (s Status) Terminal() bool { return s.Stage == "done" || s.Stage == "failed" }

// Supervisor is the background machine supervisor. Exactly one runs per host
// (per LAELIA_HOME); it owns the worker child process and the upgrade flow.
type Supervisor struct {
	exePath    string
	workerArgs []string
	foreground bool
	logPath    string
	daemonArgs []string // full argv (excluding exe) to relaunch the supervisor after an upgrade
	addrPath   string
	httpServer *http.Server
	listener   net.Listener
	workerCmd  *exec.Cmd
	workerDone chan struct{}
	upgrading  bool
	// workerPaused is set while install() deliberately stops the worker to swap
	// binaries. watchWorker must not race the swap by respawning the worker;
	// it waits for this flag to clear (failure paths) or for the process to be
	// replaced/exited (success path).
	workerPaused bool
	status       Status
	mu           sync.Mutex
	stopWatcher  chan struct{}
}

// New creates a supervisor that runs the worker as exePath + workerArgs.
// daemonArgs is the supervisor's own argv used to relaunch a new supervisor
// from the upgraded binary.
func New(exePath string, workerArgs, daemonArgs []string, foreground bool) (*Supervisor, error) {
	logDir := home.Join("logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, errors.Wrap(err, "create log dir")
	}
	return &Supervisor{
		exePath:    exePath,
		workerArgs: workerArgs,
		daemonArgs: daemonArgs,
		foreground: foreground,
		logPath:    filepath.Join(logDir, "daemon.log"),
		addrPath:   home.Join(AddrFile),
	}, nil
}

// Run starts the control endpoint and the worker watch loop. It blocks until
// ctx is cancelled or a self-upgrade replaces this process.
func (s *Supervisor) Run(ctx context.Context) error {
	logFile, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return errors.Wrap(err, "open daemon log")
	}
	defer logFile.Close()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logFile), nil)))

	slog.Info("machine supervisor starting", "version", version.Version, "pid", os.Getpid(), "foreground", s.foreground)

	if err := s.startControlServer(); err != nil {
		return err
	}
	defer s.stopControlServer()

	// Clean any binary left over from a previous upgrade.
	_ = os.Remove(s.exePath + ".old")

	s.stopWatcher = make(chan struct{})
	go s.watchWorker()

	<-ctx.Done()
	slog.Info("machine supervisor stopping")
	s.stopWorker()
	close(s.stopWatcher)
	return nil
}

// ---- control endpoint ----

func (s *Supervisor) startControlServer() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.Wrap(err, "bind supervisor control listener")
	}
	s.listener = listener
	if err := os.WriteFile(s.addrPath, []byte(listener.Addr().String()), 0o600); err != nil {
		_ = listener.Close()
		return errors.Wrap(err, "write supervisor addr file")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/upgrade", s.handleUpgrade)
	mux.HandleFunc("/status", s.handleStatus)
	s.httpServer = &http.Server{Handler: mux}
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("supervisor control server error", "error", err)
		}
	}()
	slog.Info("supervisor control endpoint listening", "addr", listener.Addr().String())
	return nil
}

func (s *Supervisor) stopControlServer() {
	_ = s.httpServer.Close()
	_ = os.Remove(s.addrPath)
}

// closeControlServer stops the HTTP listener but leaves the addr file in
// place. It is used during a self-upgrade: the freshly relaunched supervisor
// has already (or is about to) overwrite the addr file with its own loopback
// address, so removing it here would strand the new worker's progress polling.
func (s *Supervisor) closeControlServer() {
	_ = s.httpServer.Close()
}

func (s *Supervisor) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req UpgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upgrading {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(s.status)
		return
	}
	if s.status.Stage == "downloading" || s.status.Stage == "installing" || s.status.Stage == "restarting" {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(s.status)
		return
	}
	s.upgrading = true
	s.status = Status{Version: req.Version, Stage: "downloading"}
	go s.upgrade(req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.status)
}

func (s *Supervisor) handleStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	st := s.status
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

func (s *Supervisor) setStatus(stage, errMsg string) {
	s.mu.Lock()
	s.status.Stage = stage
	s.status.Error = errMsg
	s.mu.Unlock()
}

func (s *Supervisor) finishUpgrade(stage, errMsg string) {
	s.setStatus(stage, errMsg)
	s.mu.Lock()
	s.upgrading = false
	s.mu.Unlock()
}

// resumeWorker clears the install-time worker pause so watchWorker may respawn
// the business process after a failed upgrade (the old binary has been rolled
// back or was never swapped).
func (s *Supervisor) resumeWorker() {
	s.mu.Lock()
	s.workerPaused = false
	s.mu.Unlock()
}

// ---- worker lifecycle ----

// watchWorker restarts the business process whenever it dies, with backoff.
func (s *Supervisor) watchWorker() {
	backoff := 1 * time.Second
	for {
		select {
		case <-s.stopWatcher:
			return
		default:
		}
		done, err := s.startWorker()
		if err != nil {
			slog.Error("failed to spawn worker", "error", err)
			select {
			case <-s.stopWatcher:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = 1 * time.Second
		select {
		case <-s.stopWatcher:
			<-done
			return
		case <-done:
			slog.Warn("worker exited; restarting")
		}

		// During a self-upgrade install() stops the worker on purpose so it can
		// swap the binaries underneath. Don't respawn until the upgrade either
		// fails (flag cleared) or this process is replaced/exits.
		s.mu.Lock()
		paused := s.workerPaused
		s.mu.Unlock()
		for paused {
			select {
			case <-s.stopWatcher:
				return
			case <-time.After(200 * time.Millisecond):
			}
			s.mu.Lock()
			paused = s.workerPaused
			s.mu.Unlock()
		}
	}
}

func (s *Supervisor) startWorker() (chan struct{}, error) {
	cmd := exec.Command(s.exePath, s.workerArgs...)
	cmd.Env = os.Environ()
	logFile, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		defer logFile.Close()
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	s.mu.Lock()
	s.workerCmd = cmd
	s.workerDone = done
	s.mu.Unlock()
	slog.Info("worker started", "pid", cmd.Process.Pid)
	return done, nil
}

// stopWorker gracefully terminates the worker: SIGTERM, then a hard kill
// after stopTimeout.
func (s *Supervisor) stopWorker() {
	s.mu.Lock()
	cmd := s.workerCmd
	done := s.workerDone
	s.workerCmd = nil
	s.workerDone = nil
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	if done != nil {
		select {
		case <-done:
			return
		case <-time.After(30 * time.Second):
		}
	}
	_ = cmd.Process.Kill()
	if done != nil {
		<-done
	}
}

// ---- upgrade flow ----

// upgrade downloads the new binary from the manager, verifies it against the
// manifest, stops the worker, swaps the binaries, relaunches a new supervisor
// from the upgraded binary, and exits this process.
func (s *Supervisor) upgrade(req UpgradeRequest) {
	slog.Info("self-upgrade started", "from", version.Version, "to", req.Version, "target", req.Target)

	binPath, err := s.download(req)
	if err != nil {
		slog.Error("self-upgrade download failed", "error", err)
		s.finishUpgrade("failed", err.Error())
		return
	}

	s.setStatus("installing", "")
	if err := s.install(binPath); err != nil {
		slog.Error("self-upgrade install failed", "error", err)
		s.resumeWorker()
		s.finishUpgrade("failed", err.Error())
		return
	}

	s.setStatus("restarting", "")
	if err := s.relaunch(); err != nil {
		// Roll back so the next worker spawn still works.
		if rbErr := rollback(s.exePath); rbErr != nil {
			slog.Error("self-upgrade rollback failed", "error", rbErr)
		}
		slog.Error("self-upgrade relaunch failed", "error", err)
		s.resumeWorker()
		s.finishUpgrade("failed", err.Error())
		return
	}

	// The new supervisor takes over from here; this process exits. Keep the
	// addr file: the new supervisor has already overwritten it with its own
	// loopback address, and the new worker polls through it.
	s.finishUpgrade("done", "")
	slog.Info("self-upgrade complete; new supervisor taking over")
	s.closeControlServer()
	s.stopWorker()
	os.Exit(0) //nolint:revive // supervisor is replaced by the upgraded binary; exiting here is intentional
}

// download fetches the gzipped new binary and verifies it against the request
// sha256 (of the gzip) and the manifest's uncompressed sha256. It returns the
// path of the verified, decompressed new binary.
func (s *Supervisor) download(req UpgradeRequest) (binPath string, err error) {
	if req.ManagerURL == "" || req.Target == "" {
		return "", errors.New("upgrade request is missing manager url or target")
	}
	base := strings.TrimRight(req.ManagerURL, "/")

	manifest, err := fetchManifest(base)
	if err != nil {
		return "", errors.Wrap(err, "fetch manifest")
	}
	if req.Version != "" && manifest.Version != req.Version {
		return "", errors.Errorf("manifest version %q does not match requested version %q", manifest.Version, req.Version)
	}
	target, ok := manifest.Targets[req.Target]
	if !ok {
		return "", errors.Errorf("manifest has no target %q", req.Target)
	}

	httpClient := &http.Client{Timeout: 10 * time.Minute}
	resp, err := httpClient.Get(base + "/machine/bin/" + req.Target)
	if err != nil {
		return "", errors.Wrap(err, "download binary")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("download returned %s", resp.Status)
	}

	gzPath := s.exePath + ".download"
	gzFile, err := os.OpenFile(gzPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return "", errors.Wrap(err, "create download file")
	}
	gzHash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(gzFile, gzHash), resp.Body); err != nil {
		_ = gzFile.Close()
		_ = os.Remove(gzPath)
		return "", errors.Wrap(err, "write download")
	}
	_ = gzFile.Close()

	if req.Sha256 != "" {
		if got := hex.EncodeToString(gzHash.Sum(nil)); got != req.Sha256 {
			_ = os.Remove(gzPath)
			return "", errors.Errorf("gzip sha256 mismatch: got %s", got)
		}
	}

	// Decompress and verify the uncompressed binary against the manifest.
	in, err := os.Open(gzPath)
	if err != nil {
		return "", errors.Wrap(err, "reopen download")
	}
	defer in.Close()
	zr, err := gzip.NewReader(in)
	if err != nil {
		_ = os.Remove(gzPath)
		return "", errors.Wrap(err, "gunzip binary")
	}
	defer zr.Close()
	binPath = s.exePath + ".new"
	binFile, err := os.OpenFile(binPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		_ = os.Remove(gzPath)
		return "", errors.Wrap(err, "create binary file")
	}
	binHash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(binFile, binHash), zr); err != nil {
		_ = binFile.Close()
		_ = os.Remove(gzPath)
		_ = os.Remove(binPath)
		return "", errors.Wrap(err, "decompress binary")
	}
	_ = binFile.Close()
	if target.Sha256 != "" {
		if got := hex.EncodeToString(binHash.Sum(nil)); got != target.Sha256 {
			_ = os.Remove(gzPath)
			_ = os.Remove(binPath)
			return "", errors.Errorf("binary sha256 mismatch: got %s", got)
		}
	}
	_ = os.Remove(gzPath)
	return binPath, nil
}

// install stops the worker, then atomically swaps the binaries: the running
// binary is renamed to <exe>.old (a running binary can be renamed but not
// deleted on Windows) and the new one moved into place.
func (s *Supervisor) install(binPath string) error {
	s.mu.Lock()
	s.workerPaused = true
	s.mu.Unlock()
	s.stopWorker()

	old := s.exePath + ".old"
	_ = os.Remove(old)
	if err := os.Rename(s.exePath, old); err != nil {
		return errors.Wrap(err, "move current binary aside")
	}
	if err := os.Rename(binPath, s.exePath); err != nil {
		// Best-effort rollback: put the old binary back.
		_ = rollback(s.exePath)
		return errors.Wrap(err, "move new binary into place")
	}
	return nil
}

// rollback restores <exe>.old to the executable path after a failed install.
func rollback(exePath string) error {
	old := exePath + ".old"
	if _, err := os.Stat(old); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.Rename(old, exePath)
}

// relaunch replaces this supervisor with one from the upgraded binary. In
// foreground mode (container/PID-1) the process image is replaced in place
// via exec so the pid, stdout, and signal handling are preserved; otherwise a
// fresh detached supervisor is spawned and this one exits.
func (s *Supervisor) relaunch() error {
	if s.foreground {
		slog.Info("exec-ing upgraded supervisor in place")
		return execInPlace(s.exePath, append([]string{s.exePath}, s.daemonArgs...), os.Environ())
	}
	cmd := exec.Command(s.exePath, s.daemonArgs...)
	cmd.Env = os.Environ()
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	setDetached(cmd)
	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "start new supervisor")
	}
	slog.Info("new supervisor started", "pid", cmd.Process.Pid)
	return nil
}

type manifest struct {
	Version string `json:"version"`
	Targets map[string]struct {
		File   string `json:"file"`
		Sha256 string `json:"sha256"`
		Gz     struct {
			File   string `json:"file"`
			Sha256 string `json:"sha256"`
		} `json:"gz"`
	} `json:"targets"`
}

func fetchManifest(base string) (*manifest, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Get(base + "/machine/manifest.json")
	if err != nil {
		return nil, errors.Wrap(err, "get manifest")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("manifest returned %s", resp.Status)
	}
	var m manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, errors.Wrap(err, "decode manifest")
	}
	return &m, nil
}

// Detach exposes the platform detach helper for callers spawning the
// supervisor (setup's daemonize path).
func Detach(cmd *exec.Cmd) { setDetached(cmd) }

// ---- client side (used by the business process) ----

// TriggerUpgrade asks the supervisor (via its loopback control endpoint) to
// start a self-upgrade. It returns the initial status.
func TriggerUpgrade(req UpgradeRequest) (*Status, error) {
	addr, err := supervisorAddr()
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(req)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Post("http://"+addr+"/upgrade", "application/json", stringReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "post upgrade to supervisor")
	}
	defer resp.Body.Close()
	var st Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, errors.Wrap(err, "decode upgrade response")
	}
	return &st, nil
}

// PollStatus reads the current upgrade status from the supervisor.
func PollStatus() (*Status, error) {
	addr, err := supervisorAddr()
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get("http://" + addr + "/status")
	if err != nil {
		return nil, errors.Wrap(err, "get supervisor status")
	}
	defer resp.Body.Close()
	var st Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, errors.Wrap(err, "decode status response")
	}
	return &st, nil
}

// supervisorAddr reads the supervisor's loopback address from the addr file.
func supervisorAddr() (string, error) {
	data, err := os.ReadFile(home.Join(AddrFile))
	if err != nil {
		return "", errors.Wrap(err, "supervisor not running (no addr file)")
	}
	addr := string(data)
	if addr == "" {
		return "", errors.New("supervisor addr file is empty")
	}
	return addr, nil
}

func stringReader(b []byte) io.Reader {
	return bytes.NewReader(b)
}
