package agentcli_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agentcli"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type memStore struct {
	conns  map[domain.AgentCLIFlavor]domain.AgentCLIConnection
	models map[domain.AgentCLIFlavor]string
}

func (s *memStore) DefaultModel(_ context.Context, flavor domain.AgentCLIFlavor) (string, error) {
	return s.models[flavor], nil
}

func (s *memStore) SetDefaultModel(_ context.Context, flavor domain.AgentCLIFlavor, model string) error {
	if s.models == nil {
		s.models = map[domain.AgentCLIFlavor]string{}
	}
	s.models[flavor] = model
	return nil
}

func (s *memStore) Get(_ context.Context, flavor domain.AgentCLIFlavor) (domain.AgentCLIConnection, bool, error) {
	conn, ok := s.conns[flavor]
	return conn, ok, nil
}

func (s *memStore) List(context.Context) ([]domain.AgentCLIConnection, error) {
	out := make([]domain.AgentCLIConnection, 0, len(s.conns))
	for _, conn := range s.conns {
		out = append(out, conn)
	}
	return out, nil
}

func (s *memStore) Set(_ context.Context, conn domain.AgentCLIConnection) error {
	if s.conns == nil {
		s.conns = map[domain.AgentCLIFlavor]domain.AgentCLIConnection{}
	}
	s.conns[conn.Flavor] = conn
	return nil
}

func (s *memStore) Clear(_ context.Context, flavor domain.AgentCLIFlavor) error {
	delete(s.conns, flavor)
	return nil
}

type catalogOf struct {
	port.CatalogStore
	agents []domain.Agent
	skills map[uuid.UUID][]domain.Skill
	rules  map[uuid.UUID][]domain.OrchestratorRule
}

func (c *catalogOf) ListAgents(context.Context) ([]domain.Agent, error) { return c.agents, nil }

func (c *catalogOf) ListSkillsByAgent(_ context.Context, id uuid.UUID) ([]domain.Skill, error) {
	return c.skills[id], nil
}

func (c *catalogOf) ListEnabledRulesByAgent(_ context.Context, id uuid.UUID) ([]domain.OrchestratorRule, error) {
	return c.rules[id], nil
}

func oneAgentCatalog() (*catalogOf, domain.Agent) {
	agent := domain.Agent{
		ID:           uuid.New(),
		Name:         "backend-developer",
		Description:  "Writes the server",
		SystemPrompt: "You are the backend developer.",
		ProviderType: domain.LLMProviderClaudeCode,
		Enabled:      true,
	}
	return &catalogOf{
		agents: []domain.Agent{agent},
		skills: map[uuid.UUID][]domain.Skill{agent.ID: {
			{ID: uuid.New(), Name: "Code Review", Description: "Reviews a diff", Content: "Read the diff, then report.", Enabled: true},
			{ID: uuid.New(), Name: "Retired", Description: "Not in use", Content: "Nothing.", Enabled: false},
		}},
		rules: map[uuid.UUID][]domain.OrchestratorRule{agent.ID: {
			{ID: uuid.New(), Name: "Turkish", Content: "Answer in Turkish.", Enabled: true},
		}},
	}, agent
}

func okProbe(_ context.Context, flavor domain.AgentCLIFlavor) (agentcli.Probe, error) {
	return agentcli.Probe{BinaryPath: "/usr/local/bin/" + string(flavor), Version: "1.2.3"}, nil
}

func newService(t *testing.T, store port.AgentCLIStore, catalog port.CatalogStore, probe agentcli.ProbeFunc) *agentcli.Service {
	t.Helper()
	return agentcli.NewService(agentcli.Deps{
		Store:         store,
		Catalog:       catalog,
		WorkspaceRoot: t.TempDir(),
		Probe:         probe,
	})
}

func TestConnectRefusesAnUnknownFlavor(t *testing.T) {
	store := &memStore{}
	catalog, _ := oneAgentCatalog()
	probed := false
	svc := newService(t, store, catalog, func(ctx context.Context, f domain.AgentCLIFlavor) (agentcli.Probe, error) {
		probed = true
		return okProbe(ctx, f)
	})

	_, err := svc.Connect(context.Background(), domain.AgentCLIFlavor("not-a-real-cli"))
	require.ErrorContains(t, err, "unknown agent cli flavor")
	require.False(t, probed, "an unrecognised flavor must not reach the probe")
	require.Empty(t, store.conns, "a refused connect must not leave a connection behind")
}

