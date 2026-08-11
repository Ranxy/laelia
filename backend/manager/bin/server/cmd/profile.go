package cmd

import (
	"os"
	"strings"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/config"
)

func getBaseProfile(_ string) *config.Profile {
	cfg := &config.Profile{
		Mode:           common.ReleaseMode("dev"),
		Port:           flags.port,
		PgURL:          os.Getenv("LAELIA_PG_URL"),
		TLSCertDir:     flags.tlsCertDir,
		TLSDomain:      flags.tlsDomain,
		TrustProxy:     flags.trustProxy,
		PprofAddr:      flags.pprofAddr,
		AllowedOrigins: splitCSV(os.Getenv("LAELIA_ALLOWED_ORIGINS")),
		CookieSameSite: os.Getenv("LAELIA_COOKIE_SAMESITE"),
	}

	if flags.tlsHost != "" {
		cfg.TLSHosts = strings.Split(flags.tlsHost, ",")
	}

	cfg.RuntimeDebug.Store(flags.debug)
	return cfg
}

// splitCSV splits a comma-separated list, trimming whitespace and dropping
// empty entries.
func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
