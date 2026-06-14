package credential

import (
	"os"
	"path/filepath"
	"strings"
)

type Manager struct {
	tokenFilePath  string
	bootstrapToken string
	refreshToken   string
}

func New(tokenFilePath string, bootstrapToken string) *Manager {
	return &Manager{
		tokenFilePath:  tokenFilePath,
		bootstrapToken: bootstrapToken,
	}
}

func (cm *Manager) BootstrapToken() string {
	return cm.bootstrapToken
}

func (cm *Manager) LoadRefreshToken() string {
	if cm.refreshToken != "" {
		return cm.refreshToken
	}
	data, err := os.ReadFile(cm.tokenFilePath)
	if err != nil {
		return ""
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return ""
	}
	cm.refreshToken = token
	return token
}

func (cm *Manager) SaveRefreshToken(token string) {
	cm.refreshToken = token
	cm.writeToFile(token)
}

func (cm *Manager) DeleteRefreshToken() {
	cm.refreshToken = ""
	_ = os.Remove(cm.tokenFilePath)
}

func (cm *Manager) writeToFile(token string) {
	dir := filepath.Dir(cm.tokenFilePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	_ = os.WriteFile(cm.tokenFilePath, []byte(token), 0600)
}
