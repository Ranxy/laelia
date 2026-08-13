// Package state persists the machine's local registration state in
// ~/.laelia/machine.json. It is the single source of truth for the machine's
// identity (machine id) and its only credential (the refresh token); the
// bootstrap-token era's per-machine token files are gone. One machine per
// computer means one state file.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Ranxy/laelia/backend/agent/atomicfile"
)

// State is the persisted machine registration. The refresh token is the only
// credential; the manager URL and machine id let setup decide between
// re-authenticating the existing machine and creating a new one.
type State struct {
	ManagerURL   string    `json:"manager_url"`
	MachineID    string    `json:"machine_id"`
	RefreshToken string    `json:"refresh_token"`
	Hostname     string    `json:"hostname"`
	CreatedAt    time.Time `json:"created_at"`
}

// Path returns the state file location (~/.laelia/machine.json).
func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".laelia", "machine.json")
	}
	return filepath.Join(home, ".laelia", "machine.json")
}

// Load reads the state file. A missing file returns (nil, nil); a corrupt
// file returns an error so the caller can decide to wipe and re-flow.
func Load() (*State, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes the state atomically with 0600 perms. The refresh token is the
// machine's only reconnection credential, so durability matters: a truncated
// file would force a full re-auth.
func Save(s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFileAtomicSync(Path(), data, 0o600)
}

// Clear removes the state file, wiping the local machine identity and
// credential. The server-side machine row is untouched.
func Clear() error {
	err := os.Remove(Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
