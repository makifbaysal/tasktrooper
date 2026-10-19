package evolution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestParseReflectionOutput(t *testing.T) {
	clean := `{"self_assessment":"ok","skills":[],"rules":[],"memories":[],"reverts":[]}`
	out, _, err := parseReflectionOutput(clean)
	if err != nil || out.SelfAssessment != "ok" {
		t.Fatalf("clean parse failed: %v", err)
	}

	fenced := "Here is my analysis:\n```json\n{\"self_assessment\":\"fenced\",\"skills\":[{\"action\":\"create\",\"name\":\"x\",\"description\":\"d\",\"category\":\"c\",\"content\":\"body\",\"reason\":\"r\"}]}\n```"
	out, analysis, err := parseReflectionOutput(fenced)
	if err != nil {
		t.Fatalf("fenced parse failed: %v", err)
	}
	if out.SelfAssessment != "fenced" || len(out.Skills) != 1 || out.Skills[0].Action != "create" {
		t.Fatalf("fenced content wrong: %+v", out)
	}
	if !strings.Contains(analysis, "Here is my analysis") {
		t.Errorf("analysis should keep the surrounding prose, got %q", analysis)
	}

	prose := "Sure! {\"self_assessment\":\"embedded\"} hope that helps"
	out, _, err = parseReflectionOutput(prose)
	if err != nil || out.SelfAssessment != "embedded" {
		t.Fatalf("embedded parse failed: %v", err)
	}

	if _, _, err = parseReflectionOutput("no json here at all"); err == nil {
		t.Fatal("expected error for garbage output")
	}
	if _, _, err = parseReflectionOutput("{broken json"); err == nil {
		t.Fatal("expected error for broken json")
	}
}

// Locks the fix for the real bug: the CLI path has no JSON schema at all (runLLM's AllowWebResearch branch), so the model invents its own key names. Both shapes are abridged from actual raw_output rows that completed with an empty summary and zero events before this fix; both must parse with reason preserved.
func TestParseReflectionOutput_CLIShapes(t *testing.T) {
	summaryShape := "## Analysis\n**Baseline vs. current:** Composite KPI rose from 64.28 → 72.2 across the window.\n\n" +
		"```json\n" +
		`{
  "summary": "Performance improved vs baseline: fewer revisions, composite KPI up 8 points.",
  "reverts": [],
  "skill_changes": [
    {"action": "delete", "id": "5ac2e677-1234-4a11-9a11-abc123456789", "name": "incremental-commits", "reason": "Redundant with the commit-discipline rule"}
  ],
  "rule_changes": [],
  "memories": []
}` + "\n```\n"
	out, analysis, err := parseReflectionOutput(summaryShape)
	if err != nil {
		t.Fatalf("summary/skill_changes shape: %v", err)
	}
	if out.SelfAssessment != "Performance improved vs baseline: fewer revisions, composite KPI up 8 points." {
		t.Errorf("summary was not aliased to self_assessment: %q", out.SelfAssessment)
	}
	if len(out.Skills) != 1 || out.Skills[0].Action != "delete" {
		t.Fatalf("skill_changes was not aliased to skills: %+v", out.Skills)
	}
	if out.Skills[0].SkillID != "5ac2e677-1234-4a11-9a11-abc123456789" {
		t.Errorf("id was not aliased to skill_id: %q", out.Skills[0].SkillID)
	}
	if out.Skills[0].Reason != "Redundant with the commit-discipline rule" {
		t.Errorf("reason lost: %q", out.Skills[0].Reason)
	}
	if !strings.Contains(analysis, "Baseline vs. current") {
		t.Errorf("analysis prose lost: %q", analysis)
	}

	reasoningShape := "Looked at the last week of runs.\n```json\n" +
		`{
  "reasoning": "One skill kept producing bad diffs; removing it.",
  "skill_changes": [
    {"action": "delete", "id": "9f1e2222-3333-4444-5555-666677778888", "name": "stale-helper", "justification": "Caused three revisions this window"}
  ]
}` + "\n```\nHope that helps! Let me know if you want more detail."
	out, _, err = parseReflectionOutput(reasoningShape)
	if err != nil {
		t.Fatalf("reasoning/justification shape (with trailing prose): %v", err)
	}
	if out.SelfAssessment != "One skill kept producing bad diffs; removing it." {
		t.Errorf("reasoning was not aliased to self_assessment: %q", out.SelfAssessment)
	}
	if len(out.Skills) != 1 || out.Skills[0].Reason != "Caused three revisions this window" {
		t.Fatalf("justification was not aliased to reason: %+v", out.Skills)
	}
}

