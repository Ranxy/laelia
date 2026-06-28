package credential

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Manager struct {
	tokenFilePath  string
	bootstrapToken string

	// mu guards refreshToken, which is read by Connect (via the Run loop) and
	// written/cleared by Disconnect's DeleteRefreshToken, possibly concurrently.
	mu           sync.Mutex
	refreshToken string
}

func New(tokenFilePath string, bootstrapToken string) *Manager {
	return &Manager{
		tokenFilePath:  tokenFilePath,
		bootstrapToken: bootstrapToken,
	}
}

func (cm *Manager) BootstrapToken() string {
	// bootstrapToken is set once at construction and never mutated.
	return cm.bootstrapToken
}

func (cm *Manager) LoadRefreshToken() string {
	cm.mu.Lock()
	defer cm.mu.Unlock()
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
	cm.mu.Lock()
	cm.refreshToken = token
	cm.mu.Unlock()
	cm.writeToFile(token)
}

func (cm *Manager) DeleteRefreshToken() {
	cm.mu.Lock()
	cm.refreshToken = ""
	cm.mu.Unlock()
	_ = os.Remove(cm.tokenFilePath)
}

func (cm *Manager) writeToFile(token string) {
	dir := filepath.Dir(cm.tokenFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(cm.tokenFilePath, []byte(token), 0o600)
}
