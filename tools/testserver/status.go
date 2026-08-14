package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func statusCmd(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
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
	alive := m.ServerPid > 0 && processAlive(m.ServerPid)
	fmt.Printf("status: %s\n", m.Status)
	fmt.Printf("http:   http://%s:%d\n", m.Host, m.HTTPPort)
	fmt.Printf("pid:    %d (alive=%v)\n", m.ServerPid, alive)
	fmt.Printf("pg:     %s\n", m.PGURL)
	return 0
}

func processAlive(pid int) bool {
	return syscallKill0(pid) == nil
}