// Guards the "last fenced block wins" rule: a model that shows an example JSON shape before its real answer must not have the example mistaken for the decision.
func TestParseReflectionOutput_PrefersLastFencedBlock(t *testing.T) {
	raw := "Example shape:\n```json\n{\"self_assessment\":\"example, ignore me\"}\n```\n\n" +
		"My actual answer:\n```json\n{\"self_assessment\":\"real answer\",\"skills\":[],\"rules\":[],\"memories\":[],\"reverts\":[]}\n```"
	out, _, err := parseReflectionOutput(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.SelfAssessment != "real answer" {
		t.Errorf("self_assessment = %q, want the LAST fenced block's value", out.SelfAssessment)
	}
}

func evts(deltas ...float64) []domain.AgentScoreEvent {
	out := make([]domain.AgentScoreEvent, len(deltas))
	for i, d := range deltas {
		out[i] = domain.AgentScoreEvent{Delta: d, CreatedAt: time.Now()}
	}
	return out
}

func TestClassifyImpact(t *testing.T) {
	cases := []struct {
		name   string
		before []domain.AgentScoreEvent
		after  []domain.AgentScoreEvent
		want   string
	}{
		{"insufficient after data", evts(5, 5, 5), evts(5), domain.EvolutionImpactInsufficientData},
		{"effective: revisions dropped", evts(-10, -10, 5), evts(5, 5, 5), domain.EvolutionImpactEffective},
		{"regressed: revisions rose", evts(5, 5, 5), evts(-10, -10, 5), domain.EvolutionImpactRegressed},
		{"neutral: same shape", evts(5, -10, 5), evts(5, -10, 5), domain.EvolutionImpactNeutral},
		{"no baseline, positive net", evts(), evts(5, 5, 5), domain.EvolutionImpactEffective},
		{"no baseline, negative net", evts(), evts(-10, -5, 5), domain.EvolutionImpactRegressed},
		{"no baseline, flat", evts(), evts(5, -5, -1), domain.EvolutionImpactNeutral},
	}
	for _, c := range cases {
		if got := ClassifyImpact(c.before, c.after, 3); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestReflectionSystemPromptGating(t *testing.T) {
	cfg := domain.EvolutionConfig{MaxSkillChanges: 3, MaxRuleChanges: 3, MaxMemoryChanges: 5, AllowWebResearch: true}
	enabled := reflectionSystemPrompt(domain.Agent{Name: "dev", SelfEvolutionEnabled: true}, cfg)
	if !contains(enabled, "You MAY change") || !contains(enabled, "web_search") {
		t.Error("enabled prompt missing skill-change or web-research permission")
	}
	disabled := reflectionSystemPrompt(domain.Agent{Name: "dev", SelfEvolutionEnabled: false}, cfg)
	if !contains(disabled, "Self-evolution is DISABLED") {
		t.Error("disabled prompt missing gating clause")
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

// Minimal in-memory store: enough of CreateEvent/GetEvent to exercise applyOutput's outcome bookkeeping, and enough of ListReflections to exercise legacy-decision derivation.
type fakeEvolutionStore struct {
	mu          sync.Mutex
	events      []domain.AgentEvolutionEvent
	reflections []domain.AgentReflection
	created     []domain.AgentReflection
	completed   []domain.AgentReflection
}

func (f *fakeEvolutionStore) CreateReflection(_ context.Context, r domain.AgentReflection) (domain.AgentReflection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ID = uuid.New()
	f.created = append(f.created, r)
	return r, nil
}
func (f *fakeEvolutionStore) CompleteReflection(_ context.Context, r domain.AgentReflection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = append(f.completed, r)
	return nil
}
func (f *fakeEvolutionStore) GetReflection(context.Context, uuid.UUID) (domain.AgentReflection, error) {
	return domain.AgentReflection{}, nil
}
func (f *fakeEvolutionStore) ListReflections(context.Context, uuid.UUID, int) ([]domain.AgentReflection, error) {
	// Copied, not aliased: a real store scans fresh rows on every call, so mutating the caller's result (deriveLegacyDecisions does) must never reach back into what this fake holds.
	out := make([]domain.AgentReflection, len(f.reflections))
	copy(out, f.reflections)
	return out, nil
}
func (f *fakeEvolutionStore) LatestCompletedReflection(context.Context, uuid.UUID) (*domain.AgentReflection, error) {
	return nil, nil
}
func (f *fakeEvolutionStore) LatestReflectionByTrigger(context.Context, uuid.UUID, string) (*domain.AgentReflection, error) {
	return nil, nil
}
func (f *fakeEvolutionStore) CreateEvent(_ context.Context, e domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error) {
	e.ID = uuid.New()
	f.events = append(f.events, e)
	return e, nil
}
func (f *fakeEvolutionStore) GetEvent(_ context.Context, id uuid.UUID) (domain.AgentEvolutionEvent, error) {
	for _, e := range f.events {
		if e.ID == id {
			return e, nil
		}
	}
	return domain.AgentEvolutionEvent{}, fmt.Errorf("event not found")
}
func (f *fakeEvolutionStore) ListEvents(context.Context, uuid.UUID, int) ([]domain.AgentEvolutionEvent, error) {
	return f.events, nil
}
func (f *fakeEvolutionStore) ListEventsByImpact(context.Context, string, time.Time, int) ([]domain.AgentEvolutionEvent, error) {
	return nil, nil
}
func (f *fakeEvolutionStore) UpdateEventImpact(context.Context, uuid.UUID, string) error { return nil }

type fakePerfStore struct{}

func (fakePerfStore) GetScore(context.Context, uuid.UUID) (domain.AgentPerformanceScore, error) {
	return domain.AgentPerformanceScore{Score: 80}, nil
}
func (fakePerfStore) ApplyDelta(context.Context, domain.ApplyScoreInput) (domain.AgentPerformanceScore, error) {
	return domain.AgentPerformanceScore{}, nil
}
func (fakePerfStore) RecentEvents(context.Context, uuid.UUID, int) ([]domain.AgentScoreEvent, error) {
	return nil, nil
}
func (fakePerfStore) EventsInWindow(context.Context, uuid.UUID, time.Time, time.Time) ([]domain.AgentScoreEvent, error) {
	return nil, nil
}
func (fakePerfStore) HasEventForTask(context.Context, uuid.UUID, string) (bool, error) {
	return false, nil
}

// The fix end-to-end: a normalized, schema-shaped output (what parseReflectionOutput now hands back for a CLI-path raw output) must reach the catalog manager, create an evolution event, and record an outcome that carries the reason and points at that event — the record the old code silently dropped.
func TestApplyOutput_CLIShapedOutputAppliesAndRecordsOutcome(t *testing.T) {
	mgr := &fakeManager{}
	store := &fakeEvolutionStore{}
	svc := &Service{
		manager: mgr, store: store, perf: fakePerfStore{},
		cfg: domain.EvolutionConfig{MaxSkillChanges: 3, MaxRuleChanges: 3, MaxMemoryChanges: 3, MaxSkillsPerAgent: 10, MaxRulesPerAgent: 10},
	}
	agentRec := domain.Agent{ID: uuid.New(), Name: "dev", SelfEvolutionEnabled: true}
	reflection := domain.AgentReflection{ID: uuid.New(), AgentID: agentRec.ID}
	output := domain.ReflectionOutput{
		SelfAssessment: "ok",
		Skills: []domain.ReflectionSkillChange{
			{Action: "create", Name: "new-skill", Description: "d", Category: "c", Content: "body", Reason: "evidence supports it"},
		},
	}

	applyLog, events, outcomes := svc.applyOutput(context.Background(), agentRec, reflection, output, nil, nil)

	if len(mgr.created) != 1 {
		t.Fatalf("skill was not created: %v", mgr.created)
	}
	if len(applyLog) != 1 {
		t.Fatalf("applyLog = %v, want 1 entry", applyLog)
	}
	if len(events) != 1 {
		t.Fatalf("events = %v, want 1", events)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %v, want 1", outcomes)
	}
	out := outcomes[0]
	if out.Kind != "skill" || out.Action != "create" || out.Outcome != domain.ReflectionOutcomeApplied {
		t.Fatalf("outcome wrong: %+v", out)
	}
	if out.Reason != "evidence supports it" {
		t.Errorf("reason not carried through to the outcome: %q", out.Reason)
	}
	if out.EventID == nil || *out.EventID != events[0].ID {
		t.Errorf("outcome.EventID does not match the created event: %+v vs %v", out.EventID, events[0].ID)
	}
}

// The whole-batch skip when SelfEvolutionEnabled=false must still produce a "skipped" outcome for every proposed skill/rule change, not silently drop them from the record.
func TestApplyOutput_SkippedWhenSelfEvolutionDisabled(t *testing.T) {
	mgr := &fakeManager{}
	store := &fakeEvolutionStore{}
	svc := &Service{manager: mgr, store: store, perf: fakePerfStore{}, cfg: domain.EvolutionConfig{MaxMemoryChanges: 3}}
	agentRec := domain.Agent{ID: uuid.New(), SelfEvolutionEnabled: false}
	reflection := domain.AgentReflection{ID: uuid.New(), AgentID: agentRec.ID}
	output := domain.ReflectionOutput{
		Skills: []domain.ReflectionSkillChange{{Action: "create", Name: "x", Content: "c", Reason: "r"}},
		Rules:  []domain.ReflectionRuleChange{{Action: "create", Name: "y", Content: "c", Reason: "r"}},
	}

	_, _, outcomes := svc.applyOutput(context.Background(), agentRec, reflection, output, nil, nil)

	if len(mgr.created) != 0 || len(mgr.rcreated) != 0 {
		t.Fatalf("nothing should be applied when self-evolution is disabled: skills=%v rules=%v", mgr.created, mgr.rcreated)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %+v, want 2 (one skill, one rule)", outcomes)
	}
	for _, o := range outcomes {
		if o.Outcome != domain.ReflectionOutcomeSkipped {
			t.Errorf("outcome = %+v, want skipped", o)
		}
	}
}

// A completed reflection stored before Decision existed (Decision == nil, RawOutput populated) must get one synthesized on read, tagged Legacy, with Summary backfilled and Baseline pulled from the next older completed reflection with a PerformanceSnapshot — all without writing anything back to the store.
func TestListReflections_DerivesLegacyDecision(t *testing.T) {
	agentID := uuid.New()
	rawOutput := `{"self_assessment":"legacy assessment","skills":[{"action":"delete","skill_id":"s1","name":"old","reason":"stale"}],"rules":[],"memories":[],"reverts":[]}`
	baselineTime := time.Now().Add(-48 * time.Hour)
	store := &fakeEvolutionStore{
		reflections: []domain.AgentReflection{
			{ID: uuid.New(), AgentID: agentID, Status: domain.ReflectionStatusCompleted, RawOutput: rawOutput, CreatedAt: time.Now()},
			{ID: uuid.New(), AgentID: agentID, Status: domain.ReflectionStatusCompleted, Summary: "older",
				PerformanceSnapshot: &domain.PerformanceSnapshot{Score: 50, CapturedAt: baselineTime}, CreatedAt: baselineTime},
		},
	}
	svc := &Service{store: store}

	reflections, err := svc.ListReflections(context.Background(), agentID, 10)
	if err != nil {
		t.Fatalf("ListReflections: %v", err)
	}
	if len(reflections) != 2 {
		t.Fatalf("expected 2 reflections, got %d", len(reflections))
	}
	newer := reflections[0]
	if newer.Decision == nil || !newer.Decision.Legacy {
		t.Fatalf("newer reflection should have a derived legacy decision: %+v", newer.Decision)
	}
	if newer.Decision.SelfAssessment != "legacy assessment" {
		t.Errorf("self_assessment = %q", newer.Decision.SelfAssessment)
	}
	if len(newer.Decision.Changes) != 1 || newer.Decision.Changes[0].Outcome != domain.ReflectionOutcomeNotApplied {
		t.Fatalf("changes = %+v, want one not_applied change", newer.Decision.Changes)
	}
	if newer.Summary != "legacy assessment" {
		t.Errorf("summary should be backfilled from the derived self_assessment, got %q", newer.Summary)
	}
	if newer.Decision.Baseline == nil || newer.Decision.Baseline.Score != 50 {
		t.Fatalf("baseline should be backfilled from the older completed reflection with a snapshot: %+v", newer.Decision.Baseline)
	}
	// Nothing is written back to the store — deriveLegacyDecisions only mutates the slice ListReflections is about to return.
	if store.reflections[0].Decision != nil {
		t.Error("the derived decision must not be persisted back to the store")
	}
}
