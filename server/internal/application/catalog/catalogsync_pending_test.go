package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The install the bug report describes: only the local embedder, so every chat — the LLM merge included — fails.
type embedOnlyLLMClient struct {
	mu         sync.Mutex
	chats      int
	embedError error
}

func (c *embedOnlyLLMClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chats++
	return domain.AgentResponse{}, errors.New("no chat-capable model configured")
}

func (c *embedOnlyLLMClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, errors.New("no chat-capable model configured")
}

func (c *embedOnlyLLMClient) Models(ctx context.Context) ([]string, error) { return nil, nil }

func (c *embedOnlyLLMClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.embedError
}

func (c *embedOnlyLLMClient) chatCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chats
}

type memVersionStore struct {
	mu   sync.Mutex
	rows []domain.CatalogVersion
}

func (m *memVersionStore) AppendVersion(ctx context.Context, v domain.CatalogVersion) (domain.CatalogVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v.ID = uuid.New()
	v.Version = 1
	for _, r := range m.rows {
		if r.TargetKind == v.TargetKind && r.TargetID == v.TargetID && r.Version >= v.Version {
			v.Version = r.Version + 1
		}
	}
	m.rows = append(m.rows, v)
	return v, nil
}

func (m *memVersionStore) ListVersions(ctx context.Context, targetKind string, targetID uuid.UUID, limit int) ([]domain.CatalogVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.CatalogVersion
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].TargetKind == targetKind && m.rows[i].TargetID == targetID {
			out = append(out, m.rows[i])
		}
	}
	return out, nil
}

func (m *memVersionStore) GetVersion(ctx context.Context, targetKind string, targetID uuid.UUID, version int) (domain.CatalogVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.TargetKind == targetKind && r.TargetID == targetID && r.Version == version {
			return r, nil
		}
	}
	return domain.CatalogVersion{}, errors.New("version not found")
}

func embedOnlyService(store *memCatalogStore) (*Service, *embedOnlyLLMClient, *memVersionStore) {
	llm := &embedOnlyLLMClient{}
	versions := &memVersionStore{}
	svc := NewService(store, llm, "")
	svc.SetVersionStore(versions)
	return svc, llm, versions
}

func shippyDef(t *testing.T, reader *catalogrepo.Reader) domain.UpstreamAgent {
	t.Helper()
	defs, _, err := reader.ReadCatalog(context.Background())
	require.NoError(t, err)
	require.Len(t, defs, 1)
	return defs[0]
}

func pendingItems(t *testing.T, syncStore *memSyncStore) []domain.CatalogPending {
	t.Helper()
	items, err := syncStore.ListCatalogPending(context.Background())
	require.NoError(t, err)
	return items
}

// A pre-catalog install: the agent already carries the catalog slug and the current fingerprint, and its skill row was seeded with
// no catalog_sha and no history.
func legacySeedFixture(t *testing.T, seededBody string) (*memCatalogStore, *memSyncStore, *catalogrepo.Reader, domain.Agent, domain.Skill) {
	t.Helper()
	ctx := context.Background()
	store, syncStore, _, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	def := shippyDef(t, reader)
	agent, err := store.CreateAgent(ctx, domain.Agent{
		Name: "Shippy", SubagentType: "reviewer", SystemPrompt: def.SystemPrompt, Enabled: true,
		CatalogSlug: "shippy", CatalogEtag: def.Etag, AutoPullAgentUpdates: true, KeepSkillsUpdated: true,
	})
	require.NoError(t, err)
	seeded, err := store.CreateSkill(ctx, domain.Skill{AgentID: agent.ID, Name: "ship", Content: seededBody, Enabled: true})
	require.NoError(t, err)
	return store, syncStore, reader, agent, seeded
}

