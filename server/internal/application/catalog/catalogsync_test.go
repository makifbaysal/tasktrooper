package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeAgentDir(t *testing.T, root, slug, manifest, prompt string, skills, rules map[string]string) {
	t.Helper()
	base := filepath.Join(root, "agents", slug)
	writeFile(t, filepath.Join(base, "catalog.yaml"), manifest)
	writeFile(t, filepath.Join(base, "prompt.md"), prompt)
	for name, content := range skills {
		writeFile(t, filepath.Join(base, "skills", name, "SKILL.md"), content)
	}
	for name, content := range rules {
		writeFile(t, filepath.Join(base, "rules", name+".md"), content)
	}
}

func skillDoc(name, desc, category, body string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\ncategory: " + category + "\n---\n" + body
}

// Answers every merge chat with a plausible merged SKILL.md so the merge path runs end to end without a real provider.
type fixingLLMClient struct{}

func (fixingLLMClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{
		Message: domain.Message{Content: skillDoc("ship", "merged desc", "owasp", "merged body")},
	}, nil
}

func (fixingLLMClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (fixingLLMClient) Models(ctx context.Context) ([]string, error) { return nil, nil }
func (fixingLLMClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	return nil, nil
}

type memSyncStore struct {
	mu      sync.Mutex
	state   domain.CatalogSyncState
	pending []domain.CatalogPending
}

func newMemSyncStore() *memSyncStore { return &memSyncStore{} }

func (m *memSyncStore) GetCatalogSyncState(ctx context.Context) (domain.CatalogSyncState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, nil
}

func (m *memSyncStore) SaveCatalogSyncState(ctx context.Context, state domain.CatalogSyncState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
	return nil
}

func (m *memSyncStore) AppendCatalogPending(ctx context.Context, pending domain.CatalogPending) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pending {
		if p.AgentSlug == pending.AgentSlug && p.Kind == pending.Kind &&
			p.Name == pending.Name && p.Action == pending.Action && p.Reason == pending.Reason {
			return nil
		}
	}
	if pending.ID == uuid.Nil {
		pending.ID = uuid.New()
	}
	m.pending = append(m.pending, pending)
	return nil
}

func (m *memSyncStore) ListCatalogPending(ctx context.Context) ([]domain.CatalogPending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.CatalogPending(nil), m.pending...), nil
}

func (m *memSyncStore) DeleteCatalogPending(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.pending {
		if p.ID == id {
			m.pending = append(m.pending[:i], m.pending[i+1:]...)
			return nil
		}
	}
	return nil
}

func syncFixture(t *testing.T) (*memCatalogStore, *memSyncStore, *Service, string) {
	t.Helper()
	store := newMemCatalogStore()
	syncStore := newMemSyncStore()
	dir := t.TempDir()
	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\n",
		"you are the shipper\n",
		map[string]string{
			"ship": skillDoc("ship", "ship it", "release", "original ship body"),
		},
		map[string]string{
			"no-skip": "---\nname: NoSkip\npriority: 10\n---\nnever skip a ship\n",
		},
	)
	svc := NewService(store, fixingLLMClient{}, "")
	return store, syncStore, svc, dir
}

func localEditSkill(t *testing.T, store *memCatalogStore, agentID uuid.UUID, name, content string) {
	t.Helper()
	sk, err := store.GetSkillByAgentAndName(context.Background(), agentID, name)
	if err != nil {
		t.Fatalf("GetSkillByAgentAndName: %v", err)
	}
	sk.Content = content
	if _, err := store.UpdateSkill(context.Background(), sk); err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}
}

