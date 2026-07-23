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

func LoadLocalState(machineID, agentID string) (*LocalState, error) {
	data, err := os.ReadFile(statePath(machineID, agentID))
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

func SaveLocalState(machineID, agentID string, state *LocalState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	dir := filepath.Dir(statePath(machineID, agentID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	return os.WriteFile(statePath(machineID, agentID), data, 0o600)
}

func ClearLocalState(machineID, agentID string) error {
	if err := os.Remove(statePath(machineID, agentID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func statePath(machineID, agentID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".laelia", machineID, agentID, "command-state.json")
}