func TestSyncCatalog_NewUpstreamSkillLandsOnAnAgentPastTheEvolutionBudget(t *testing.T) {
	ctx := context.Background()
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	_, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	agent := agentByName(t, store, "Shippy")
	for i := 0; i < 30; i++ {
		_, err := store.CreateSkill(ctx, domain.Skill{AgentID: agent.ID, Name: fmt.Sprintf("learned-%d", i), Content: "learned", Enabled: true})
		require.NoError(t, err)
	}

	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\n",
		"you are the shipper\n",
		map[string]string{
			"ship":   skillDoc("ship", "ship it", "release", "original ship body"),
			"deploy": skillDoc("deploy", "deploy it", "release", "deploy body"),
		},
		nil,
	)
	res, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)

	sk, err := store.GetSkillByAgentAndName(ctx, agent.ID, "deploy")
	require.NoError(t, err, "a catalog skill must not be capped by max_skills_per_agent")
	require.Equal(t, "deploy body", sk.Content)
	require.Equal(t, 1, res.Created)
	require.Empty(t, pendingItems(t, syncStore))
}

func TestSyncCatalog_SkillSetAsideIsRetriedWhileTheFingerprintMatches(t *testing.T) {
	ctx := context.Background()
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	_, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	agent := agentByName(t, store, "Shippy")
	sk, err := store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.NoError(t, err)
	require.NoError(t, store.DeleteSkill(ctx, sk.ID))
	require.NoError(t, syncStore.AppendCatalogPending(ctx, domain.CatalogPending{
		AgentSlug: "shippy", AgentName: "Shippy", Kind: domain.CatalogPendingKindSkill, Name: "ship",
		Action: domain.CatalogPendingActionCreate, Reason: "skill budget dolu (25)",
	}))

	res, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)

	got, err := store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.NoError(t, err, "an unchanged fingerprint must not lock a set-aside skill out")
	require.Equal(t, "original ship body", got.Content)
	require.Equal(t, 1, res.Created)
	require.Empty(t, pendingItems(t, syncStore), "the stale pending row goes once the skill lands")
	state, _ := syncStore.GetCatalogSyncState(ctx)
	require.Zero(t, state.PendingCount)
}

func TestSyncCatalog_UnchangedPassLeavesARemovedStackRemoved(t *testing.T) {
	ctx := context.Background()
	store, syncStore, svc, dir := syncFixture(t)
	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\ntech_stacks:\n    - name: Go\n",
		"you are the shipper\n",
		map[string]string{"ship": skillDoc("ship", "ship it", "release", "original ship body")},
		nil,
	)
	reader := &catalogrepo.Reader{Source: dir}
	_, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	agent := agentByName(t, store, "Shippy")
	stacks, _ := store.ListTechStacksByAgent(ctx, agent.ID)
	require.Len(t, stacks, 1)
	require.NoError(t, svc.DeleteTechStackForAgent(ctx, agent.ID, stacks[0].ID))

	_, err = svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)

	stacks, _ = store.ListTechStacksByAgent(ctx, agent.ID)
	require.Empty(t, stacks)
}

func TestSyncCatalog_FailedPassLeavesTheEtagForTheNextPass(t *testing.T) {
	ctx := context.Background()
	store, syncStore, _, dir := syncFixture(t)
	svc, llm, _ := embedOnlyService(store)
	reader := &catalogrepo.Reader{Source: dir}
	_, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	before := agentByName(t, store, "Shippy").CatalogEtag

	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\n",
		"you are the shipper\n",
		map[string]string{"deploy": skillDoc("deploy", "deploy it", "release", "deploy body")},
		nil,
	)
	llm.embedError = errors.New("embedder still starting")
	_, err = svc.SyncFromCatalog(ctx, reader, syncStore)
	require.Error(t, err)
	require.Equal(t, before, agentByName(t, store, "Shippy").CatalogEtag)

	llm.embedError = nil
	_, err = svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	agent := agentByName(t, store, "Shippy")
	require.Equal(t, shippyDef(t, reader).Etag, agent.CatalogEtag)
	_, err = store.GetSkillByAgentAndName(ctx, agent.ID, "deploy")
	require.NoError(t, err)
}

