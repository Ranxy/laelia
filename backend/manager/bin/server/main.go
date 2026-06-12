package main

import (
	"os"

	"github.com/Ranxy/laelia/backend/manager/bin/server/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