func TestConnectReportsAMissingBinaryDistinctly(t *testing.T) {
	store := &memStore{}
	catalog, _ := oneAgentCatalog()
	svc := newService(t, store, catalog, func(context.Context, domain.AgentCLIFlavor) (agentcli.Probe, error) {
		return agentcli.Probe{}, fmt.Errorf("claude not found on PATH: %w", domain.ErrAgentCLIBinaryMissing)
	})

	_, err := svc.Connect(context.Background(), domain.AgentCLIFlavorClaude)
	require.ErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	require.NotErrorIs(t, err, domain.ErrAgentCLIUnauthenticated,
		"the two failures must not collapse into one: they are fixed in different places")
	require.Empty(t, store.conns, "an unverified CLI must not be recorded as connected")
}

func TestConnectReportsAnUnauthenticatedBinaryDistinctly(t *testing.T) {
	store := &memStore{}
	catalog, _ := oneAgentCatalog()
	svc := newService(t, store, catalog, func(context.Context, domain.AgentCLIFlavor) (agentcli.Probe, error) {
		return agentcli.Probe{}, fmt.Errorf("please run /login: %w", domain.ErrAgentCLIUnauthenticated)
	})

	_, err := svc.Connect(context.Background(), domain.AgentCLIFlavorClaude)
	require.ErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	require.NotErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	require.Empty(t, store.conns)
}

func TestConnectWritesTheCatalogAndRecordsTheFlavor(t *testing.T) {
	store := &memStore{}
	catalog, agent := oneAgentCatalog()
	svc := newService(t, store, catalog, okProbe)

	state, err := svc.Connect(context.Background(), domain.AgentCLIFlavorClaude)
	require.NoError(t, err)

	require.Len(t, state.Connections, 1)
	conn := state.Connections[0]
	require.Equal(t, domain.AgentCLIFlavorClaude, conn.Flavor)
	require.Equal(t, domain.LLMProviderClaudeCode, conn.ProviderType)
	require.Equal(t, "1.2.3", conn.BinaryVersion, "the evidence travels with the connection")
	require.Equal(t, 1, conn.AgentCount)
	require.Equal(t, 1, conn.SkillCount, "a disabled skill is not written: the CLI would offer a role the user switched off")

	agentRoot := filepath.Join(conn.CatalogPath, agent.ID.String())
	body, err := os.ReadFile(filepath.Join(agentRoot, ".claude", "skills", "tt-code-review", "SKILL.md"))
	require.NoError(t, err, "the enabled skill has to be on disk, or the connect installed nothing")
	require.Contains(t, string(body), "Read the diff, then report.")

	entries, err := os.ReadDir(filepath.Join(agentRoot, ".claude", "skills"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "the disabled skill must not have been written")

	rule, err := os.ReadFile(filepath.Join(agentRoot, ".claude", "agents", "tt-backend-developer.md"))
	require.NoError(t, err)
	require.Contains(t, string(rule), "Answer in Turkish.")
	require.Contains(t, string(rule), "You are the backend developer.")

	require.Len(t, state.Flavors, 4)
	for _, f := range state.Flavors {
		require.Equal(t, f.Flavor == domain.AgentCLIFlavorClaude, f.Connected)
	}
}

func TestConnectingASecondFlavorLeavesTheFirstConnected(t *testing.T) {
	store := &memStore{}
	require.NoError(t, store.Set(context.Background(), domain.AgentCLIConnection{
		Flavor:       domain.AgentCLIFlavorCursor,
		ProviderType: domain.LLMProviderCursorAgent,
	}))
	catalog, _ := oneAgentCatalog()
	svc := newService(t, store, catalog, okProbe)

	state, err := svc.Connect(context.Background(), domain.AgentCLIFlavorClaude)
	require.NoError(t, err)

	require.Len(t, state.Connections, 2, "connecting one flavor must not disconnect another")
	connectedCount := 0
	for _, f := range state.Flavors {
		if f.Connected {
			connectedCount++
		}
	}
	require.Equal(t, 2, connectedCount, "both flavors stay connected")

	claude, ok, err := store.Get(context.Background(), domain.AgentCLIFlavorClaude)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, domain.AgentCLIFlavorClaude, claude.Flavor)

	cursor, ok, err := store.Get(context.Background(), domain.AgentCLIFlavorCursor)
	require.NoError(t, err)
	require.True(t, ok, "the flavor connected before must still be in the store")
	require.Equal(t, domain.AgentCLIFlavorCursor, cursor.Flavor)
}

