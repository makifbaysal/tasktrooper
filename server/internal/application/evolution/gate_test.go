package evolution

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeManager struct {
	created  []string
	updated  []string
	deleted  []string
	rcreated []string
	rupdated []string
}

func (f *fakeManager) CreateSkillForAgent(_ context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) (domain.Skill, error) {
	f.created = append(f.created, req.Name)
	return domain.Skill{ID: uuid.New(), AgentID: agentID, Name: req.Name, Content: req.Content, Enabled: true}, nil
}

func (f *fakeManager) UpdateSkillForAgent(_ context.Context, agentID, skillID uuid.UUID, req domain.UpdateSkillRequest) (domain.Skill, error) {
	f.updated = append(f.updated, req.Name)
	return domain.Skill{ID: skillID, AgentID: agentID, Name: req.Name, Content: req.Content, Enabled: true}, nil
}

func (f *fakeManager) DeleteSkillForAgent(_ context.Context, _, skillID uuid.UUID) error {
	f.deleted = append(f.deleted, skillID.String())
	return nil
}

func (f *fakeManager) CreateRuleForAgent(_ context.Context, agentID uuid.UUID, req domain.CreateOrchestratorRuleRequest) (domain.OrchestratorRule, error) {
	f.rcreated = append(f.rcreated, req.Name)
	return domain.OrchestratorRule{ID: uuid.New(), AgentID: agentID, Name: req.Name, Content: req.Content}, nil
}

func (f *fakeManager) UpdateRuleForAgent(_ context.Context, agentID, ruleID uuid.UUID, req domain.UpdateOrchestratorRuleRequest) (domain.OrchestratorRule, error) {
	f.rupdated = append(f.rupdated, req.Name)
	return domain.OrchestratorRule{ID: ruleID, AgentID: agentID, Name: req.Name, Content: req.Content}, nil
}

func (f *fakeManager) DeleteRuleForAgent(_ context.Context, _, _ uuid.UUID) error { return nil }

// No-op stand-ins for applyOutput's real callbacks, in tests that exercise applySkillChanges/applyRuleChanges directly and only care about the manager calls.
func noopRecordEvent(domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error) {
	return domain.AgentEvolutionEvent{}, nil
}
func noopRecordOutcome(domain.ReflectionChangeOutcome) {}

func skillMap(names ...string) map[string]domain.Skill {
	out := map[string]domain.Skill{}
	for _, n := range names {
		id := uuid.New()
		out[id.String()] = domain.Skill{ID: id, Name: n, Enabled: true}
	}
	return out
}

func TestSkillBudgetRejectsCreateAtCap(t *testing.T) {
	mgr := &fakeManager{}
	s := &Service{manager: mgr, cfg: domain.EvolutionConfig{MaxSkillChanges: 3, MaxSkillsPerAgent: 2}}
	existing := skillMap("alpha", "beta")

	applied := s.applySkillChanges(context.Background(), domain.Agent{ID: uuid.New(), Name: "dev"},
		[]domain.ReflectionSkillChange{{Action: "create", Name: "gamma", Description: "d", Content: "c"}},
		existing, noopRecordEvent, noopRecordOutcome)

	if len(mgr.created) != 0 {
		t.Fatalf("create should be rejected at budget, got %v", mgr.created)
	}
	if len(applied) != 1 || !strings.Contains(applied[0], "rejected") {
		t.Fatalf("rejection not reported: %v", applied)
	}
}

func TestSkillBudgetFreesAfterDelete(t *testing.T) {
	mgr := &fakeManager{}
	s := &Service{manager: mgr, cfg: domain.EvolutionConfig{MaxSkillChanges: 5, MaxSkillsPerAgent: 2}}
	existing := skillMap("alpha", "beta")
	var doomed string
	for id, sk := range existing {
		if sk.Name == "beta" {
			doomed = id
		}
	}

	s.applySkillChanges(context.Background(), domain.Agent{ID: uuid.New()},
		[]domain.ReflectionSkillChange{
			{Action: "delete", SkillID: doomed},
			{Action: "create", Name: "gamma", Description: "d", Content: "c"},
		}, existing, noopRecordEvent, noopRecordOutcome)

	if len(mgr.created) != 1 {
		t.Fatalf("create after delete should fit the budget, got %v", mgr.created)
	}
}

func TestCreateOnExistingNameBecomesUpdate(t *testing.T) {
	mgr := &fakeManager{}
	s := &Service{manager: mgr, cfg: domain.EvolutionConfig{MaxSkillChanges: 3, MaxSkillsPerAgent: 10}}

	s.applySkillChanges(context.Background(), domain.Agent{ID: uuid.New()},
		[]domain.ReflectionSkillChange{{Action: "create", Name: "Alpha", Description: "d", Content: "new body"}},
		skillMap("alpha"), noopRecordEvent, noopRecordOutcome)

	if len(mgr.created) != 0 || len(mgr.updated) != 1 {
		t.Fatalf("same-name create should merge into an update: created=%v updated=%v", mgr.created, mgr.updated)
	}
}

func TestRuleBudgetRejectsCreateAtCap(t *testing.T) {
	mgr := &fakeManager{}
	s := &Service{manager: mgr, cfg: domain.EvolutionConfig{MaxRuleChanges: 3, MaxRulesPerAgent: 1}}
	id := uuid.New()
	existing := map[string]domain.OrchestratorRule{id.String(): {ID: id, Name: "only"}}

	applied := s.applyRuleChanges(context.Background(), domain.Agent{ID: uuid.New()},
		[]domain.ReflectionRuleChange{{Action: "create", Name: "second", Content: "c"}},
		existing, noopRecordEvent, noopRecordOutcome)

	if len(mgr.rcreated) != 0 || len(applied) != 1 || !strings.Contains(applied[0], "rejected") {
		t.Fatalf("rule create should be rejected at budget: created=%v applied=%v", mgr.rcreated, applied)
	}
}

func TestJudgeRoutingPrefersJudgeModel(t *testing.T) {
	s := &Service{cfg: domain.EvolutionConfig{Model: "cheap", JudgeModel: "strict", JudgeProviderType: "openai"}}
	model, provider := s.judgeRouting(domain.Agent{Model: "agent-model", ProviderType: "anthropic"})
	if model != "strict" || provider != domain.LLMProviderType("openai") {
		t.Fatalf("judge routing wrong: %s / %s", model, provider)
	}
}

func TestReflectionPromptCarriesBudgetAndGate(t *testing.T) {
	cfg := domain.EvolutionConfig{MaxSkillChanges: 3, MaxRuleChanges: 3, MaxMemoryChanges: 5,
		MaxSkillsPerAgent: 25, MaxRulesPerAgent: 15, GoldenGate: true}
	prompt := reflectionSystemPrompt(domain.Agent{Name: "dev", SelfEvolutionEnabled: true}, cfg)
	if !strings.Contains(prompt, "CONSOLIDATION FIRST") {
		t.Error("prompt missing consolidation instruction")
	}
	if !strings.Contains(prompt, "at most 25 skills") {
		t.Error("prompt missing standing budget")
	}
	if !strings.Contains(prompt, "Nothing you change is written directly") {
		t.Error("prompt missing the probe gate")
	}
	if !strings.Contains(prompt, `"dimension"`) {
		t.Error("prompt does not ask for a dimension on each change")
	}
}
