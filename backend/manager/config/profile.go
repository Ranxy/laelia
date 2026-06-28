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

	// DisableACP disables ACP task submission for non-admin users.
	// When true, only workspace admins can send ACP tasks (controlled rollout).
	DisableACP bool

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
}
