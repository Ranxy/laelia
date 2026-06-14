package state

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type NonceManager struct {
	mu      sync.RWMutex
	secrets map[string][]byte
}

func NewNonceManager() *NonceManager {
	return &NonceManager{
		secrets: make(map[string][]byte),
	}
}

func (nm *NonceManager) GenerateNonce(agentResourceID string, sessionID string) string {
	key := nm.getOrCreateKey(agentResourceID)

	randomBytes := make([]byte, 24)
	_, _ = rand.Read(randomBytes)

	timestampSec := time.Now().Unix()
	data := fmt.Sprintf("%s:%s:%s:%d", agentResourceID, sessionID, base64urlEncode(randomBytes), timestampSec)

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	signature := mac.Sum(nil)

	return fmt.Sprintf("%s.%d.%s", base64urlEncode(randomBytes), timestampSec, hex.EncodeToString(signature))
}

func (nm *NonceManager) VerifyNonce(nonce string, agentResourceID string, sessionID string) bool {
	if nonce == "" {
		return false
	}

	parts := splitNonce(nonce)
	if len(parts) != 3 {
		return false
	}

	randomB64 := parts[0]
	timestampStr := parts[1]
	signatureHex := parts[2]

	var timestampSec int64
	if _, err := fmt.Sscanf(timestampStr, "%d", &timestampSec); err != nil {
		return false
	}

	nowSec := time.Now().Unix()
	if timestampSec < nowSec-35 || timestampSec > nowSec+5 {
		return false
	}

	data := fmt.Sprintf("%s:%s:%s:%d", agentResourceID, sessionID, randomB64, timestampSec)

	nm.mu.RLock()
	key := nm.secrets[agentResourceID]
	nm.mu.RUnlock()
	if key == nil {
		return false
	}

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	expectedSig := mac.Sum(nil)

	actualSig, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}

	return hmac.Equal(actualSig, expectedSig)
}

func (nm *NonceManager) getOrCreateKey(agentResourceID string) []byte {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if key, ok := nm.secrets[agentResourceID]; ok {
		return key
	}

	key := make([]byte, 32)
	_, _ = rand.Read(key)
	nm.secrets[agentResourceID] = key
	return key
}

func (nm *NonceManager) DeleteKey(agentResourceID string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	delete(nm.secrets, agentResourceID)
}

func base64urlEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func splitNonce(nonce string) []string {
	dot1 := -1
	dot2 := -1
	for i, c := range nonce {
		if c == '.' {
			if dot1 == -1 {
				dot1 = i
			} else {
				dot2 = i
				break
			}
		}
	}
	if dot1 == -1 || dot2 == -1 {
		return nil
	}
	return []string{nonce[:dot1], nonce[dot1+1 : dot2], nonce[dot2+1:]}
}
