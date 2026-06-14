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

	// LastActiveTS is the service last active timestamp, any API calls will refresh this value.
	LastActiveTS atomic.Int64
	// can be set in runtime
	RuntimeDebug atomic.Bool
}
