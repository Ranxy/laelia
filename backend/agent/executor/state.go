package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type LocalState struct {
	CommandID        string        `json:"command_id"`
	ExecutorKind     string        `json:"executor_kind,omitempty"`
	Status           string        `json:"status"`
	StartedAt        int64         `json:"started_at"`
	LastSeqSent      int32         `json:"last_seq_sent"`
	LastEventSeqSent int32         `json:"last_event_seq_sent"`
	SessionID        string        `json:"session_id,omitempty"`
	OutputBuffer     []OutputChunk `json:"output_buffer"`
}

func LoadLocalState() (*LocalState, error) {
	data, err := os.ReadFile(statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state LocalState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func SaveLocalState(state *LocalState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	dir := filepath.Dir(statePath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return os.WriteFile(statePath(), data, 0600)
}

func ClearLocalState() error {
	if err := os.Remove(statePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func statePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".laelia", "command-state.json")
}
