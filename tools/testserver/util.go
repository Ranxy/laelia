package main

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// randomFreePort returns a free TCP port in [lo, hi]. It binds, records the
// port, and closes the listener so the caller can bind it again.
func randomFreePort(lo, hi int) (int, error) {
	for i := 0; i < 64; i++ {
		p := lo + rand.Intn(hi-lo+1)
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return p, nil
	}
	return 0, fmt.Errorf("no free port found in [%d, %d]", lo, hi)
}

// defaultCacheDir returns the shared build/cache directory, honoring
// LAELIA_TEST_CACHE. Falls back to ~/.cache/laelia-test.
func defaultCacheDir() string {
	if v := os.Getenv("LAELIA_TEST_CACHE"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "laelia-test")
	}
	return filepath.Join(home, ".cache", "laelia-test")
}

// defaultBinaryPath returns the path of the built laelia manager binary in the
// shared cache.
func defaultBinaryPath(cacheDir string) string {
	return filepath.Join(cacheDir, "laelia")
}

// randomPassword returns a random alphanumeric string of the given length.
func randomPassword(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func now() time.Time { return time.Now() }

// lanIP returns the first non-loopback IPv4 address of the host, or "".
func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			if ip4 := ipn.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}

// syscallKill0 sends signal 0 to check whether a process exists.
func syscallKill0(pid int) error {
	return syscall.Kill(pid, 0)
}

// portOpen reports whether something is listening on 127.0.0.1:port.
func portOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