func TestSyncCatalog_NewCatalogCreatesAgentSkillAndRule(t *testing.T) {
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}

	res, err := svc.SyncFromCatalog(context.Background(), reader, syncStore)
	if err != nil {
		t.Fatalf("SyncFromCatalog: %v", err)
	}

	agent := agentByName(t, store, "Shippy")
	if agent.CatalogSlug != "shippy" || agent.CatalogEtag == "" {
		t.Fatalf("agent not stamped with catalog identity: %+v", agent)
	}
	if !agent.AutoPullAgentUpdates || !agent.KeepSkillsUpdated {
		t.Fatalf("new agent must be born connected to the catalog")
	}
	sk, err := store.GetSkillByAgentAndName(context.Background(), agent.ID, "ship")
	if err != nil {
		t.Fatalf("skill not created: %v", err)
	}
	if sk.Content != "original ship body" {
		t.Fatalf("skill content = %q", sk.Content)
	}
	if sk.CatalogSha == "" || sk.CatalogSha != hashContent(sk.Content) {
		t.Fatalf("skill sha marker not stamped: %q", sk.CatalogSha)
	}
	rules, err := store.ListRulesByAgent(context.Background(), agent.ID)
	if err != nil {
		t.Fatalf("ListRulesByAgent: %v", err)
	}
	if len(rules) != 1 || rules[0].Name != "NoSkip" {
		t.Fatalf("rule not created: %+v", rules)
	}

	if res.Created != 2 || res.Updated != 0 || res.Merged != 0 || res.Skipped != 0 {
		t.Fatalf("result counts = %+v", res)
	}
	if state, _ := syncStore.GetCatalogSyncState(context.Background()); state.RepoRef != "dir:"+dir || state.LastError != "" {
		t.Fatalf("sync state = %+v", state)
	}
	if items, _ := syncStore.ListCatalogPending(context.Background()); len(items) != 0 {
		t.Fatalf("unexpected pending: %+v", items)
	}
}

func TestSyncCatalog_UnchangedCatalogIsANoOp(t *testing.T) {
	_, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	if _, err := svc.SyncFromCatalog(context.Background(), reader, syncStore); err != nil {
		t.Fatalf("sync 1: %v", err)
	}

	res, err := svc.SyncFromCatalog(context.Background(), reader, syncStore)
	if err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	if res.Created != 0 || res.Updated != 0 || res.Merged != 0 || res.Skipped != 0 {
		t.Fatalf("second sync touched something: %+v", res)
	}
}

func TestSyncCatalog_UpstreamSkillChangeAppliedWhenLocalUntouched(t *testing.T) {
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	if _, err := svc.SyncFromCatalog(context.Background(), reader, syncStore); err != nil {
		t.Fatalf("sync 1: %v", err)
	}

	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\n",
		"you are the shipper\n",
		map[string]string{"ship": skillDoc("ship", "ship it", "release", "shinier ship body")},
		map[string]string{"no-skip": "---\nname: NoSkip\npriority: 10\n---\nnever skip a ship\n"},
	)

	res, err := svc.SyncFromCatalog(context.Background(), reader, syncStore)
	if err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	agent := agentByName(t, store, "Shippy")
	sk, _ := store.GetSkillByAgentAndName(context.Background(), agent.ID, "ship")
	if sk.Content != "shinier ship body" {
		t.Fatalf("skill not updated: %+v", sk)
	}
	if sk.CatalogSha != hashContent("shinier ship body") {
		t.Fatalf("skill sha not renewed: %q", sk.CatalogSha)
	}
	if res.Updated != 1 && res.Updated != 2 {
		t.Fatalf("result = %+v", res)
	}
}

