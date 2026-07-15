// Package store provides persistence for scan results.
package store

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/erantimothy/depscan/internal/domain"
)

type MemoryStore struct {
	mu      sync.RWMutex
	results map[string]domain.ScanResult
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		results: make(map[string]domain.ScanResult),
	}
}

func (m *MemoryStore) Save(_ context.Context, result domain.ScanResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[result.ID] = result
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (domain.ScanResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result, ok := m.results[id]
	if !ok {
		return domain.ScanResult{}, fmt.Errorf("scan %q: %w", id, domain.ErrNotFound)
	}
	return result, nil
}

func (m *MemoryStore) List(_ context.Context) ([]domain.ScanResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]domain.ScanResult, 0, len(m.results))
	for _, r := range m.results {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}
