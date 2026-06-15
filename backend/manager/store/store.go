package store

import (
	"context"
	"database/sql"
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

type Store struct {
	Secret        string
	dbConnManager *DBConnectionManager
	enableCache   bool

	userIDCache          *lru.Cache[int, *UserMessage]
	userEmailCache       *lru.Cache[string, *UserMessage]
	settingCache         *lru.Cache[models.SettingName, *SettingMessage]
	policyCache          *lru.Cache[string, *PolicyMessage]
	idpCache             *lru.Cache[string, *IdentityProviderMessage]
	groupCache           *lru.Cache[string, *GroupMessage]
	agentIDCache         *lru.Cache[int, *AgentMessage]
	agentResourceIDCache *lru.Cache[string, *AgentMessage]
}

func New(ctx context.Context, pgURL string, enableCache bool) (*Store, error) {
	userIDCache, err := lru.New[int, *UserMessage](32768)
	if err != nil {
		return nil, err
	}
	userEmailCache, err := lru.New[string, *UserMessage](32768)
	if err != nil {
		return nil, err
	}

	dbConnManager := NewDBConnectionManager(pgURL)
	if err := dbConnManager.Initialize(ctx); err != nil {
		return nil, err
	}
	settingCache, err := lru.New[models.SettingName, *SettingMessage](32768)
	if err != nil {
		return nil, err
	}
	policyCache, err := lru.New[string, *PolicyMessage](32768)
	if err != nil {
		return nil, err
	}
	idpCache, err := lru.New[string, *IdentityProviderMessage](32768)
	if err != nil {
		return nil, err
	}
	groupCache, err := lru.New[string, *GroupMessage](32768)
	if err != nil {
		return nil, err
	}
	agentIDCache, err := lru.New[int, *AgentMessage](32768)
	if err != nil {
		return nil, err
	}
	agentResourceIDCache, err := lru.New[string, *AgentMessage](32768)
	if err != nil {
		return nil, err
	}
	s := &Store{
		dbConnManager:        dbConnManager,
		enableCache:          enableCache,
		userIDCache:          userIDCache,
		userEmailCache:       userEmailCache,
		settingCache:         settingCache,
		policyCache:          policyCache,
		idpCache:             idpCache,
		groupCache:           groupCache,
		agentIDCache:         agentIDCache,
		agentResourceIDCache: agentResourceIDCache,
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.dbConnManager.Close()
}

func (s *Store) GetDB() *sql.DB {
	return s.dbConnManager.GetDB()
}

// DeleteCache deletes the cache.
func (s *Store) DeleteCache() {
	s.userEmailCache.Purge()
	s.userIDCache.Purge()
}

func getPolicyCacheKey(resourceType models.Policy_Resource, resource string, policyType models.Policy_Type) string {
	return fmt.Sprintf("policies/%s/%s/%s", resourceType, resource, policyType)
}
