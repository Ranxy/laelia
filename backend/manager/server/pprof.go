package server

import (
	"net/http"
	"net/http/pprof"
)

// newPprofServer builds a standalone HTTP server that exposes /debug/pprof/* on
// the given address. It is intentionally separate from the public echo listener
// so heap/goroutine/profile dumps are not reachable from the network: bind it
// to a localhost or admin-only address and only start it when runtime debug is
// enabled. The caller owns its lifecycle (Serve + Shutdown). Registering pprof
// on the public listener (even gated by a runtime flag) exposed dumps to anyone
// who could reach the port with no authentication.
func newPprofServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return &http.Server{Addr: addr, Handler: mux}
}
