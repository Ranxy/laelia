package state

import (
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/manager/store"
)

type State struct {
	TokenExpireCache *lru.Cache[string, bool]
	NonceManager     *NonceManager
	HeartbeatBuffer  *HeartbeatBuffer
}

func New() (*State, error) {
	expireCache, err := lru.New[string, bool](128)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create auth expire cache")
	}
	return &State{
		TokenExpireCache: expireCache,
		NonceManager:     NewNonceManager(),
	}, nil
}

func NewWithStore(stores *store.Store) (*State, error) {
	s, err := New()
	if err != nil {
		return nil, err
	}
	s.HeartbeatBuffer = NewHeartbeatBuffer(stores, 0)
	return s, nil
}
