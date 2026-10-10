package catalog_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Answers the two agent lookups UpdateAgent performs; the embedded interface leaves the rest unimplemented.
type agentStore struct {
	port.CatalogStore
	existing domain.Agent
	saved    domain.Agent
}

func (s *agentStore) GetAgent(_ context.Context, _ uuid.UUID) (domain.Agent, error) {
	return s.existing, nil
}

func (s *agentStore) UpdateAgent(_ context.Context, agent domain.Agent) (domain.Agent, error) {
	s.saved = agent
	return agent, nil
}

func TestUpdateAgent_ProviderSwitchDropsTheOldProvidersModels(t *testing.T) {
	id := uuid.New()
	store := &agentStore{existing: domain.Agent{
		ID:           id,
		Name:         "product-manager",
		ProviderType: "anthropic",
		Model:        "anthropic/claude-sonnet-5",
		ModelHeavy:   "anthropic/claude-opus-5",
	}}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.UpdateAgent(context.Background(), id, domain.UpdateAgentRequest{
		Name:         "product-manager",
		ProviderType: "mistral-endpoint",
		Model:        "anthropic/claude-sonnet-5",
		ModelHeavy:   "anthropic/claude-opus-5",
	})

	require.NoError(t, err)
	assert.Empty(t, store.saved.Model)
	assert.Empty(t, store.saved.ModelHeavy)
}

func TestUpdateAgent_ProviderSwitchKeepsTheModelsPickedForTheNewProvider(t *testing.T) {
	id := uuid.New()
	store := &agentStore{existing: domain.Agent{
		ID:           id,
		Name:         "product-manager",
		ProviderType: "anthropic",
		Model:        "anthropic/claude-sonnet-5",
		ModelHeavy:   "anthropic/claude-opus-5",
	}}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.UpdateAgent(context.Background(), id, domain.UpdateAgentRequest{
		Name:         "product-manager",
		ProviderType: "mistral-endpoint",
		Model:        "mistral-small-latest",
		ModelHeavy:   "mistral-large-latest",
	})

	require.NoError(t, err)
	assert.Equal(t, "mistral-small-latest", store.saved.Model)
	assert.Equal(t, "mistral-large-latest", store.saved.ModelHeavy)
}

func TestUpdateAgent_SameProviderKeepsTheModels(t *testing.T) {
	id := uuid.New()
	store := &agentStore{existing: domain.Agent{
		ID:           id,
		Name:         "product-manager",
		ProviderType: "mistral-endpoint",
		Model:        "mistral-small-latest",
		ModelHeavy:   "mistral-large-latest",
	}}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.UpdateAgent(context.Background(), id, domain.UpdateAgentRequest{
		Name:         "product-manager",
		ProviderType: "mistral-endpoint",
		Model:        "mistral-small-latest",
		ModelHeavy:   "mistral-large-latest",
	})

	require.NoError(t, err)
	assert.Equal(t, "mistral-small-latest", store.saved.Model)
	assert.Equal(t, "mistral-large-latest", store.saved.ModelHeavy)
}

type deleteStore struct {
	agentStore
	deleted []uuid.UUID
}

func (s *deleteStore) DeleteAgent(_ context.Context, id uuid.UUID) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func updateReq(name string, enabled bool) domain.UpdateAgentRequest {
	return domain.UpdateAgentRequest{Name: name, Enabled: enabled}
}

func TestUpdateAgent_CoreAgentCannotBeDisabled(t *testing.T) {
	for _, slug := range domain.CoreAgentSlugs {
		id := uuid.New()
		store := &agentStore{existing: domain.Agent{ID: id, Name: slug, CatalogSlug: slug, Enabled: true}}
		svc := catalog.NewService(store, nil, "")

		_, err := svc.UpdateAgent(context.Background(), id, updateReq(slug, false))

		require.ErrorIs(t, err, catalog.ErrInvalidInput, slug)
		assert.Empty(t, store.saved.Name, "nothing may be persisted")
	}
}

func TestUpdateAgent_CoreAgentCanBeEnabledAgain(t *testing.T) {
	id := uuid.New()
	store := &agentStore{existing: domain.Agent{ID: id, Name: "qa-agent", CatalogSlug: "qa-agent", Enabled: false}}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.UpdateAgent(context.Background(), id, updateReq("qa-agent", true))

	require.NoError(t, err)
	assert.True(t, store.saved.Enabled)
}

func TestUpdateAgent_NonCoreAgentStillToggles(t *testing.T) {
	id := uuid.New()
	store := &agentStore{existing: domain.Agent{ID: id, Name: "ui-designer", CatalogSlug: "ui-designer", Enabled: true}}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.UpdateAgent(context.Background(), id, updateReq("ui-designer", false))

	require.NoError(t, err)
	assert.False(t, store.saved.Enabled)
}

func TestDeleteAgent_CoreAgentRefused(t *testing.T) {
	id := uuid.New()
	store := &deleteStore{agentStore: agentStore{existing: domain.Agent{ID: id, Name: "security-agent", CatalogSlug: "security-agent"}}}
	svc := catalog.NewService(store, nil, "")

	err := svc.DeleteAgent(context.Background(), id)

	require.ErrorIs(t, err, catalog.ErrInvalidInput)
	assert.Empty(t, store.deleted)
}

func TestDeleteAgent_NonCoreAgentStillDeletes(t *testing.T) {
	id := uuid.New()
	store := &deleteStore{agentStore: agentStore{existing: domain.Agent{ID: id, Name: "custom", CatalogSlug: ""}}}
	svc := catalog.NewService(store, nil, "")

	require.NoError(t, svc.DeleteAgent(context.Background(), id))
	assert.Equal(t, []uuid.UUID{id}, store.deleted)
}
