package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// startServer launches the laelia manager binary as a subprocess and waits
// until /healthz responds. It returns the *exec.Cmd so the caller can manage
// its lifecycle.
func startServer(ctx context.Context, binary, pgURL string, port int, logFile io.Writer) (*exec.Cmd, error) {
	cmd := exec.Command(binary, "--port", strconv.Itoa(port), "--debug")
	cmd.Env = append(os.Environ(), "LAELIA_PG_URL="+pgURL)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start laelia server: %w", err)
	}

	// Poll /healthz until the server is ready or the context is cancelled.
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	deadline := time.Now().Add(90 * time.Second)
	for {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return nil, fmt.Errorf("laelia server exited early (code %d)", cmd.ProcessState.ExitCode())
		}
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return cmd, nil
			}
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("laelia server did not become ready within 90s")
		}
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// stopServer sends SIGTERM to the server process and waits for it to exit.
func stopServer(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return nil
	}
}
