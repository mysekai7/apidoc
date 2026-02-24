package store

import "github.com/yourorg/apidoc/pkg/types"

type Store interface {
	CreateSession(source, scenario, host string) (*types.Session, error)
	GetSession(id string) (*types.Session, error)
	UpdateSessionStatus(id, status string) error
	ListSessions() ([]types.Session, error)
	DeleteSession(id string) error

	SaveLogs(sessionID string, logs []types.TrafficLog) error
	GetLogs(sessionID string) ([]types.TrafficLog, error)

	SaveBatchCache(cache *types.LLMCache) error
	GetBatchCaches(sessionID string) ([]types.LLMCache, error)
	GetFailedBatches(sessionID string) ([]types.LLMCache, error)
	ClearCaches(sessionID string) error

	Close() error
}
