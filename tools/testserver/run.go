package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type runOptions struct {
	workdir       string
	repo          string
	port          int
	pgPort        int
	host          string
	noSeed        bool
	build         bool
	keep          bool
	cache         string
	binary        string
	adminEmail    string
	adminPassword string
}

func runCmd(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	opts := &runOptions{}
	fs.StringVar(&opts.workdir, "workdir", "", "work directory (required)")
	fs.StringVar(&opts.repo, "repo", "", "repository root (for the build script)")
	fs.IntVar(&opts.port, "port", 0, "HTTP port (default: random free port)")
	fs.IntVar(&opts.pgPort, "pg-port", 0, "postgres port (default: random free port)")
	fs.StringVar(&opts.host, "host", "127.0.0.1", "bind address for the HTTP server")
	fs.BoolVar(&opts.noSeed, "no-seed", false, "skip seeding test data")
	fs.BoolVar(&opts.build, "build", false, "force rebuild of the laelia binary")
	fs.BoolVar(&opts.keep, "keep", false, "keep postgres data on exit (for debugging)")
	fs.StringVar(&opts.cache, "cache", "", "shared cache dir (default: LAELIA_TEST_CACHE or ~/.cache/laelia-test)")
	fs.StringVar(&opts.binary, "binary", "", "path to the laelia binary (default: <cache>/laelia)")
	fs.StringVar(&opts.adminEmail, "admin-email", "admin@laelia.test", "admin email")
	fs.StringVar(&opts.adminPassword, "admin-password", "admin1234", "admin password")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if opts.workdir == "" {
		fmt.Fprintln(os.Stderr, "error: --workdir is required")
		return 2
	}
	wd, err := filepath.Abs(opts.workdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: bad workdir: %v\n", err)
		return 2
	}
	opts.workdir = wd
	if opts.cache == "" {
		opts.cache = defaultCacheDir()
	}
	if opts.binary == "" {
		opts.binary = defaultBinaryPath(opts.cache)
	}

	if err := os.MkdirAll(filepath.Join(opts.workdir, "logs"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Join(opts.workdir, "run"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Ensure the laelia binary exists (build if requested or missing).
	if opts.build || !fileExists(opts.binary) {
		if err := buildBinary(opts); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}

	// Pick ports.
	if opts.port == 0 {
		opts.port, err = randomFreePort(20000, 40000)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}
	if opts.pgPort == 0 {
		opts.pgPort, err = randomFreePort(41000, 50000)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}

	pgPassword := randomPassword(24)
	pgCfg := pgConfig{workdir: opts.workdir, cacheDir: opts.cache, port: opts.pgPort, password: pgPassword}

	// Start embedded postgres.
	pgURL, err := startPG(pgCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Start the laelia server.
	logFile, err := os.OpenFile(filepath.Join(opts.workdir, "logs", "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverCmd, err := startServer(ctx, opts.binary, pgURL, opts.port, logFile)
	if err != nil {
		_ = stopPG(pgCfg)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Seed test data.
	users := defaultUsers(opts.adminEmail, opts.adminPassword)
	if !opts.noSeed {
		if err := seedUsers(ctx, pgURL, users); err != nil {
			_ = stopServer(serverCmd)
			_ = stopPG(pgCfg)
			fmt.Fprintf(os.Stderr, "error: seeding failed: %v\n", err)
			return 1
		}
	}

	// Persist metadata and write info.txt / stop.sh.
	m := &meta{
		Workdir:       opts.workdir,
		Host:          opts.host,
		HTTPPort:      opts.port,
		PGPort:        opts.pgPort,
		PGURL:         pgURL,
		PGPassword:    pgPassword,
		CacheDir:      opts.cache,
		ServerPid:     serverCmd.Process.Pid,
		Status:        "running",
		CreatedAt:     now(),
		AdminEmail:    opts.adminEmail,
		AdminPassword: opts.adminPassword,
		Users:         users,
	}
	if err := m.save(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	writeInfo(m)
	writeStopScript(m)

	printURLs(m)

	// Wait for a signal or for the laelia server to exit (e.g. when the stop
	// command terminates it), then shut down. Watching the child means an
	// instance never leaves an orphaned run process behind after stop.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	serverDone := make(chan struct{})
	go func() {
		_ = serverCmd.Wait()
		close(serverDone)
	}()

	var reason string
	select {
	case <-sigCh:
		reason = "signal received"
		// Gracefully stop the laelia server; the Wait goroutine observes exit.
		_ = serverCmd.Process.Signal(os.Interrupt)
		select {
		case <-serverDone:
		case <-time.After(15 * time.Second):
			_ = serverCmd.Process.Kill()
			<-serverDone
		}
	case <-serverDone:
		reason = "laelia server exited"
	}
	fmt.Printf("\n%s; stopping postgres...\n", reason)
	if !opts.keep {
		_ = stopPG(pgCfg)
	}
	m.Status = "stopped"
	_ = m.save()
	return 0
}

func buildBinary(opts *runOptions) error {
	repo := opts.repo
	if repo == "" {
		repo = os.Getenv("LAELIA_TEST_REPO")
	}
	if repo == "" {
		return fmt.Errorf("cannot locate repo root; pass --repo or set LAELIA_TEST_REPO")
	}
	script := filepath.Join(repo, "scripts", "build_test_server.sh")
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(), "LAELIA_TEST_CACHE="+opts.cache)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func defaultUsers(adminEmail, adminPassword string) []seedUser {
	return []seedUser{
		{Email: adminEmail, Password: adminPassword, Name: "Admin", Admin: true},
		{Email: "alice@laelia.test", Password: "alice1234", Name: "Alice"},
		{Email: "bob@laelia.test", Password: "bob1234", Name: "Bob"},
	}
}

func printURLs(m *meta) {
	fmt.Println()
	fmt.Println("Laelia test server is running")
	fmt.Printf("  page:    http://127.0.0.1:%d\n", m.HTTPPort)
	if ip := lanIP(); ip != "" {
		fmt.Printf("  lan:     http://%s:%d\n", ip, m.HTTPPort)
	}
	fmt.Printf("  admin:   %s / %s\n", m.AdminEmail, m.AdminPassword)
	for _, u := range m.Users {
		if !u.Admin {
			fmt.Printf("  user:    %s / %s\n", u.Email, u.Password)
		}
	}
	fmt.Printf("  stop:    bash %s\n", filepath.Join(m.Workdir, "stop.sh"))
	fmt.Printf("  delete:  rm -rf %s   (run stop first)\n", m.Workdir)
	fmt.Println()
}

func writeInfo(m *meta) {
	var b strings.Builder
	b.WriteString("Laelia test server\n")
	b.WriteString(fmt.Sprintf("  page:    http://127.0.0.1:%d\n", m.HTTPPort))
	if ip := lanIP(); ip != "" {
		b.WriteString(fmt.Sprintf("  lan:     http://%s:%d\n", ip, m.HTTPPort))
	}
	b.WriteString(fmt.Sprintf("  admin:   %s / %s\n", m.AdminEmail, m.AdminPassword))
	for _, u := range m.Users {
		if !u.Admin {
			b.WriteString(fmt.Sprintf("  user:    %s / %s\n", u.Email, u.Password))
		}
	}
	b.WriteString(fmt.Sprintf("  stop:    bash %s\n", filepath.Join(m.Workdir, "stop.sh")))
	b.WriteString(fmt.Sprintf("  delete:  rm -rf %s   (run stop first)\n", m.Workdir))
	_ = os.WriteFile(filepath.Join(m.Workdir, "info.txt"), []byte(b.String()), 0o644)
}

func writeStopScript(m *meta) {
	content := fmt.Sprintf("#!/usr/bin/env bash\n# Stop the laelia test server in %s\nexec %s stop --workdir %q\n",
		m.Workdir, os.Args[0], m.Workdir)
	_ = os.WriteFile(filepath.Join(m.Workdir, "stop.sh"), []byte(content), 0o755)
}
