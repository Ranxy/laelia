package cmd

import (
	"os"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/config"
)

func getBaseProfile(_ string) *config.Profile {
	config := &config.Profile{
		Mode:  common.ReleaseMode("dev"),
		Port:  flags.port,
		PgURL: os.Getenv("LAELIA_PG_URL"),
	}

	config.RuntimeDebug.Store(flags.debug)
	return config
}
