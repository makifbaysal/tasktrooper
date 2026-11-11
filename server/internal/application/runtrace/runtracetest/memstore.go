// Package runtracetest holds the in-memory trace store the tests of the
// recorder and its hooks share.
package runtracetest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type MemStore struct {
	mu       sync.Mutex
	Headers  map[uuid.UUID]domain.RunTrace
	Events   []domain.RunTraceEvent
	Finished *domain.RunTraceVerification
	Head     string
	Dropped  int
}

func NewMemStore() *MemStore { return &MemStore{Headers: map[uuid.UUID]domain.RunTrace{}} }

var _ port.RunTraceStore = (*MemStore)(nil)

func (m *MemStore) Begin(_ context.Context, t domain.RunTrace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Headers[t.RunID]; !ok {
		m.Headers[t.RunID] = t
	}
	return nil
}
func (m *MemStore) Header(_ context.Context, id uuid.UUID) (domain.RunTrace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.Headers[id]
	if !ok {
		return h, domain.ErrRunTraceNotFound
	}
	h.Verification = m.Finished
	return h, nil
}
func (m *MemStore) ListByTask(_ context.Context, taskID uuid.UUID) ([]domain.RunTrace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []domain.RunTrace{}
	for _, h := range m.Headers {
		if h.TaskID == taskID {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}
func (m *MemStore) NextSeq(_ context.Context, id uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.Events {
		if e.RunID == id && e.Seq > n {
			n = e.Seq
		}
	}
	return n + 1, nil
}
func (m *MemStore) Append(_ context.Context, evs []domain.RunTraceEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, evs...)
	return nil
}
func (m *MemStore) Finish(_ context.Context, _ uuid.UUID, head string, dropped int, v *domain.RunTraceVerification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Head, m.Dropped, m.Finished = head, dropped, v
	return nil
}
func (m *MemStore) List(_ context.Context, id uuid.UUID, f port.RunTraceFilter) ([]domain.RunTraceEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.RunTraceEvent
	for _, e := range m.Events {
		if e.RunID == id && e.Seq > f.After && (f.Category == "" || e.Category == f.Category) {
			out = append(out, e)
			if f.Limit > 0 && len(out) == f.Limit {
				break
			}
		}
	}
	return out, nil
}
func (m *MemStore) Counts(context.Context, uuid.UUID) (domain.RunTraceCounts, error) {
	return domain.RunTraceCounts{}, nil
}
func (m *MemStore) Effects(_ context.Context, id uuid.UUID) ([]domain.RunTraceEvent, error) {
	return m.List(context.Background(), id, port.RunTraceFilter{})
}
func (m *MemStore) Changes(_ context.Context, id uuid.UUID) ([]domain.RunTraceEvent, error) {
	return m.List(context.Background(), id, port.RunTraceFilter{Category: domain.TraceCategoryChange})
}
func (m *MemStore) DeleteBefore(context.Context, time.Time, int) (int, error) { return 0, nil }

func (m *MemStore) BySeq(seq int) domain.RunTraceEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.Events {
		if e.Seq == seq {
			return e
		}
	}
	return domain.RunTraceEvent{}
}