func TestSyncCatalog_UnchangedPassDoesNotRetryAParkedMerge(t *testing.T) {
	ctx := context.Background()
	store, syncStore, _, dir := syncFixture(t)
	svc, llm, _ := embedOnlyService(store)
	reader := &catalogrepo.Reader{Source: dir}
	_, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	agent := agentByName(t, store, "Shippy")
	localEditSkill(t, store, agent.ID, "ship", "local hacked body")
	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\n",
		"you are the shipper\n",
		map[string]string{"ship": skillDoc("ship", "ship it", "release", "upstream new body")},
		nil,
	)

	_, err = svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	require.Equal(t, 1, llm.chatCount())
	items := pendingItems(t, syncStore)
	require.Len(t, items, 1)
	require.Equal(t, domain.CatalogPendingActionMerge, items[0].Action)

	res, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	require.Equal(t, 1, llm.chatCount(), "an unchanged catalog must not re-run the LLM merge")
	require.Equal(t, domain.CatalogSyncResult{RepoRef: res.RepoRef}, *res)
	require.Len(t, pendingItems(t, syncStore), 1)
	sk, _ := store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.Equal(t, "local hacked body", sk.Content)
}

func TestSyncCatalog_UserEditOnTheCurrentRevisionIsNotMerged(t *testing.T) {
	ctx := context.Background()
	store, syncStore, _, dir := syncFixture(t)
	svc, llm, _ := embedOnlyService(store)
	reader := &catalogrepo.Reader{Source: dir}
	_, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	agent := agentByName(t, store, "Shippy")
	sk, err := store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.NoError(t, err)

	_, err = svc.UpdateSkillForAgent(ctx, agent.ID, sk.ID, domain.UpdateSkillRequest{
		Name: "ship", Description: sk.Description, Category: sk.Category, Content: "my own ship body", Enabled: true,
	})
	require.NoError(t, err)
	edited, _ := store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.Equal(t, hashContent("original ship body"), edited.CatalogSha, "an edit keeps the revision it was made on")

	res, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	require.Zero(t, llm.chatCount())
	require.Equal(t, domain.CatalogSyncResult{RepoRef: res.RepoRef}, *res)
	after, _ := store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.Equal(t, "my own ship body", after.Content)
}

func TestSyncCatalog_LegacySeedSkillTakesUpstreamWithoutAnLLM(t *testing.T) {
	ctx := context.Background()
	store, syncStore, reader, agent, _ := legacySeedFixture(t, "seeded ship body")
	require.NoError(t, syncStore.AppendCatalogPending(ctx, domain.CatalogPending{
		AgentSlug: "shippy", AgentName: "Shippy", Kind: domain.CatalogPendingKindSkill, Name: "ship",
		Action: domain.CatalogPendingActionMerge, Reason: "LLM merge basarisiz: no chat-capable model configured",
	}))
	svc, llm, versions := embedOnlyService(store)

	res, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)

	sk, err := store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.NoError(t, err)
	require.Equal(t, "original ship body", sk.Content)
	require.Equal(t, hashContent("original ship body"), sk.CatalogSha)
	require.Zero(t, llm.chatCount())
	require.Equal(t, 1, res.Updated)
	require.Empty(t, pendingItems(t, syncStore))
	history, _ := versions.ListVersions(ctx, domain.CatalogVersionKindSkill, sk.ID, 0)
	require.NotEmpty(t, history)
	require.Equal(t, domain.CatalogVersionSourceUpstream, history[0].Source)
}

