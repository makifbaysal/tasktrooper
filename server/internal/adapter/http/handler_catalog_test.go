package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type stubCatalogReader struct{ defs []domain.UpstreamAgent }

func (r stubCatalogReader) ReadCatalog(context.Context) ([]domain.UpstreamAgent, string, error) {
	return r.defs, "dir:test", nil
}

type memCatalogPending struct {
	mu    sync.Mutex
	items []domain.CatalogPending
}

func (m *memCatalogPending) GetCatalogSyncState(context.Context) (domain.CatalogSyncState, error) {
	return domain.CatalogSyncState{}, nil
}

func (m *memCatalogPending) SaveCatalogSyncState(context.Context, domain.CatalogSyncState) error {
	return nil
}

func (m *memCatalogPending) AppendCatalogPending(_ context.Context, p domain.CatalogPending) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, p)
	return nil
}

func (m *memCatalogPending) ListCatalogPending(context.Context) ([]domain.CatalogPending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.CatalogPending(nil), m.items...), nil
}

func (m *memCatalogPending) DeleteCatalogPending(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.items {
		if p.ID == id {
			m.items = append(m.items[:i], m.items[i+1:]...)
			return nil
		}
	}
	return errors.New("pending change not found")
}

type applyCatalogStore struct {
	port.CatalogStore
	agents []domain.Agent
	skills map[uuid.UUID]domain.Skill
}

func (s *applyCatalogStore) ListAgents(context.Context) ([]domain.Agent, error) { return s.agents, nil }

func (s *applyCatalogStore) ListSkillsByAgent(_ context.Context, agentID uuid.UUID) ([]domain.Skill, error) {
	var out []domain.Skill
	for _, sk := range s.skills {
		if sk.AgentID == agentID {
			out = append(out, sk)
		}
	}
	return out, nil
}

func (s *applyCatalogStore) GetSkill(_ context.Context, id uuid.UUID) (domain.Skill, error) {
	sk, ok := s.skills[id]
	if !ok {
		return domain.Skill{}, errors.New("skill not found")
	}
	return sk, nil
}

func (s *applyCatalogStore) UpdateSkill(_ context.Context, sk domain.Skill) (domain.Skill, error) {
	s.skills[sk.ID] = sk
	return sk, nil
}

type embedOnlyLLM struct{ port.LLMClient }

func (embedOnlyLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

func applyPendingApp(t *testing.T, defs []domain.UpstreamAgent) (*fiber.App, *applyCatalogStore, *memCatalogPending, domain.CatalogPending) {
	t.Helper()
	agent := domain.Agent{ID: uuid.New(), Name: "Shippy", CatalogSlug: "shippy", KeepSkillsUpdated: true}
	skill := domain.Skill{ID: uuid.New(), AgentID: agent.ID, Name: "ship", Content: "hand tuned body", Enabled: true}
	store := &applyCatalogStore{agents: []domain.Agent{agent}, skills: map[uuid.UUID]domain.Skill{skill.ID: skill}}
	item := domain.CatalogPending{
		ID: uuid.New(), AgentSlug: "shippy", AgentName: "Shippy", Kind: domain.CatalogPendingKindSkill,
		Name: "ship", Action: domain.CatalogPendingActionMerge, Reason: "LLM merge basarisiz: no chat model",
	}
	pending := &memCatalogPending{items: []domain.CatalogPending{item}}
	h := &Handler{
		catalogSvc:       catalog.NewService(store, embedOnlyLLM{}, ""),
		catalogRepo:      stubCatalogReader{defs: defs},
		catalogSyncStore: pending,
	}
	app := fiber.New()
	h.registerCatalogRoutes(app)
	return app, store, pending, item
}

func postApply(t *testing.T, app *fiber.App, id string) (int, map[string]any) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("POST", "/v1/catalog/pending/"+id+"/apply", nil))
	require.NoError(t, err)
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func shippyUpstream() []domain.UpstreamAgent {
	return []domain.UpstreamAgent{{
		Slug: "shippy", Name: "Shippy", Etag: "etag-1",
		Skills: []domain.UpstreamSkill{{Name: "ship", Content: "catalog body", Enabled: true, Sha: "sha-catalog"}},
	}}
}

func TestApplyCatalogPending_TakesTheCatalogVersion(t *testing.T) {
	app, store, pending, item := applyPendingApp(t, shippyUpstream())

	status, _ := postApply(t, app, item.ID.String())

	require.Equal(t, fiber.StatusNoContent, status)
	skills, _ := store.ListSkillsByAgent(context.Background(), store.agents[0].ID)
	require.Len(t, skills, 1)
	require.Equal(t, "catalog body", skills[0].Content)
	require.Equal(t, "sha-catalog", skills[0].CatalogSha)
	left, _ := pending.ListCatalogPending(context.Background())
	require.Empty(t, left)
}

func TestApplyCatalogPending_UnknownItemIs404(t *testing.T) {
	app, _, _, _ := applyPendingApp(t, shippyUpstream())
	status, _ := postApply(t, app, uuid.NewString())
	require.Equal(t, fiber.StatusNotFound, status)
}

func TestApplyCatalogPending_MalformedIDIs400(t *testing.T) {
	app, _, _, _ := applyPendingApp(t, shippyUpstream())
	status, _ := postApply(t, app, "not-a-uuid")
	require.Equal(t, fiber.StatusBadRequest, status)
}

func TestApplyCatalogPending_AgentGoneFromTheCatalogIsACodedRefusal(t *testing.T) {
	app, store, pending, item := applyPendingApp(t, nil)

	status, body := postApply(t, app, item.ID.String())

	require.Equal(t, fiber.StatusBadRequest, status)
	require.Equal(t, codeInvalidCatalogInput, body["code"])
	skills, _ := store.ListSkillsByAgent(context.Background(), store.agents[0].ID)
	require.Equal(t, "hand tuned body", skills[0].Content)
	left, _ := pending.ListCatalogPending(context.Background())
	require.Len(t, left, 1)
}

func TestApplyCatalogPending_NotConfiguredIs503(t *testing.T) {
	h := &Handler{}
	app := fiber.New()
	h.registerCatalogRoutes(app)
	status, _ := postApply(t, app, uuid.NewString())
	require.Equal(t, fiber.StatusServiceUnavailable, status)
}
