package state

import (
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pkg/errors"
)

type State struct {
	TokenExpireCache *lru.Cache[string, bool]
}

func New() (*State, error) {
	expireCache, err := lru.New[string, bool](128)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create auth expire cache")
	}
	return &State{
		TokenExpireCache: expireCache,
	}, nil
}
