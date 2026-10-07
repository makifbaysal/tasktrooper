package agentcli

import (
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/antigravity"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/claudecode"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/cursor"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/opencode"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agentfs"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const snapshotDir = "agent-cli"

type Probe struct {
	BinaryPath string
	Version    string
}

type ProbeFunc func(ctx context.Context, flavor domain.AgentCLIFlavor) (Probe, error)

type ProbeBinaries struct {
	ClaudeBinary         string
	ClaudeSettingSources string
	AntigravityBinary    string
	CursorBinary         string
	OpencodeBinary       string
}

func DefaultProbe(bins ProbeBinaries) ProbeFunc {
	return func(ctx context.Context, flavor domain.AgentCLIFlavor) (Probe, error) {
		switch flavor {
		case domain.AgentCLIFlavorClaude:
			res, err := claudecode.Probe(ctx, bins.ClaudeBinary, bins.ClaudeSettingSources)
			if err != nil {
				return Probe{}, err
			}
			return Probe{BinaryPath: res.BinaryPath, Version: res.Version}, nil
		case domain.AgentCLIFlavorAntigravity:
			res, err := antigravity.Probe(ctx, bins.AntigravityBinary)
			if err != nil {
				return Probe{}, err
			}
			return Probe{BinaryPath: res.BinaryPath, Version: res.Version}, nil
		case domain.AgentCLIFlavorCursor:
			res, err := cursor.Probe(ctx, bins.CursorBinary)
			if err != nil {
				return Probe{}, err
			}
			return Probe{BinaryPath: res.BinaryPath, Version: res.Version}, nil
		case domain.AgentCLIFlavorOpencode:
			res, err := opencode.Probe(ctx, bins.OpencodeBinary)
			if err != nil {
				return Probe{}, err
			}
			return Probe{BinaryPath: res.BinaryPath, Version: res.Version}, nil
		default:
			return Probe{}, fmt.Errorf("no probe exists for the %s CLI on this server", flavor)
		}
	}
}

type ModelsFunc func(ctx context.Context, flavor domain.AgentCLIFlavor) ([]domain.LLMModelOption, error)

func DefaultModels(bins ProbeBinaries) ModelsFunc {
	return func(ctx context.Context, flavor domain.AgentCLIFlavor) ([]domain.LLMModelOption, error) {
		switch flavor {
		case domain.AgentCLIFlavorClaude:
			return domain.ClaudeCodeModels(), nil
		case domain.AgentCLIFlavorAntigravity:
			return antigravity.Models(ctx, bins.AntigravityBinary)
		case domain.AgentCLIFlavorCursor:
			return cursor.Models(ctx, bins.CursorBinary)
		case domain.AgentCLIFlavorOpencode:
			return opencode.Models(ctx, bins.OpencodeBinary)
		default:
			return nil, fmt.Errorf("no model catalog exists for the %s CLI on this server", flavor)
		}
	}
}

type Deps struct {
	Store         port.AgentCLIStore
	Catalog       port.CatalogStore
	WorkspaceRoot string
	Probe         ProbeFunc
	Models        ModelsFunc
	// Runtimes moves agents onto a connected CLI whenever the set of connected
	// CLIs changes. Optional.
	Runtimes AgentRuntimeReconciler
}

// AgentRuntimeReconciler is catalog.Service's ReconcileAgentRuntimes.
type AgentRuntimeReconciler interface {
	ReconcileAgentRuntimes(ctx context.Context, connected []domain.LLMProviderType) (int, error)
}

type Service struct {
	store         port.AgentCLIStore
	catalog       port.CatalogStore
	workspaceRoot string
	probe         ProbeFunc
	models        ModelsFunc
	runtimes      AgentRuntimeReconciler
}

func NewService(deps Deps) *Service {
	probe := deps.Probe
	if probe == nil {
		probe = DefaultProbe(ProbeBinaries{})
	}
	models := deps.Models
	if models == nil {
		models = DefaultModels(ProbeBinaries{})
	}
	return &Service{
		store:         deps.Store,
		catalog:       deps.Catalog,
		workspaceRoot: strings.TrimSpace(deps.WorkspaceRoot),
		runtimes:      deps.Runtimes,
		probe:         probe,
		models:        models,
	}
}

func (s *Service) ModelsFor(ctx context.Context, provider domain.LLMProviderType) ([]domain.LLMModelOption, bool, error) {
	flavor, ok := domain.AgentCLIFlavorFor(provider)
	if !ok {
		return nil, false, nil
	}
	opts, err := s.models(ctx, flavor)
	return opts, true, err
}

func (s *Service) State(ctx context.Context) (domain.AgentCLIState, error) {
	conns, err := s.store.List(ctx)
	if err != nil {
		return domain.AgentCLIState{}, err
	}
	connectedFlavors := make(map[domain.AgentCLIFlavor]bool, len(conns))
	for _, conn := range conns {
		connectedFlavors[conn.Flavor] = true
	}

	out := domain.AgentCLIState{
		Connections: conns,
		Flavors:     make([]domain.AgentCLIFlavorView, 0, len(domain.AgentCLIFlavors())),
	}
	for _, flavor := range domain.AgentCLIFlavors() {
		provider, _ := domain.AgentCLIProviderFor(flavor)
		label := string(provider)
		if def, defOK := domain.LLMProviderDefinitionFor(provider); defOK && def.Label != "" {
			label = def.Label
		}
		out.Flavors = append(out.Flavors, domain.AgentCLIFlavorView{
			Flavor:       flavor,
			ProviderType: provider,
			Label:        label,
			Available:    domain.ProviderAvailable(provider),
			Connected:    connectedFlavors[flavor],
		})
	}
	return out, nil
}

func (s *Service) Connected(ctx context.Context, flavor domain.AgentCLIFlavor) (*domain.AgentCLIConnection, error) {
	conn, ok, err := s.store.Get(ctx, flavor)
	if err != nil || !ok {
		return nil, err
	}
	return &conn, nil
}

func (s *Service) Connect(ctx context.Context, flavor domain.AgentCLIFlavor) (domain.AgentCLIState, error) {
	if !domain.ValidAgentCLIFlavor(flavor) {
		return domain.AgentCLIState{}, fmt.Errorf("unknown agent cli flavor: %s", flavor)
	}
	provider, _ := domain.AgentCLIProviderFor(flavor)

	if !domain.ProviderAvailable(provider) {
		return domain.AgentCLIState{}, domain.ErrUnavailableProvider(provider)
	}

	probe, err := s.probe(ctx, flavor)
	if err != nil {
		return domain.AgentCLIState{}, err
	}

	root, agents, skills, err := s.snapshotCatalog(ctx, flavor)
	if err != nil {
		return domain.AgentCLIState{}, err
	}

	if err := s.store.Set(ctx, domain.AgentCLIConnection{
		Flavor:        flavor,
		ProviderType:  provider,
		BinaryPath:    probe.BinaryPath,
		BinaryVersion: probe.Version,
		CatalogPath:   root,
		AgentCount:    agents,
		SkillCount:    skills,
	}); err != nil {
		return domain.AgentCLIState{}, err
	}
	s.reconcileRuntimes(ctx)
	return s.State(ctx)
}

func (s *Service) Disconnect(ctx context.Context, flavor domain.AgentCLIFlavor) (domain.AgentCLIState, error) {
	if !domain.ValidAgentCLIFlavor(flavor) {
		return domain.AgentCLIState{}, fmt.Errorf("unknown agent cli flavor: %s", flavor)
	}
	if err := s.store.Clear(ctx, flavor); err != nil {
		return domain.AgentCLIState{}, err
	}
	if root, err := s.snapshotRoot(flavor); err == nil {
		_ = os.RemoveAll(root)
	}
	s.reconcileRuntimes(ctx)
	return s.State(ctx)
}

// reconcileRuntimes never fails the connect: the CLI is connected either way,
// and an agent left on its old runtime can still be moved by hand.
func (s *Service) reconcileRuntimes(ctx context.Context) {
	if s.runtimes == nil {
		return
	}
	conns, err := s.store.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("agent cli: could not list connections to reconcile agent runtimes")
		return
	}
	providers := make([]domain.LLMProviderType, 0, len(conns))
	for _, conn := range conns {
		providers = append(providers, conn.ProviderType)
	}
	moved, err := s.runtimes.ReconcileAgentRuntimes(ctx, providers)
	if err != nil {
		log.Warn().Err(err).Msg("agent cli: reconciling agent runtimes failed")
		return
	}
	if moved > 0 {
		log.Info().Int("moved", moved).Msg("agent cli: agents moved onto a connected CLI")
	}
}

