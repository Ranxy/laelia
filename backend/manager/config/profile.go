package config

import (
	"sync/atomic"

	"github.com/Ranxy/laelia/backend/common"
)

type Profile struct {
	// Mode can be "prod" or "dev"
	Mode common.ReleaseMode
	// Port is the binding port for the server.
	Port int
	// PgURL is the PostgreSQL instance connection url
	PgURL string

	ExternalURL string

	// TLS config
	TLSDomain  string
	TLSCertDir string
	TLSDataDir string
	TLSHosts   []string

	// AllowedOrigins lists extra origins (scheme://host[:port]) allowed to call
	// the API cross-origin with credentials. Same-origin requests are always
	// allowed. Populated from LAELIA_ALLOWED_ORIGINS (comma-separated); empty
	// means only same-origin requests are accepted.
	AllowedOrigins []string

	// CookieSameSite overrides the access-token cookie SameSite policy: "lax"
	// (default), "strict", or "none". "none" is only for deployments that serve
	// the frontend from a different site than the API and accept the CSRF
	// tradeoff (mitigated by Origin validation + CORS allowlist); it is only
	// honored over HTTPS. Populated from LAELIA_COOKIE_SAMESITE.
	CookieSameSite string

	// TrustProxy controls whether client-supplied forwarding headers
	// (X-Forwarded-For / X-Real-IP) are trusted as the request source IP. Enable
	// only when the server sits behind a trusted reverse proxy that overwrites
	// these headers; otherwise a client can spoof its source IP to bypass IP
	// allowlists and pin/per-user rate limits. When false, the source IP is the
	// raw TCP peer address.
	TrustProxy bool

	// LastActiveTS is the service last active timestamp, any API calls will refresh this value.
	LastActiveTS atomic.Int64
	// can be set in runtime
	RuntimeDebug atomic.Bool

	// PprofAddr is the bind address for the standalone pprof HTTP server, e.g.
	// "127.0.0.1:6060". Empty disables pprof entirely. pprof is served on a
	// separate listener (never the public port) so heap/goroutine dumps are not exposed unauthenticated
	// on the network. Bind to localhost or an admin-only address.
	PprofAddr string
}
