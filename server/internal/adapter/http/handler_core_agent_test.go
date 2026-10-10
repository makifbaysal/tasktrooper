package http

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type coreAgentStore struct {
	port.CatalogStore
	agents map[uuid.UUID]domain.Agent
}

func (s *coreAgentStore) GetAgent(_ context.Context, id uuid.UUID) (domain.Agent, error) {
	a, ok := s.agents[id]
	if !ok {
		return domain.Agent{}, errors.New("agent not found")
	}
	return a, nil
}

func (s *coreAgentStore) DeleteAgent(_ context.Context, id uuid.UUID) error {
	delete(s.agents, id)
	return nil
}

func deleteAgentStatus(t *testing.T, agents ...domain.Agent) func(uuid.UUID) int {
	t.Helper()
	store := &coreAgentStore{agents: map[uuid.UUID]domain.Agent{}}
	for _, a := range agents {
		store.agents[a.ID] = a
	}
	h := &Handler{catalogSvc: catalog.NewService(store, nil, "")}
	app := fiber.New()
	app.Delete("/admin/agents/:id", h.DeleteAgent)
	return func(id uuid.UUID) int {
		resp, err := app.Test(httptest.NewRequest("DELETE", "/admin/agents/"+id.String(), nil))
		require.NoError(t, err)
		return resp.StatusCode
	}
}

func TestDeleteAgent_CoreIs400CustomIs204MissingIs404(t *testing.T) {
	core := domain.Agent{ID: uuid.New(), Name: "Product Manager", CatalogSlug: "product-manager"}
	custom := domain.Agent{ID: uuid.New(), Name: "Mine"}
	del := deleteAgentStatus(t, core, custom)

	require.Equal(t, 400, del(core.ID))
	require.Equal(t, 204, del(custom.ID))
	require.Equal(t, 404, del(uuid.New()))
}
