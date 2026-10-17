package errordef

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// memStore is a stateful fake: what the service writes is what the sweep and
// the inbox read back on later calls.
type memStore struct {
	mu        sync.Mutex
	defs      map[uuid.UUID]domain.ErrorDefinition
	links     map[string]domain.ErrorGroupLink
	claimedAt map[string]time.Time
	// now stamps a claim, as the database's now() does.
	now func() time.Time
	// beforeCreate runs under the lock as a create starts: a racing writer.
	beforeCreate func(d domain.ErrorDefinition)
	clock        time.Time
	lists        int
}

func newMemStore() *memStore {
	return &memStore{defs: map[uuid.UUID]domain.ErrorDefinition{}, links: map[string]domain.ErrorGroupLink{},
		claimedAt: map[string]time.Time{}, clock: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC), now: time.Now}
}

var _ port.ErrorDefinitionStore = (*memStore)(nil)

func (m *memStore) tick() time.Time {
	m.clock = m.clock.Add(time.Second)
	return m.clock
}

func (m *memStore) ListErrorDefinitions(_ context.Context, f domain.ErrorDefinitionFilter) ([]domain.ErrorDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lists++
	out := []domain.ErrorDefinition{}
	for _, d := range m.defs {
		if len(f.RepositoryIDs) > 0 && !slices.Contains(f.RepositoryIDs, d.RepositoryID) {
			continue
		}
		if f.ComponentID != nil && d.ComponentID != nil && *d.ComponentID != *f.ComponentID {
			continue
		}
		if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, d.Status) {
			continue
		}
		if len(f.Actions) > 0 && !slices.Contains(f.Actions, d.Action) {
			continue
		}
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b domain.ErrorDefinition) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

func (m *memStore) GetErrorDefinition(_ context.Context, id uuid.UUID) (domain.ErrorDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.defs[id]
	if !ok {
		return domain.ErrorDefinition{}, fmt.Errorf("get: %w", port.ErrNotFound)
	}
	return d, nil
}

func (m *memStore) CreateErrorDefinition(_ context.Context, d domain.ErrorDefinition) (domain.ErrorDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.beforeCreate != nil {
		m.beforeCreate(d)
	}
	if d.Status == domain.ErrorDefinitionSuggested {
		for _, e := range m.defs {
			if e.Status == domain.ErrorDefinitionSuggested && e.RepositoryID == d.RepositoryID &&
				sameComponent(e.ComponentID, d.ComponentID) && sameMatch(e.Match, d.Match) {
				return domain.ErrorDefinition{}, port.ErrErrorDefinitionExists
			}
		}
	}
	d.ID = uuid.New()
	d.CreatedAt = m.tick()
	d.UpdatedAt = d.CreatedAt
	m.defs[d.ID] = d
	return d, nil
}

func (m *memStore) UpdateErrorDefinition(_ context.Context, d domain.ErrorDefinition) (domain.ErrorDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.defs[d.ID]
	if !ok {
		return domain.ErrorDefinition{}, port.ErrNotFound
	}
	d.Status, d.Source, d.TaskID, d.CreatedAt = cur.Status, cur.Source, cur.TaskID, cur.CreatedAt
	if d.Action != cur.Action {
		d.FiringSince = nil
	}
	d.UpdatedAt = m.tick()
	m.defs[d.ID] = d
	return d, nil
}

func (m *memStore) SetErrorDefinitionStatus(_ context.Context, id uuid.UUID, status domain.ErrorDefinitionStatus) (domain.ErrorDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.defs[id]
	if !ok {
		return domain.ErrorDefinition{}, port.ErrNotFound
	}
	d.Status = status
	d.UpdatedAt = m.tick()
	m.defs[id] = d
	return d, nil
}

func (m *memStore) SetErrorDefinitionFiring(_ context.Context, id uuid.UUID, since *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.defs[id]
	d.FiringSince = since
	m.defs[id] = d
	return nil
}

func (m *memStore) SetErrorDefinitionTask(_ context.Context, id, taskID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.defs[id]
	if d.TaskID == nil {
		d.TaskID = &taskID
	}
	m.defs[id] = d
	return nil
}

func (m *memStore) DeleteErrorDefinition(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.defs[id]; !ok {
		return port.ErrNotFound
	}
	delete(m.defs, id)
	return nil
}

