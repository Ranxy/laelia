package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// meta is the per-instance metadata persisted in <workdir>/.meta.json. It is
// the single source of truth for stop/status and for reconstructing the
// embedded-postgres config on a later stop.
type meta struct {
	Workdir       string     `json:"workdir"`
	Host          string     `json:"host"`
	HTTPPort      int        `json:"httpPort"`
	PGPort        int        `json:"pgPort"`
	PGURL         string     `json:"pgURL"`
	PGPassword    string     `json:"pgPassword"`
	CacheDir      string     `json:"cacheDir"`
	ServerPid     int        `json:"serverPid"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"createdAt"`
	AdminEmail    string     `json:"adminEmail"`
	AdminPassword string     `json:"adminPassword"`
	Users         []seedUser `json:"users"`
}

type seedUser struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Admin    bool   `json:"admin"`
}

func metaPath(workdir string) string {
	return filepath.Join(workdir, ".meta.json")
}

func loadMeta(workdir string) (*meta, error) {
	b, err := os.ReadFile(metaPath(workdir))
	if err != nil {
		return nil, err
	}
	var m meta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *meta) save() error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(m.Workdir), b, 0o644)
}
