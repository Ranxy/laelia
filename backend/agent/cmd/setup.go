package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/agent/client"
	"github.com/Ranxy/laelia/backend/agent/state"
	"github.com/Ranxy/laelia/backend/common/log"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

func init() {
	rootCmd.AddCommand(setupCmd)
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure/validate the machine login, then run in the foreground",
	Long: `Configure or validate this machine's login with the manager, then run
in the foreground.

On first use it starts a device-code login: it prints an approval URL and a
user code, waits for a logged-in user to approve in the browser, and registers
this machine (creating it on the manager if it is not registered yet). On later
runs it validates the saved login and reports "already logged in" before
running. Use --force to wipe the local state and register a brand-new machine.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return setupMachine()
	},
}

// setupMachine is the single entry command: validate or establish the login,
// then run the machine in the foreground.
func setupMachine() error {
	if flags.debug {
		log.LogLevel.Set(slog.LevelDebug)
	}
	log.SetSlog()

	if alreadyRunning() {
		_, _ = fmt.Println("laelia-machine is already running on this computer")
		return nil
	}

	managerURL := strings.TrimRight(flags.managerURL, "/")

	st, err := state.Load()
	if err != nil {
		return errors.New("failed to read local machine state: " + err.Error())
	}
	if flags.force {
		if st != nil {
			fmt.Printf("Local machine state cleared (machine %s stays registered on the manager).\n", st.MachineID)
		}
		if err := state.Clear(); err != nil {
			return errors.New("failed to clear local machine state: " + err.Error())
		}
		st = nil
	}

	if st != nil && st.ManagerURL != managerURL {
		fmt.Printf("This machine is configured for %s; registering a new machine on %s (the old machine stays registered on %s).\n", st.ManagerURL, managerURL, st.ManagerURL)
		st = nil
	}

	if st != nil && st.MachineID != "" && st.RefreshToken != "" {
		switch probeRefreshToken(managerURL, st) {
		case probeOK:
			fmt.Printf("Already logged in as machine %s (%s)\n", st.MachineID, st.Hostname)
			return runMachine()
		case probePermanent:
			_, _ = fmt.Println("The saved login is no longer valid; re-authenticating this machine...")
			// Keep the machine id so the device flow re-authenticates the
			// existing machine instead of registering a duplicate; only the
			// dead credential is dropped (the approval mints a fresh one).
			// `setup --force` is the only way to wipe the machine id and
			// register a brand-new machine.
			st.RefreshToken = ""
			if err := state.Save(st); err != nil {
				return errors.New("failed to persist local machine state: " + err.Error())
			}
		case probeTransient:
			slog.Warn("could not validate the saved login (manager unreachable); starting anyway", "manager", managerURL)
			return runMachine()
		default:
			return errors.New("unexpected refresh-token probe result")
		}
	}

	if err := deviceLogin(managerURL, st); err != nil {
		return err
	}
	return runMachine()
}

type probeResult int

const (
	probeOK probeResult = iota
	probePermanent
	probeTransient
)

// probeRefreshToken validates the saved refresh token against the manager.
// probeOK means the login is still valid (a rolling renewal is persisted);
// probePermanent means the token is dead (revoked/expired/machine deleted) and
// the device flow must re-authenticate; probeTransient means the manager was
// unreachable and the run loop should just retry with backoff.
func probeRefreshToken(managerURL string, st *state.State) probeResult {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	machineClient := v1connect.NewMachineServiceClient(httpClient, managerURL)

	hostname, _ := os.Hostname()
	fingerprint := client.ComputeFingerprint(hostname, runtime.GOOS, runtime.GOARCH)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := machineClient.RefreshMachineToken(ctx, connect.NewRequest(&v1pb.RefreshMachineTokenRequest{
		RefreshToken: st.RefreshToken,
		Fingerprint:  fingerprint,
	}))
	if err != nil {
		var ce *connect.Error
		if errors.As(err, &ce) && (ce.Code() == connect.CodeUnauthenticated || ce.Code() == connect.CodePermissionDenied) {
			return probePermanent
		}
		return probeTransient
	}
	if resp.Msg.RefreshToken != "" {
		st.RefreshToken = resp.Msg.RefreshToken
		if err := state.Save(st); err != nil {
			slog.Warn("failed to persist renewed refresh token", "error", err)
		}
	}
	return probeOK
}

// deviceLogin runs the OAuth2-style device code flow: start a session, print
// the approval URL + user code, poll until a logged-in user approves, then
// persist the minted machine id + refresh token. st carries the existing
// machine id when re-authenticating (nil for a brand-new machine).
func deviceLogin(managerURL string, st *state.State) error {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	deviceClient := v1connect.NewDeviceServiceClient(httpClient, managerURL)

	hostname, _ := os.Hostname()
	osName := runtime.GOOS
	arch := runtime.GOARCH
	fingerprint := client.ComputeFingerprint(hostname, osName, arch)

	machineID := ""
	if st != nil {
		machineID = st.MachineID
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startResp, err := deviceClient.StartDeviceLogin(ctx, connect.NewRequest(&v1pb.StartDeviceLoginRequest{
		Hostname:    hostname,
		Os:          osName,
		Arch:        arch,
		Ip:          client.OutboundIP(),
		Version:     "0.2.0",
		Fingerprint: fingerprint,
		MachineId:   machineID,
	}))
	if err != nil {
		return errors.New("failed to start device login: " + err.Error())
	}

	verificationURL := managerURL + startResp.Msg.VerificationPath
	_, _ = fmt.Println("To approve this machine, open the following URL in a browser and sign in:")
	_, _ = fmt.Println("  " + verificationURL)
	_, _ = fmt.Printf("User code: %s\n", startResp.Msg.UserCode)
	if !flags.noBrowser {
		openBrowser(verificationURL)
	}

	interval := time.Duration(startResp.Msg.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(startResp.Msg.ExpiresIn) * time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		pollResp, err := deviceClient.PollDeviceLogin(ctx, connect.NewRequest(&v1pb.PollDeviceLoginRequest{
			DeviceCode: startResp.Msg.DeviceCode,
		}))
		if err != nil {
			// Transient (network/server) failure: keep polling.
			slog.Debug("device login poll failed", "error", err)
			continue
		}

		switch pollResp.Msg.Status {
		case v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_PENDING:
			remaining := time.Until(deadline)
			if remaining <= 0 {
				_, _ = fmt.Println()
				return errors.New("the login code expired; run `laelia-machine setup` again")
			}
			fmt.Printf("\rWaiting for approval... (%s remaining)", remaining.Round(time.Second))
		case v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_APPROVED:
			_, _ = fmt.Println()
			newState := &state.State{
				ManagerURL:   managerURL,
				MachineID:    pollResp.Msg.MachineId,
				RefreshToken: pollResp.Msg.RefreshToken,
				Hostname:     hostname,
				CreatedAt:    time.Now(),
			}
			if err := state.Save(newState); err != nil {
				return errors.New("failed to save machine state: " + err.Error())
			}
			fmt.Printf("Machine %q registered as %s\n", pollResp.Msg.MachineTitle, pollResp.Msg.MachineId)
			return nil
		case v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_DENIED:
			_, _ = fmt.Println()
			reason := pollResp.Msg.DenialReason
			if reason == "" {
				reason = "the approval was denied"
			}
			return errors.New(reason)
		case v1pb.DeviceLoginStatus_DEVICE_LOGIN_STATUS_EXPIRED:
			_, _ = fmt.Println()
			return errors.New("the login code expired; run `laelia-machine setup` again")
		default:
			_, _ = fmt.Println()
			return errors.New("unexpected device login status")
		}
	}
}

// openBrowser best-effort opens the approval URL in the user's default
// browser. Failures are logged at debug level only: the URL is printed to the
// terminal anyway.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		slog.Debug("failed to open browser", "error", err)
	}
}
