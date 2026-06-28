package credential

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCredentialManager_ConcurrentAccessNoRace hammers Load/Save/Delete from
// many goroutines. Under -race this fails if refreshToken is unprotected.
func TestCredentialManager_ConcurrentAccessNoRace(t *testing.T) {
	cm := New(filepath.Join(t.TempDir(), "agent-token-test"), "bootstrap")

	const goroutines = 16
	const iters = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				switch (n + j) % 3 {
				case 0:
					_ = cm.LoadRefreshToken()
				case 1:
					cm.SaveRefreshToken("refresh-value")
				case 2:
					cm.DeleteRefreshToken()
				default:
					_ = cm.LoadRefreshToken()
				}
			}
		}(i)
	}
	wg.Wait()

	// Bootstrap token is immutable and never races; sanity-check it survives.
	assert.Equal(t, "bootstrap", cm.BootstrapToken())
}
