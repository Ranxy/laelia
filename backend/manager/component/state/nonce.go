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

// nonceReplayTTL is how long a verified nonce is remembered as seen. The
// verification window is [-35s, +5s] (40s span); adding a 5s margin covers the
// tail so a nonce at the leading edge cannot be replayed just after it would
// otherwise age out of the window.
const nonceReplayTTL = 45 * time.Second

// nonceWindowPast/nonceWindowFuture define the validity window around the
// embedded timestamp: a nonce is accepted only if its timestamp is within
// [now-35s, now+5s] (clock-skew tolerance for the agent heartbeat path).
const (
	nonceWindowPast   = 35 * time.Second
	nonceWindowFuture = 5 * time.Second
)

type NonceManager struct {
	mu      sync.RWMutex
	secrets map[string][]byte

	// replay guards against reusing a captured heartbeat nonce within its
	// validity window. Keyed by agentResourceID + separator + nonce so a nonce
	// is one-time per agent. Entries expire after replayTTL and are swept lazily
	// on each successful verification, so the map never holds stale entries and
	// memory is bounded by the nonce rate over one TTL.
	// TODO(T14): single-process only; a multi-instance manager must share this
	// cache (e.g. Redis) or a captured nonce validates on a different instance.
	replayMu  sync.Mutex
	replay    map[string]int64 // key -> expiry unix nano
	replayTTL time.Duration
}

func NewNonceManager() *NonceManager {
	return &NonceManager{
		secrets:   make(map[string][]byte),
		replay:    make(map[string]int64),
		replayTTL: nonceReplayTTL,
	}
}

// nonceWithinWindow reports whether a timestamp falls in the accepted validity
// window relative to now. Extracted as a pure helper so the window logic is
// unit-testable without generating a nonce and waiting for it to age out.
func nonceWithinWindow(timestampSec, nowSec int64) bool {
	if timestampSec < nowSec-int64(nonceWindowPast/time.Second) {
		return false
	}
	if timestampSec > nowSec+int64(nonceWindowFuture/time.Second) {
		return false
	}
	return true
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
	if !nonceWithinWindow(timestampSec, nowSec) {
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

	if !hmac.Equal(actualSig, expectedSig) {
		return false
	}

	// Signature valid and within window: enforce one-time use. A captured nonce
	// replayed within the window is rejected, and the nonce is recorded so a
	// second replay is also rejected. The check+write is atomic under replayMu
	// so two concurrent replays of the same nonce cannot both pass.
	return nm.recordAndCheckReplay(agentResourceID, nonce)
}

// recordAndCheckReplay returns true if the nonce is fresh (first use) and false
// if it is a replay of a nonce already verified within its TTL. It atomically
// checks and writes under replayMu and lazily sweeps expired entries so the
// cache does not leak.
func (nm *NonceManager) recordAndCheckReplay(agentResourceID, nonce string) bool {
	key := agentResourceID + "\x00" + nonce
	nowNs := time.Now().UnixNano()
	expiry := nowNs + int64(nm.replayTTL)

	nm.replayMu.Lock()
	defer nm.replayMu.Unlock()

	// A present, unexpired entry means this nonce was already accepted: replay.
	if e, ok := nm.replay[key]; ok && e > nowNs {
		return false
	}

	// Lazily evict expired entries so the map only covers ~one TTL of activity.
	for k, exp := range nm.replay {
		if exp <= nowNs {
			delete(nm.replay, k)
		}
	}

	nm.replay[key] = expiry
	return true
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
