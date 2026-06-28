package cmd

import (
	"os"
	"strings"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/config"
)

func getBaseProfile(_ string) *config.Profile {
	cfg := &config.Profile{
		Mode:       common.ReleaseMode("dev"),
		Port:       flags.port,
		PgURL:      os.Getenv("LAELIA_PG_URL"),
		TLSCertDir: flags.tlsCertDir,
		TLSDomain:  flags.tlsDomain,
		DisableACP: flags.disableACP,
		TrustProxy: flags.trustProxy,
	}

	if flags.tlsHost != "" {
		cfg.TLSHosts = strings.Split(flags.tlsHost, ",")
	}

	cfg.RuntimeDebug.Store(flags.debug)
	return cfg
}
