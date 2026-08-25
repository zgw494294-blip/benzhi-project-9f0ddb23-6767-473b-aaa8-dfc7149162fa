package repository

import (
	"encoding/json"
	"sync"

	"karst-map-release/internal/domain"
)

type MemoryStore struct {
	mu          sync.RWMutex
	packages    map[string]*domain.SurveyPackage
	idempotency map[string]json.RawMessage
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{packages: map[string]*domain.SurveyPackage{}, idempotency: map[string]json.RawMessage{}}
}

func (m *MemoryStore) Get(id string) (*domain.SurveyPackage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := m.packages[id]
	if p == nil {
		return nil, domain.NotFound("成果包")
	}
	return clonePackage(p)
}

func (m *MemoryStore) GetIdempotency(key string) (json.RawMessage, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.idempotency[key]
	return cloneRaw(v), ok
}

func (m *MemoryStore) Commit(id string, expected int64, eventType string, aggregate *domain.SurveyPackage, key string, result json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := int64(0)
	if p := m.packages[id]; p != nil {
		current = p.Version
	}
	if current != expected {
		return ErrVersionConflict
	}
	if aggregate == nil || aggregate.Version != expected+1 {
		return domain.Conflict("提交版本无效")
	}
	if _, ok := m.idempotency[key]; ok {
		return domain.Conflict("幂等键冲突")
	}
	copyAggregate, err := clonePackage(aggregate)
	if err != nil {
		return err
	}
	m.packages[id] = copyAggregate
	m.idempotency[key] = cloneRaw(result)
	return nil
}

func (m *MemoryStore) Health() (HealthReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return HealthReport{SchemaVersion: schemaVersion, PackageCount: len(m.packages), Integrity: "verified"}, nil
}

func (m *MemoryStore) Close() error { return nil }