func TestDisconnectClearsTheConnectionAndItsSnapshot(t *testing.T) {
	store := &memStore{}
	catalog, _ := oneAgentCatalog()
	svc := newService(t, store, catalog, okProbe)

	connected, err := svc.Connect(context.Background(), domain.AgentCLIFlavorClaude)
	require.NoError(t, err)
	root := connected.Connections[0].CatalogPath

	state, err := svc.Disconnect(context.Background(), domain.AgentCLIFlavorClaude)
	require.NoError(t, err)
	require.Empty(t, state.Connections)

	_, statErr := os.Stat(root)
	require.True(t, os.IsNotExist(statErr), "a disconnected CLI's catalog must not stay on disk looking current")
}

func TestConnectPrunesAgentsThatLeftTheCatalog(t *testing.T) {
	store := &memStore{}
	catalog, first := oneAgentCatalog()
	svc := newService(t, store, catalog, okProbe)

	state, err := svc.Connect(context.Background(), domain.AgentCLIFlavorClaude)
	require.NoError(t, err)
	root := state.Connections[0].CatalogPath
	require.DirExists(t, filepath.Join(root, first.ID.String()))

	catalog.agents = nil
	state, err = svc.Connect(context.Background(), domain.AgentCLIFlavorClaude)
	require.NoError(t, err)
	require.Equal(t, 0, state.Connections[0].AgentCount)
	require.NoDirExists(t, filepath.Join(root, first.ID.String()),
		"a deleted agent's skills must not sit in the snapshot looking as current as the rest")
}

func TestConnectRefusesWithoutAWorkspaceRoot(t *testing.T) {
	store := &memStore{}
	catalog, _ := oneAgentCatalog()
	svc := agentcli.NewService(agentcli.Deps{Store: store, Catalog: catalog, Probe: okProbe})

	_, err := svc.Connect(context.Background(), domain.AgentCLIFlavorClaude)
	require.ErrorContains(t, err, "workspace root")
	require.Empty(t, store.conns)
}

func (c *catalogOf) ListTechStacksByAgent(_ context.Context, _ uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}

func TestSetDefaultModelIsReportedInStateAndResolvedPerProvider(t *testing.T) {
	store := &memStore{}
	catalog, _ := oneAgentCatalog()
	svc := newService(t, store, catalog, okProbe)

	state, err := svc.SetDefaultModel(context.Background(), domain.AgentCLIFlavorOpencode, "  anthropic/claude-sonnet-5  ")
	require.NoError(t, err)
	for _, f := range state.Flavors {
		want := ""
		if f.Flavor == domain.AgentCLIFlavorOpencode {
			want = "anthropic/claude-sonnet-5"
		}
		require.Equal(t, want, f.DefaultModel, f.Flavor)
	}
	require.Equal(t, "anthropic/claude-sonnet-5", svc.DefaultModelFor(context.Background(), domain.LLMProviderOpencode))
	require.Empty(t, svc.DefaultModelFor(context.Background(), domain.LLMProviderOpenAI), "an HTTP provider has no CLI default here")

	_, err = svc.SetDefaultModel(context.Background(), domain.AgentCLIFlavorOpencode, "")
	require.NoError(t, err)
	require.Empty(t, svc.DefaultModelFor(context.Background(), domain.LLMProviderOpencode))
}

func TestSetDefaultModelRefusesAnUnknownFlavor(t *testing.T) {
	catalog, _ := oneAgentCatalog()
	svc := newService(t, &memStore{}, catalog, okProbe)
	_, err := svc.SetDefaultModel(context.Background(), domain.AgentCLIFlavor("nope"), "x")
	require.ErrorContains(t, err, "unknown agent cli flavor")
}
