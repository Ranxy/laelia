package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// pgConfig bundles the paths needed to start/stop the embedded postgres for an
// instance. It is reconstructed from meta on stop.
type pgConfig struct {
	workdir  string
	cacheDir string
	port     int
	password string
}

// dataPath holds the actual postgres data. runtimePath is the scratch dir used
// by embedded-postgres for initdb; it MUST live outside dataPath because
// embedded-postgres removes dataPath (and everything inside it) when it
// re-initializes the data directory.
func (c pgConfig) dataPath() string    { return filepath.Join(c.workdir, "pgdata") }
func (c pgConfig) runtimePath() string { return filepath.Join(c.workdir, "runtime") }
func (c pgConfig) archivePath() string { return filepath.Join(c.cacheDir, "pg", "archive") }
func (c pgConfig) binariesPath() string {
	return filepath.Join(c.cacheDir, "pg", "binaries")
}

func (c pgConfig) newDatabase() *embeddedpostgres.EmbeddedPostgres {
	logFile, err := os.OpenFile(filepath.Join(c.workdir, "logs", "postgres.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logFile = os.Stderr
	}
	return embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(uint32(c.port)).
		Database("laelia").
		Username("laelia").
		Password(c.password).
		CachePath(c.archivePath()).
		BinariesPath(c.binariesPath()).
		RuntimePath(c.runtimePath()).
		DataPath(c.dataPath()).
		StartTimeout(120 * time.Second).
		Logger(logFile))
}

// startPG starts the embedded postgres and returns the connection URL.
func startPG(c pgConfig) (string, error) {
	if err := os.MkdirAll(filepath.Join(c.workdir, "logs"), 0o755); err != nil {
		return "", err
	}
	pg := c.newDatabase()
	if err := pg.Start(); err != nil {
		return "", fmt.Errorf("failed to start embedded postgres: %w", err)
	}
	return fmt.Sprintf("postgresql://laelia:%s@127.0.0.1:%d/laelia", c.password, c.port), nil
}

// stopPG stops the embedded postgres by invoking pg_ctl directly. It cannot
// use embedded-postgres's Stop() because that requires the same instance that
// started the server (a fresh instance reports ErrServerNotStarted and does
// nothing). It is safe to call even if postgres is not running.
func stopPG(c pgConfig) error {
	pgCtl := filepath.Join(c.binariesPath(), "bin", "pg_ctl")
	if _, err := os.Stat(pgCtl); err != nil {
		return fmt.Errorf("pg_ctl not found at %s: %w", pgCtl, err)
	}
	cmd := exec.Command(pgCtl, "stop", "-D", c.dataPath(), "-m", "fast", "-w")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// "no server running" is fine — nothing to stop.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("failed to stop postgres: %v: %s", err, string(out))
	}
	return nil
}

// waitPGDown polls until the postgres port stops accepting connections.
func waitPGDown(c pgConfig, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !portOpen(c.port) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
