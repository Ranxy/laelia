//go:build release

package cmd

import (
	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/config"
)

func activeProfile(dataDir string) *config.Profile {
	p := getBaseProfile(dataDir)
	p.Mode = common.ReleaseModeProd
	return p
}
