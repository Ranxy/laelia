package credential

import (
	"os"
	"strings"
	"sync"

	"github.com/Ranxy/laelia/backend/agent/atomicfile"
)

type Manager struct {
	tokenFilePath  string
	bootstrapToken string

	// mu guards refreshToken, which is read by Connect (via the Run loop) and
	// written by SaveRefreshToken on each successful rotation, possibly
	// concurrently with Connect. The token is intentionally NOT cleared on
	// Disconnect: it is the agent's persistent reconnection credential after
	// the single-use bootstrap token has been consumed.
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
	// Atomic + fsync: a truncated refresh token file bricks reconnection (the
	// single-use bootstrap token is already consumed), so durability matters
	// more than the tiny fsync cost on this low-frequency write.
	_ = atomicfile.WriteFileAtomicSync(cm.tokenFilePath, []byte(token), 0o600)
}
