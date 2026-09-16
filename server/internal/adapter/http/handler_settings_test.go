package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeSettingsStoreHTTP struct {
	got             domain.AppSettings
	updateCalls     int
	updateBackend   string
	analizAppliedTo domain.AppSettings
}

func (f *fakeSettingsStoreHTTP) Get(context.Context) (domain.AppSettings, error) { return f.got, nil }

func (f *fakeSettingsStoreHTTP) Update(context.Context, domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	return f.got, nil
}

func (f *fakeSettingsStoreHTTP) UpdateAnalizAssignment(_ context.Context, backend, _, _ string) (domain.AppSettings, error) {
	f.updateCalls++
	f.updateBackend = backend
	f.got.AnalizAssigneeBackend = backend
	return f.got, nil
}

type fakeAgentCatalogHTTP struct {
	agents map[string]domain.Agent
}

func (f *fakeAgentCatalogHTTP) ListAgents(context.Context) ([]domain.Agent, error) {
	out := make([]domain.Agent, 0, len(f.agents))
	for _, a := range f.agents {
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeAgentCatalogHTTP) UpdateAgent(_ context.Context, id uuid.UUID, req domain.UpdateAgentRequest) (domain.Agent, error) {
	updated := domain.Agent{ID: id, Name: req.Name, ToolPolicy: req.ToolPolicy}
	for name, a := range f.agents {
		if a.ID == id {
			f.agents[name] = updated
		}
	}
	return updated, nil
}

func newAnalizAssignmentTestApp(store *fakeSettingsStoreHTTP, catalog *fakeAgentCatalogHTTP) (*fiber.App, *Handler) {
	svc := settings.NewService(store)
	if catalog != nil {
		svc.SetAgentCatalog(catalog)
	}
	h := &Handler{settingsSvc: svc}
	app := fiber.New()
	h.registerSettingsRoutes(app)
	return app, h
}

func putAnalizAssignment(t *testing.T, app *fiber.App, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest("PUT", "/v1/settings/analiz-assignment", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, out
}

func TestUpdateAnalizAssignmentSavesWhenAgentHasRequiredTools(t *testing.T) {
	store := &fakeSettingsStoreHTTP{}
	app, _ := newAnalizAssignmentTestApp(store, nil)

	status, out := putAnalizAssignment(t, app, map[string]any{"backend": domain.AgentSystemArchitect})

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, out)
	}
	if saved, _ := out["saved"].(bool); !saved {
		t.Fatalf("expected saved=true, got %v", out)
	}
	if store.updateCalls != 1 {
		t.Fatalf("expected the setting to be written once, got %d calls", store.updateCalls)
	}
}

func TestUpdateAnalizAssignmentRefusesAndDoesNotSaveWhenAgentMissingTools(t *testing.T) {
	agentID := uuid.New()
	catalog := &fakeAgentCatalogHTTP{agents: map[string]domain.Agent{
		domain.AgentBackendDeveloper: {
			ID:   agentID,
			Name: domain.AgentBackendDeveloper,
			ToolPolicy: domain.ToolPolicy{AllowTools: []string{
				"run_terminal", "codebase_search", "read_file",
				"claim_board_task", "move_board_task",
			}},
		},
	}}
	store := &fakeSettingsStoreHTTP{}
	app, _ := newAnalizAssignmentTestApp(store, catalog)

	status, out := putAnalizAssignment(t, app, map[string]any{"backend": domain.AgentBackendDeveloper})

	if status != fiber.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %v", status, out)
	}
	if saved, _ := out["saved"].(bool); saved {
		t.Fatalf("expected saved=false, got %v", out)
	}
	missing, _ := out["missing_tools"].(map[string]any)
	backendMissing, _ := missing["backend"].([]any)
	if len(backendMissing) == 0 {
		t.Fatalf("expected missing_tools.backend to list the gap, got %v", out)
	}
	if store.updateCalls != 0 {
		t.Fatalf("expected the setting to stay unsaved, got %d calls", store.updateCalls)
	}
}

func TestUpdateAnalizAssignmentConfirmGrantToolsSavesAndWidensPolicy(t *testing.T) {
	agentID := uuid.New()
	catalog := &fakeAgentCatalogHTTP{agents: map[string]domain.Agent{
		domain.AgentBackendDeveloper: {
			ID:   agentID,
			Name: domain.AgentBackendDeveloper,
			ToolPolicy: domain.ToolPolicy{AllowTools: []string{
				"run_terminal", "codebase_search", "read_file",
				"claim_board_task", "move_board_task",
			}},
		},
	}}
	store := &fakeSettingsStoreHTTP{}
	app, _ := newAnalizAssignmentTestApp(store, catalog)

	status, out := putAnalizAssignment(t, app, map[string]any{
		"backend":             domain.AgentBackendDeveloper,
		"confirm_grant_tools": true,
	})

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, out)
	}
	if saved, _ := out["saved"].(bool); !saved {
		t.Fatalf("expected saved=true, got %v", out)
	}
	if store.updateCalls != 1 {
		t.Fatalf("expected the setting to be written once, got %d calls", store.updateCalls)
	}
	updated := catalog.agents[domain.AgentBackendDeveloper]
	found := false
	for _, tool := range updated.ToolPolicy.AllowTools {
		if tool == "create_board_task" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected create_board_task to be granted to the agent, got %v", updated.ToolPolicy.AllowTools)
	}
}