func TestSyncCatalog_EditedLegacySkillIsParkedAndApplyTakesUpstream(t *testing.T) {
	ctx := context.Background()
	store, syncStore, reader, agent, seeded := legacySeedFixture(t, "hand tuned ship body")
	stale := agentByName(t, store, "Shippy")
	stale.CatalogEtag = ""
	_, err := store.UpdateAgent(ctx, stale)
	require.NoError(t, err)
	svc, llm, versions := embedOnlyService(store)
	_, err = versions.AppendVersion(ctx, domain.CatalogVersion{
		AgentID: agent.ID, TargetKind: domain.CatalogVersionKindSkill, TargetID: seeded.ID,
		Action: domain.CatalogVersionActionUpdate, Name: "ship", Content: "hand tuned ship body",
		Source: domain.CatalogVersionSourceUser,
	})
	require.NoError(t, err)

	_, err = svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	require.Equal(t, 1, llm.chatCount())
	sk, _ := store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.Equal(t, "hand tuned ship body", sk.Content, "a user edit must not be overwritten without the user")
	items := pendingItems(t, syncStore)
	require.Len(t, items, 1)
	require.True(t, strings.HasPrefix(items[0].Reason, "LLM merge basarisiz"), items[0].Reason)

	require.NoError(t, svc.ApplyCatalogPending(ctx, reader, syncStore, items[0].ID))
	sk, _ = store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.Equal(t, "original ship body", sk.Content)
	require.Equal(t, hashContent("original ship body"), sk.CatalogSha)
	require.Empty(t, pendingItems(t, syncStore))
}

func TestApplyCatalogPending_AgentUpdateTakesUpstreamAndKeepsTheToggle(t *testing.T) {
	ctx := context.Background()
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	_, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	agent := agentByName(t, store, "Shippy")
	off := false
	_, err = svc.UpdateAgent(ctx, agent.ID, domain.UpdateAgentRequest{
		Name: "Shippy", SystemPrompt: agent.SystemPrompt, SubagentType: agent.SubagentType,
		Enabled: true, AutoPullAgentUpdates: &off,
	})
	require.NoError(t, err)
	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\n",
		"you are the new shipper\n",
		nil, nil,
	)
	_, err = svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	items := pendingItems(t, syncStore)
	require.Len(t, items, 1)
	require.Equal(t, domain.CatalogPendingKindAgent, items[0].Kind)

	require.NoError(t, svc.ApplyCatalogPending(ctx, reader, syncStore, items[0].ID))

	def := shippyDef(t, reader)
	got := agentByName(t, store, "Shippy")
	require.Equal(t, def.SystemPrompt, got.SystemPrompt)
	require.Equal(t, def.Etag, got.CatalogEtag)
	require.False(t, got.AutoPullAgentUpdates)
	require.Empty(t, pendingItems(t, syncStore))

	res, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	require.Zero(t, res.Skipped)
	require.Empty(t, pendingItems(t, syncStore))
}

func TestApplyCatalogPending_DeletesASkillTheCatalogDropped(t *testing.T) {
	ctx := context.Background()
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	_, err := svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	agent := agentByName(t, store, "Shippy")
	localEditSkill(t, store, agent.ID, "ship", "local hacked body")
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "agents", "shippy", "skills")))
	_, err = svc.SyncFromCatalog(ctx, reader, syncStore)
	require.NoError(t, err)
	items := pendingItems(t, syncStore)
	require.Len(t, items, 1)
	require.Equal(t, domain.CatalogPendingActionDelete, items[0].Action)

	require.NoError(t, svc.ApplyCatalogPending(ctx, reader, syncStore, items[0].ID))

	_, err = store.GetSkillByAgentAndName(ctx, agent.ID, "ship")
	require.Error(t, err)
	require.Empty(t, pendingItems(t, syncStore))
}

func TestApplyCatalogPending_UnknownItemIsNotFound(t *testing.T) {
	_, syncStore, svc, dir := syncFixture(t)
	err := svc.ApplyCatalogPending(context.Background(), &catalogrepo.Reader{Source: dir}, syncStore, uuid.New())
	require.ErrorIs(t, err, ErrCatalogPendingNotFound)
}