func linkKey(id uuid.UUID, fp string) string { return id.String() + "\x00" + fp }

func (m *memStore) ClaimErrorGroupLink(_ context.Context, l domain.ErrorGroupLink, staleBefore time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := linkKey(l.DefinitionID, l.Fingerprint)
	if have, ok := m.links[k]; ok && (have.TaskID != nil || !m.claimedAt[k].Before(staleBefore)) {
		return false, nil
	}
	m.links[k] = l
	m.claimedAt[k] = m.now()
	return true, nil
}

func (m *memStore) SetErrorGroupLinkTask(_ context.Context, id uuid.UUID, fp string, taskID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.links[linkKey(id, fp)]
	l.TaskID = &taskID
	m.links[linkKey(id, fp)] = l
	return nil
}

func (m *memStore) ReleaseErrorGroupLink(_ context.Context, id uuid.UUID, fp string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.links, linkKey(id, fp))
	return nil
}

type fakeComponents struct{ comps []domain.Component }

func (f *fakeComponents) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	for _, c := range f.comps {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.Component{}, port.ErrNotFound
}

func (f *fakeComponents) ListComponents(_ context.Context, repo uuid.UUID) ([]domain.Component, error) {
	var out []domain.Component
	for _, c := range f.comps {
		if c.RepositoryID == repo {
			out = append(out, c)
		}
	}
	return out, nil
}

type fakeErrors struct {
	queries []opsquery.ErrorsQuery
	answer  domain.OpsErrors
	// byComponent, when set, answers each query with the groups and
	// unavailable entries of its components only.
	byComponent bool
}

func (f *fakeErrors) Errors(_ context.Context, q opsquery.ErrorsQuery) (domain.OpsErrors, error) {
	f.queries = append(f.queries, q)
	if !f.byComponent {
		return f.answer, nil
	}
	out := domain.OpsErrors{Truncated: f.answer.Truncated}
	for _, g := range f.answer.Groups {
		if slices.Contains(q.ComponentIDs, g.ComponentID) {
			out.Groups = append(out.Groups, g)
		}
	}
	for _, u := range f.answer.Unavailable {
		if slices.Contains(q.ComponentIDs, u.ComponentID) {
			out.Unavailable = append(out.Unavailable, u)
		}
	}
	return out, nil
}

type fakeIncidents struct {
	ingested []domain.IncidentInput
	live     []domain.Incident
	resolved []uuid.UUID
}

func (f *fakeIncidents) Ingest(_ context.Context, in domain.IncidentInput) (domain.Incident, error) {
	f.ingested = append(f.ingested, in)
	return domain.Incident{ID: uuid.New()}, nil
}

func (f *fakeIncidents) List(_ context.Context, filter domain.IncidentFilter) ([]domain.Incident, error) {
	var out []domain.Incident
	for _, in := range f.live {
		if filter.Source == "" || in.Source == filter.Source {
			out = append(out, in)
		}
	}
	return out, nil
}

func (f *fakeIncidents) Resolve(_ context.Context, id uuid.UUID, _ string) (domain.Incident, error) {
	f.resolved = append(f.resolved, id)
	return domain.Incident{ID: id, Status: domain.IncidentStatusResolved}, nil
}

type fakeTasks struct {
	created []domain.CreateBoardTaskRequest
	fail    bool
}

func (f *fakeTasks) CreateTask(_ context.Context, _ uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	if f.fail {
		return domain.BoardTask{}, fmt.Errorf("board down")
	}
	f.created = append(f.created, req)
	return domain.BoardTask{ID: uuid.New(), Key: fmt.Sprintf("B-%d", len(f.created))}, nil
}

type fakeRoles struct {
	developerRole uuid.UUID
	byArea        map[string]uuid.UUID
}

func (f *fakeRoles) RoleByKey(_ context.Context, key string) (domain.AgentRole, error) {
	if key != domain.RoleKeyDeveloper {
		return domain.AgentRole{}, port.ErrNotFound
	}
	return domain.AgentRole{ID: f.developerRole}, nil
}

func (f *fakeRoles) AgentForRole(_ context.Context, roleID uuid.UUID, area string) (*uuid.UUID, error) {
	if roleID != f.developerRole {
		return nil, nil
	}
	if id, ok := f.byArea[area]; ok {
		return &id, nil
	}
	return nil, nil
}