// ConnectedProviders lists the provider types whose CLI is currently connected,
// so a caller reconciling agent runtimes after some other provider change can
// pass them the same set this service reconciles with.
func (s *Service) ConnectedProviders(ctx context.Context) ([]domain.LLMProviderType, error) {
	conns, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	providers := make([]domain.LLMProviderType, 0, len(conns))
	for _, conn := range conns {
		providers = append(providers, conn.ProviderType)
	}
	return providers, nil
}

func (s *Service) snapshotRoot(flavor domain.AgentCLIFlavor) (string, error) {
	if s.workspaceRoot == "" {
		return "", errors.New("this server has no workspace root configured, so there is nowhere it owns to write the agent catalog")
	}
	root, err := workspace.ResolveRoot(s.workspaceRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, snapshotDir, string(flavor)), nil
}

func (s *Service) snapshotCatalog(ctx context.Context, flavor domain.AgentCLIFlavor) (root string, agentCount, skillCount int, err error) {

	root, err = s.snapshotRoot(flavor)
	if err != nil {
		return "", 0, 0, err
	}
	fsFlavor := agentfs.Flavor(flavor)
	if !agentfs.KnownFlavor(fsFlavor) {
		return "", 0, 0, fmt.Errorf("no catalog renderer exists for the %s CLI", flavor)
	}

	agents, err := s.catalog.ListAgents(ctx)
	if err != nil {
		return "", 0, 0, fmt.Errorf("agents could not be listed: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", 0, 0, fmt.Errorf("catalog directory %s could not be created: %w", root, err)
	}

	kept := make(map[string]struct{}, len(agents))
	for _, agent := range agents {

		if !agent.Enabled {
			continue
		}
		skills, err := s.catalog.ListSkillsByAgent(ctx, agent.ID)
		if err != nil {
			return "", 0, 0, fmt.Errorf("skills for agent %q could not be read: %w", agent.Name, err)
		}
		enabled := make([]domain.Skill, 0, len(skills))
		for _, sk := range skills {
			if sk.Enabled {
				enabled = append(enabled, sk)
			}
		}
		stacks, err := s.catalog.ListTechStacksByAgent(ctx, agent.ID)
		if err != nil {
			return "", 0, 0, fmt.Errorf("tech stacks for agent %q could not be read: %w", agent.Name, err)
		}
		rules, err := s.catalog.ListEnabledRulesByAgent(ctx, agent.ID)
		if err != nil {
			return "", 0, 0, fmt.Errorf("rules for agent %q could not be read: %w", agent.Name, err)
		}
		ruleTexts := make([]string, 0, len(rules))
		for _, rule := range rules {
			ruleTexts = append(ruleTexts, rule.Content)
		}

		dir := agent.ID.String()
		kept[dir] = struct{}{}
		if _, err := agentfs.Materialize(filepath.Join(root, dir), fsFlavor, agentfs.Bundle{
			Agent:      agent,
			Skills:     enabled,
			TechStacks: stacks,
			Rules:      ruleTexts,
		}); err != nil {
			return "", 0, 0, fmt.Errorf("catalog for agent %q could not be written: %w", agent.Name, err)
		}
		agentCount++
		skillCount += len(enabled)
	}

	if err := pruneAgentDirs(root, kept); err != nil {
		return "", 0, 0, err
	}
	return root, agentCount, skillCount, nil
}

func pruneAgentDirs(root string, kept map[string]struct{}) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("catalog directory %s could not be read: %w", root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, keep := kept[entry.Name()]; keep {
			continue
		}
		if err := agentfs.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return fmt.Errorf("stale catalog %s could not be removed: %w", entry.Name(), err)
		}
	}
	return nil
}
