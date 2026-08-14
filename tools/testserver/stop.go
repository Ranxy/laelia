package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func stopCmd(args []string) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	var workdir string
	fs.StringVar(&workdir, "workdir", "", "work directory (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if workdir == "" {
		fmt.Fprintln(os.Stderr, "error: --workdir is required")
		return 2
	}
	wd, err := filepath.Abs(workdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	m, err := loadMeta(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: no instance metadata in %s: %v\n", wd, err)
		return 1
	}

	// Stop the laelia server process.
	if m.ServerPid > 0 {
		if p, err := os.FindProcess(m.ServerPid); err == nil {
			_ = p.Signal(syscall.SIGTERM)
			waitPid(m.ServerPid, 15*time.Second)
		}
	}

	// Stop the embedded postgres.
	pgCfg := pgConfig{workdir: wd, cacheDir: m.CacheDir, port: m.PGPort, password: m.PGPassword}
	if err := stopPG(pgCfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to stop postgres: %v\n", err)
	} else {
		waitPGDown(pgCfg, 15*time.Second)
	}

	m.Status = "stopped"
	_ = m.save()
	fmt.Printf("stopped instance in %s\n", wd)
	return 0
}

// waitPid waits up to timeout for the process to exit.
func waitPid(pid int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Sending signal 0 checks existence without delivering a signal.
		if err := syscall.Kill(pid, 0); err != nil {
			return // process gone
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
