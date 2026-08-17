package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/supervisor"
)

func init() {
	rootCmd.AddCommand(stopCmd)
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background laelia-machine daemon",
	Long: `Stop the background laelia-machine daemon started by 'laelia-machine
setup': the supervisor stops its worker (laelia-machine run) and exits. The
saved login is kept, so 'laelia-machine setup' starts the machine again
without re-authenticating.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return stopMachine()
	},
}

// stopMachine asks the background supervisor to shut down and waits for it to
// exit (the addr file disappears once the supervisor has stopped its worker
// and removed its control endpoint).
func stopMachine() error {
	if err := supervisor.Stop(); err != nil {
		return errors.New("failed to stop laelia-machine: " + err.Error())
	}
	_, _ = fmt.Println("laelia-machine is stopping...")

	addrPath := home.Join(supervisor.AddrFile)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(addrPath); os.IsNotExist(err) {
			_, _ = fmt.Println("laelia-machine stopped")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("laelia-machine did not stop within 60s; check the daemon log for errors")
}