func TestSyncCatalog_LocalEditWithKeepOffParksInsteadOfOverwriting(t *testing.T) {
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	if _, err := svc.SyncFromCatalog(context.Background(), reader, syncStore); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	agent := agentByName(t, store, "Shippy")
	f := false
	if _, err := svc.UpdateAgent(context.Background(), agent.ID, domain.UpdateAgentRequest{
		Name: "Shippy", SystemPrompt: agent.SystemPrompt, SubagentType: agent.SubagentType,
		Enabled: true, KeepSkillsUpdated: &f,
	}); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	localEditSkill(t, store, agent.ID, "ship", "local hacked body")

	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\n",
		"you are the shipper\n",
		map[string]string{"ship": skillDoc("ship", "ship it", "release", "upstream new body")},
		map[string]string{"no-skip": "---\nname: NoSkip\npriority: 10\n---\nnever skip a ship\n"},
	)

	res, err := svc.SyncFromCatalog(context.Background(), reader, syncStore)
	if err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	sk, _ := store.GetSkillByAgentAndName(context.Background(), agent.ID, "ship")
	if sk.Content != "local hacked body" {
		t.Fatalf("local edit overwritten: %+v", sk)
	}
	items, _ := syncStore.ListCatalogPending(context.Background())
	if len(items) != 1 || items[0].Kind != "skill" || items[0].Name != "ship" || items[0].Action != "merge" {
		t.Fatalf("pending = %+v", items)
	}
	if !strings.Contains(items[0].Reason, "keep_skills_updated kapali") {
		t.Fatalf("reason = %q", items[0].Reason)
	}
	if res.Skipped != 1 || res.Merged != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestSyncCatalog_LocalEditWithKeepOnMerges(t *testing.T) {
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	if _, err := svc.SyncFromCatalog(context.Background(), reader, syncStore); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	agent := agentByName(t, store, "Shippy")
	localEditSkill(t, store, agent.ID, "ship", "local hacked body")

	writeAgentDir(t, dir, "shippy",
		"name: Shippy\nsubagent_type: reviewer\ndescription: ships things\neffort: medium\n",
		"you are the shipper\n",
		map[string]string{"ship": skillDoc("ship", "ship it", "release", "upstream new body")},
		map[string]string{"no-skip": "---\nname: NoSkip\npriority: 10\n---\nnever skip a ship\n"},
	)

	res, err := svc.SyncFromCatalog(context.Background(), reader, syncStore)
	if err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	sk, _ := store.GetSkillByAgentAndName(context.Background(), agent.ID, "ship")
	if sk.Content != "merged body" {
		t.Fatalf("merge not applied: %+v", sk)
	}
	if sk.CatalogSha != hashContent("upstream new body") {
		t.Fatalf("merge did not stamp upstream sha: %q", sk.CatalogSha)
	}
	items, _ := syncStore.ListCatalogPending(context.Background())
	if len(items) != 0 {
		t.Fatalf("merge must not linger in pending: %+v", items)
	}
	if res.Merged != 1 {
		t.Fatalf("result = %+v", res)
	}
}

func TestSyncCatalog_UpstreamSkillRemovedDeletesUntouchedLocal(t *testing.T) {
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	if _, err := svc.SyncFromCatalog(context.Background(), reader, syncStore); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	agent := agentByName(t, store, "Shippy")

	if err := os.RemoveAll(filepath.Join(dir, "agents", "shippy", "skills")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := svc.SyncFromCatalog(context.Background(), reader, syncStore); err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	if _, err := store.GetSkillByAgentAndName(context.Background(), agent.ID, "ship"); err == nil {
		t.Fatalf("untouched local skill must be deleted when removed upstream")
	}
	items, _ := syncStore.ListCatalogPending(context.Background())
	if len(items) != 0 {
		t.Fatalf("unexpected pending after deletion: %+v", items)
	}
}

func TestSyncCatalog_RemovedSkillKeptWhenEditedLocally(t *testing.T) {
	store, syncStore, svc, dir := syncFixture(t)
	reader := &catalogrepo.Reader{Source: dir}
	if _, err := svc.SyncFromCatalog(context.Background(), reader, syncStore); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	agent := agentByName(t, store, "Shippy")
	localEditSkill(t, store, agent.ID, "ship", "local hacked body")

	if err := os.RemoveAll(filepath.Join(dir, "agents", "shippy", "skills")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := svc.SyncFromCatalog(context.Background(), reader, syncStore); err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	sk, err := store.GetSkillByAgentAndName(context.Background(), agent.ID, "ship")
	if err != nil {
		t.Fatalf("locally-edited skill must survive upstream removal")
	}
	if sk.Content != "local hacked body" {
		t.Fatalf("skill content changed: %+v", sk)
	}
	items, _ := syncStore.ListCatalogPending(context.Background())
	if len(items) != 1 || items[0].Action != "delete" {
		t.Fatalf("pending = %+v", items)
	}
}
