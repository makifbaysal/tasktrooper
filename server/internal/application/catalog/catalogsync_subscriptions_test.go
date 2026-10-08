package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func subscriptionSyncFixture(t *testing.T) (*memCatalogStore, *memSyncStore, *memBoardConfigStore, *Service, string) {
	t.Helper()
	store := newMemCatalogStore()
	syncStore := newMemSyncStore()
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	dir := t.TempDir()
	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\nsubscriptions:\n    - todo\n    - ready_for_qa\n",
		"you are the shipper\n",
		nil, nil,
	)
	svc := NewService(store, fixingLLMClient{}, "")
	svc.SetBoardConfigStore(board)
	return store, syncStore, board, svc, dir
}

func TestSyncCatalog_AdoptedAgentGetsItsCatalogColumns(t *testing.T) {
	store, syncStore, board, svc, dir := subscriptionSyncFixture(t)
	existing, err := store.CreateAgent(context.Background(), domain.Agent{
		Name: "Shippy", SubagentType: "reviewer", Description: "ships things",
		SystemPrompt: "hand edited prompt",
	})
	require.NoError(t, err)

	_, err = svc.SyncFromCatalog(context.Background(), &catalogrepo.Reader{Source: dir}, syncStore)
	require.NoError(t, err)

	adopted := agentByName(t, store, "Shippy")
	require.Equal(t, existing.ID, adopted.ID, "the same-named agent must be adopted, not duplicated")
	assert.False(t, adopted.AutoPullAgentUpdates, "a hand-edited agent still parks its prompt update")
	assert.ElementsMatch(t, []string{"todo", "ready_for_qa"}, board.subs[adopted.ID],
		"adoption must still wire the agent to the columns it is supposed to listen on")
}

func TestSyncCatalog_ParkedUpdateStillWiresColumns(t *testing.T) {
	store, syncStore, board, svc, dir := subscriptionSyncFixture(t)
	existing, err := store.CreateAgent(context.Background(), domain.Agent{
		Name: "Shippy", SubagentType: "reviewer", Description: "ships things",
		SystemPrompt: "hand edited prompt",
		CatalogSlug:  "shippy", CatalogEtag: "stale", AutoPullAgentUpdates: false,
	})
	require.NoError(t, err)

	res, err := svc.SyncFromCatalog(context.Background(), &catalogrepo.Reader{Source: dir}, syncStore)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Skipped, "the prompt update is parked")

	assert.ElementsMatch(t, []string{"todo", "ready_for_qa"}, board.subs[existing.ID],
		"auto_pull gates prompt content, not dispatch wiring")
}

func TestSyncCatalog_ColumnClaimedByAnotherAgentIsNotStolen(t *testing.T) {
	store, syncStore, board, svc, dir := subscriptionSyncFixture(t)
	incumbent := uuid.New()
	board.subs[incumbent] = []string{"ready_for_qa"}
	existing, err := store.CreateAgent(context.Background(), domain.Agent{
		Name: "Shippy", SubagentType: "reviewer", Description: "ships things",
		SystemPrompt: "hand edited prompt",
	})
	require.NoError(t, err)

	_, err = svc.SyncFromCatalog(context.Background(), &catalogrepo.Reader{Source: dir}, syncStore)
	require.NoError(t, err)

	assert.Equal(t, []string{"todo"}, board.subs[existing.ID], "todo is an assignee-only queue: joining it takes no one's seat")
	assert.Equal(t, []string{"ready_for_qa"}, board.subs[incumbent])
}
