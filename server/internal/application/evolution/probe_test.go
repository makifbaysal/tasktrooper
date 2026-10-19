package evolution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeCatalogStore struct {
	port.CatalogStore
	agents []domain.Agent
	skills []domain.Skill
	rules  []domain.OrchestratorRule
}

func (f *fakeCatalogStore) GetAgent(_ context.Context, id uuid.UUID) (domain.Agent, error) {
	for _, a := range f.agents {
		if a.ID == id {
			return a, nil
		}
	}
	return domain.Agent{}, errors.New("agent not found")
}
func (f *fakeCatalogStore) ListAgents(context.Context) ([]domain.Agent, error) { return f.agents, nil }
func (f *fakeCatalogStore) ListSkillsByAgent(context.Context, uuid.UUID) ([]domain.Skill, error) {
	return f.skills, nil
}
func (f *fakeCatalogStore) ListRulesByAgent(context.Context, uuid.UUID) ([]domain.OrchestratorRule, error) {
	return f.rules, nil
}
func (f *fakeCatalogStore) ListTechStacksByAgent(context.Context, uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}

type fakeGoldenStore struct {
	port.GoldenTaskStore
	mu      sync.Mutex
	tasks   []domain.GoldenTask
	results []domain.GoldenResult
}

func (f *fakeGoldenStore) ListByAgent(context.Context, uuid.UUID) ([]domain.GoldenTask, error) {
	return f.tasks, nil
}
func (f *fakeGoldenStore) SaveResult(_ context.Context, r domain.GoldenResult) (domain.GoldenResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = append(f.results, r)
	return r, nil
}

type fakeRunStore struct {
	port.TaskAgentRunStore
	runs []domain.TaskAgentRun
}

func (f *fakeRunStore) ListRecent(context.Context, int) ([]domain.TaskAgentRun, error) {
	return f.runs, nil
}

type fakeTaskStore struct {
	port.BoardTaskStore
	tasks map[uuid.UUID]domain.BoardTask
}

func (f *fakeTaskStore) GetByID(_ context.Context, id uuid.UUID) (domain.BoardTask, error) {
	if t, ok := f.tasks[id]; ok {
		return t, nil
	}
	return domain.BoardTask{}, errors.New("task not found")
}

type fakeSessionStore struct{ port.SessionStore }

func (fakeSessionStore) ListByAgent(context.Context, uuid.UUID, int, int) ([]domain.Session, error) {
	return nil, nil
}

type fakeBoardStore struct {
	port.BoardConfigStore
	members []domain.BoardMember
}

func (f *fakeBoardStore) ListMembers(context.Context) ([]domain.BoardMember, error) {
	return f.members, nil
}

// Answers by what a request is for: the reflection, a rehearsal, the probe judge or the lesson.
type scriptedLLM struct {
	mu       sync.Mutex
	requests []domain.AgentRequest
	reflect  []string
	rehearse func(system, user string) string
	score    func(answer string) int
	lesson   string
}

func (l *scriptedLLM) answer(msgs []domain.Message) string {
	system, user := msgs[0].Content, msgs[len(msgs)-1].Content
	switch {
	case strings.Contains(system, "self-improvement process"):
		l.mu.Lock()
		defer l.mu.Unlock()
		if len(l.reflect) == 0 {
			return `{"self_assessment":"nothing more","skills":[],"rules":[],"memories":[],"reverts":[]}`
		}
		next := l.reflect[0]
		l.reflect = l.reflect[1:]
		return next
	case strings.Contains(system, "strict, independent evaluator"):
		var parts []string
		for _, chunk := range strings.Split(user, "### Answer ")[1:] {
			label, text := chunk[:1], chunk[1:]
			parts = append(parts, `{"label":"`+label+`","score":`+strconv.Itoa(l.score(text))+`,"reason":"r"}`)
		}
		return `{"scores":[` + strings.Join(parts, ",") + `]}`
	case strings.Contains(system, "record of one tested change set"):
		return l.lesson
	default:
		return l.rehearse(system, user)
	}
}

func (l *scriptedLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	l.mu.Lock()
	l.requests = append(l.requests, req)
	l.mu.Unlock()
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: l.answer(req.Messages)}}, nil
}
func (l *scriptedLLM) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return l.Chat(ctx, req)
}
func (l *scriptedLLM) Models(context.Context) ([]string, error)                 { return nil, nil }
func (l *scriptedLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

func (l *scriptedLLM) count(pred func(domain.AgentRequest) bool) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, r := range l.requests {
		if pred(r) {
			n++
		}
	}
	return n
}

func isReflection(r domain.AgentRequest) bool {
	return strings.Contains(r.Messages[0].Content, "self-improvement process")
}

type probeFixture struct {
	svc     *Service
	agent   domain.Agent
	mgr     *fakeManager
	store   *fakeEvolutionStore
	golden  *fakeGoldenStore
	llm     *scriptedLLM
	catalog *fakeCatalogStore
}

func newProbeFixture(t *testing.T) *probeFixture {
	t.Helper()
	agentRec := domain.Agent{
		ID: uuid.New(), Name: "backend-developer", Enabled: true, SelfEvolutionEnabled: true,
		SystemPrompt: "You are a backend developer.", Model: "m", ProviderType: "openai",
	}
	f := &probeFixture{
		agent: agentRec, mgr: &fakeManager{}, store: &fakeEvolutionStore{}, golden: &fakeGoldenStore{},
		llm:     &scriptedLLM{lesson: `{"entry":false,"cue":"","lesson":"","evidence":"","confidence":"low"}`},
		catalog: &fakeCatalogStore{agents: []domain.Agent{agentRec}},
	}
	f.svc = &Service{
		store: f.store, catalog: f.catalog, manager: f.mgr, perf: fakePerfStore{},
		sessions: fakeSessionStore{}, runs: &fakeRunStore{}, golden: f.golden, llm: f.llm,
		tasks: &fakeTaskStore{tasks: map[uuid.UUID]domain.BoardTask{}},
		cfg: domain.EvolutionConfig{
			Enabled: true, GoldenGate: true, ProbeCases: 2, ProbeMaxTurns: 4,
			MaxSkillChanges: 3, MaxRuleChanges: 3, MaxSkillsPerAgent: 10, MaxRulesPerAgent: 10,
			MaxMemoryChanges: 3, MaxAlwaysOnChars: 8000, EvidenceMaxChars: 24000,
		},
	}
	return f
}

func (f *probeFixture) reflection() domain.AgentReflection {
	now := time.Now()
	return domain.AgentReflection{
		ID: uuid.New(), AgentID: f.agent.ID, Trigger: domain.ReflectionTriggerManual,
		WindowStart: now.Add(-24 * time.Hour), WindowEnd: now,
	}
}

func (f *probeFixture) run(t *testing.T) domain.AgentReflection {
	t.Helper()
	f.svc.processReflection(context.Background(), f.reflection())
	if len(f.store.completed) != 1 {
		t.Fatalf("completed reflections = %d, want 1", len(f.store.completed))
	}
	got := f.store.completed[0]
	if got.Status != domain.ReflectionStatusCompleted {
		t.Fatalf("reflection status = %s (%s)", got.Status, got.Error)
	}
	return got
}

func (f *probeFixture) addReplayTask(title string) {
	taskID := uuid.New()
	f.svc.tasks.(*fakeTaskStore).tasks[taskID] = domain.BoardTask{ID: taskID, Key: "ORD-1", Title: title, Description: "Do the work."}
	runs := f.svc.runs.(*fakeRunStore)
	runs.runs = append(runs.runs, domain.TaskAgentRun{
		ID: uuid.New(), TaskID: taskID, AgentID: f.agent.ID, Status: "completed", CreatedAt: time.Now().Add(-time.Hour),
	})
}

func outcomeFor(t *testing.T, d *domain.ReflectionDecision, name string) domain.ReflectionChangeOutcome {
	t.Helper()
	for _, c := range d.Changes {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no outcome for %q in %+v", name, d.Changes)
	return domain.ReflectionChangeOutcome{}
}

const schemaSkillOutput = "Looked at the revisions.\n```json\n" +
	`{"self_assessment":"rollbacks keep being missed","skills":[{"action":"create","dimension":"procedure","skill_id":"","name":"schema-changes","description":"When a change touches the database schema, load this first","category":"db","content":"Always plan the ROLLBACK before writing the migration.","source_urls":[],"reason":"two revisions asked for a rollback"}],"rules":[],"memories":[],"reverts":[]}` +
	"\n```"

// The core of the change: a change set is rehearsed before it exists, and only what did better is written — here the golden task fails on the current setup and passes with the new skill.
func TestProbeAdoptsAChangeThatDoesBetterAndWritesOnlyThen(t *testing.T) {
	f := newProbeFixture(t)
	f.golden.tasks = []domain.GoldenTask{{
		ID: uuid.New(), AgentID: f.agent.ID, Name: "ship-schema", Prompt: "How do you ship a schema change?",
		Expected: []string{"rollback"}, Enabled: true,
	}}
	f.llm.reflect = []string{schemaSkillOutput}
	f.llm.rehearse = func(system, _ string) string {
		if strings.Contains(system, "ROLLBACK") {
			return "Plan the rollback, then migrate."
		}
		return "Write the migration and ship it."
	}
	f.llm.lesson = `{"entry":true,"cue":"reviewers keep asking for the same missing step","lesson":"put the step in a skill the run loads for that work","evidence":"golden task passed only with the skill","confidence":"medium"}`

	got := f.run(t)

	if len(f.mgr.created) != 1 || f.mgr.created[0] != "schema-changes" {
		t.Fatalf("the probed-and-better skill was not written: %v", f.mgr.created)
	}
	gate := got.Decision.Gate
	if gate == nil || !gate.Probed || gate.Adopted != domain.ProbeVariantAll || !gate.Keep {
		t.Fatalf("gate = %+v, want the whole set adopted", gate)
	}
	if gate.BeforeRate == nil || *gate.BeforeRate != 0 || gate.AfterRate == nil || *gate.AfterRate != 1 {
		t.Fatalf("golden rates = %v → %v, want 0 → 1", gate.BeforeRate, gate.AfterRate)
	}
	out := outcomeFor(t, got.Decision, "schema-changes")
	if out.Outcome != domain.ReflectionOutcomeApplied || out.Verdict != domain.ProbeVerdictHelped || out.Dimension != domain.ReflectionDimensionProcedure {
		t.Fatalf("outcome = %+v, want applied/helped/procedure", out)
	}
	if got.Decision.Lesson == nil || got.Decision.Lesson.Confidence != "medium" {
		t.Fatalf("lesson = %+v, want the distilled lesson recorded", got.Decision.Lesson)
	}
	if len(f.golden.results) != 2 || f.golden.results[0].Passed || !f.golden.results[1].Passed {
		t.Fatalf("golden history = %+v, want a failing before and a passing after", f.golden.results)
	}
	if got.PerformanceSnapshot.GoldenPassRate == nil || *got.PerformanceSnapshot.GoldenPassRate != 1 {
		t.Fatalf("snapshot golden rate = %v, want 1", got.PerformanceSnapshot.GoldenPassRate)
	}
}

// An agent that rewrote its own instructions with nothing to test them on keeps its old ones: the case the old gate let through as "no suite".
func TestProbeWritesNothingWhenThereIsNothingToMeasure(t *testing.T) {
	f := newProbeFixture(t)
	f.llm.reflect = []string{schemaSkillOutput}
	f.llm.rehearse = func(string, string) string { t.Fatal("no case, so nothing should be rehearsed"); return "" }

	got := f.run(t)

	if len(f.mgr.created) != 0 {
		t.Fatalf("an unmeasured change was written: %v", f.mgr.created)
	}
	if g := got.Decision.Gate; g == nil || !g.Unmeasured || g.Keep {
		t.Fatalf("gate = %+v, want unmeasured and nothing kept", g)
	}
	if out := outcomeFor(t, got.Decision, "schema-changes"); out.Outcome != domain.ReflectionOutcomeNotAdopted {
		t.Fatalf("outcome = %+v, want not_adopted", out)
	}
}

// The floor the old gate had: a golden task that passed before and fails after vetoes the variant, whatever the rehearsals say.
func TestProbeGoldenRegressionVetoesARehearsalWin(t *testing.T) {
	f := newProbeFixture(t)
	f.golden.tasks = []domain.GoldenTask{{
		ID: uuid.New(), Name: "ship", Prompt: "How do you ship?", Expected: []string{"ship it"}, Enabled: true,
	}}
	f.addReplayTask("Paginate orders")
	f.llm.reflect = []string{schemaSkillOutput}
	f.llm.rehearse = func(system, _ string) string {
		if strings.Contains(system, "ROLLBACK") {
			return "Plan the rollback first."
		}
		return "Write it and ship it."
	}
	f.llm.score = func(answer string) int {
		if strings.Contains(answer, "rollback") {
			return 9
		}
		return 3
	}

	got := f.run(t)

	if len(f.mgr.created) != 0 {
		t.Fatalf("a variant that broke a golden task was written: %v", f.mgr.created)
	}
	gate := got.Decision.Gate
	if gate.Adopted != "" || len(gate.Variants) != 1 || gate.Variants[0].GoldenLosses != 1 || gate.Variants[0].Wins != 1 {
		t.Fatalf("gate = %+v, want one rehearsal win, one golden loss, nothing adopted", gate)
	}
	if out := outcomeFor(t, got.Decision, "schema-changes"); out.Verdict != domain.ProbeVerdictHurt {
		t.Fatalf("verdict = %q, want hurt", out.Verdict)
	}
}

// Propose–probe–compose: the two halves are tried apart, the half that helped is written and the half that hurt is not, and each change carries its own verdict.
func TestProbeComposesTheHalfThatHelped(t *testing.T) {
	f := newProbeFixture(t)
	f.addReplayTask("Paginate orders")
	f.addReplayTask("Add an index")
	f.llm.reflect = []string{"```json\n" + `{"self_assessment":"s","skills":[{"action":"create","dimension":"procedure","skill_id":"","name":"verify-first","description":"When you are about to report a task done, load this","category":"qa","content":"PROC-MARK run the tests before reporting","source_urls":[],"reason":"r"}],"rules":[{"action":"create","dimension":"domain","rule_id":"","name":"always-graphql","content":"DOM-MARK every endpoint is GraphQL","priority":1,"reason":"r"}],"memories":[],"reverts":[]}` + "\n```"}
	f.llm.rehearse = func(system, _ string) string {
		var parts []string
		if strings.Contains(system, "PROC-MARK") {
			parts = append(parts, "P")
		}
		if strings.Contains(system, "DOM-MARK") {
			parts = append(parts, "D")
		}
		return "answer " + strings.Join(parts, "")
	}
	f.llm.score = func(answer string) int {
		score := 5
		if strings.Contains(answer, "P") {
			score += 3
		}
		if strings.Contains(answer, "D") {
			score -= 3
		}
		return score
	}

	got := f.run(t)

	if got.Decision.Gate.Adopted != domain.ReflectionDimensionProcedure {
		t.Fatalf("adopted = %q, want procedure; variants %+v", got.Decision.Gate.Adopted, got.Decision.Gate.Variants)
	}
	if len(f.mgr.created) != 1 || len(f.mgr.rcreated) != 0 {
		t.Fatalf("written skills=%v rules=%v, want only the procedure skill", f.mgr.created, f.mgr.rcreated)
	}
	if out := outcomeFor(t, got.Decision, "verify-first"); out.Verdict != domain.ProbeVerdictHelped || out.Outcome != domain.ReflectionOutcomeApplied {
		t.Fatalf("skill outcome = %+v", out)
	}
	if out := outcomeFor(t, got.Decision, "always-graphql"); out.Verdict != domain.ProbeVerdictHurt || out.Outcome != domain.ReflectionOutcomeNotAdopted {
		t.Fatalf("rule outcome = %+v", out)
	}
}

type scriptedExecutor struct {
	llm           *scriptedLLM
	mu            sync.Mutex
	calls         int
	reflectionSaw bool
	othersSaw     int
}

func (e *scriptedExecutor) Supports(p domain.LLMProviderType) bool {
	return p == domain.LLMProviderClaudeCode
}

func (e *scriptedExecutor) Execute(_ context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	e.mu.Lock()
	e.calls++
	if _, err := os.Stat(filepath.Join(req.WorkDir, "EVIDENCE.md")); err == nil {
		if strings.Contains(req.History[0].Content, "self-improvement process") {
			e.reflectionSaw = true
		} else {
			e.othersSaw++
		}
	}
	e.mu.Unlock()
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: e.llm.answer(req.History)}}, nil
}

// The root cause of the gate never running for Claude Code / OpenCode agents: the suite, the rehearsals, the judge and the lesson were HTTP-only calls their engine refused. They now run as short sessions on the agent's own CLI, and the reflection gets its evidence as files.
func TestProbeRunsOnTheAgentsOwnCLI(t *testing.T) {
	f := newProbeFixture(t)
	f.agent.ProviderType = domain.LLMProviderClaudeCode
	f.catalog.agents = []domain.Agent{f.agent}
	f.svc.cfg.AllowWebResearch = true
	f.golden.tasks = []domain.GoldenTask{{
		ID: uuid.New(), Name: "ship-schema", Prompt: "How do you ship a schema change?", Expected: []string{"rollback"}, Enabled: true,
	}}
	f.llm.reflect = []string{schemaSkillOutput}
	f.llm.rehearse = func(system, _ string) string {
		if strings.Contains(system, "ROLLBACK") {
			return "Plan the rollback."
		}
		return "Ship it."
	}
	cli := &scriptedExecutor{llm: f.llm}
	router := agent.NewRouter(refusingLoop{t: t})
	router.SetTaskExecutor(cli)
	f.svc.agentLoop = router
	f.svc.llm = &failingLLM{t: t}

	got := f.run(t)

	if len(f.mgr.created) != 1 {
		t.Fatalf("the CLI agent's probed change was not adopted: %+v", got.Decision.Gate)
	}
	// reflection + base and variant rehearsals + lesson; no judge (golden only)
	if cli.calls < 4 {
		t.Fatalf("executor calls = %d, want the reflection, both rehearsals and the lesson on the CLI", cli.calls)
	}
	if !cli.reflectionSaw {
		t.Fatal("the reflection session's working directory had no EVIDENCE.md")
	}
	if cli.othersSaw > 0 {
		t.Fatalf("%d rehearsal/judge/lesson sessions ran inside the evidence directory; they must not see what the judge grades against", cli.othersSaw)
	}
}

type failingLLM struct{ t *testing.T }

func (l *failingLLM) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	l.t.Fatal("a claude_code agent's evolution must not make an HTTP utility call")
	return domain.AgentResponse{}, nil
}
func (l *failingLLM) ChatStream(context.Context, domain.AgentRequest, func(string)) (domain.AgentResponse, error) {
	l.t.Fatal("a claude_code agent's evolution must not make an HTTP utility call")
	return domain.AgentResponse{}, nil
}
func (l *failingLLM) Models(context.Context) ([]string, error)                 { return nil, nil }
func (l *failingLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

// A rehearsal the engine refuses means nothing was measured, and an unmeasured change set is not written — what server/.ai/architecture.md always said the gate did.
func TestProbeUnservableEngineWritesNothing(t *testing.T) {
	f := newProbeFixture(t)
	f.agent.ProviderType = domain.LLMProviderClaudeCode
	f.catalog.agents = []domain.Agent{f.agent}
	f.golden.tasks = []domain.GoldenTask{{ID: uuid.New(), Name: "g", Prompt: "p", Expected: []string{"x"}, Enabled: true}}
	f.llm.reflect = []string{schemaSkillOutput}
	f.svc.agentLoop = nil

	got := f.run(t)

	if len(f.mgr.created) != 0 {
		t.Fatalf("a change set nothing could grade was written: %v", f.mgr.created)
	}
	g := got.Decision.Gate
	if g == nil || !g.Unmeasured || !strings.Contains(g.Reason, "could not run") {
		t.Fatalf("gate = %+v, want unmeasured with the engine named as the reason", g)
	}
}

// Harness² discards a proposal that fails validation after one retry with the error fed back; here the retry fixes it and the corrected set is what gets written.
func TestValidationProblemsGoBackToTheReflectionOnce(t *testing.T) {
	f := newProbeFixture(t)
	f.svc.cfg.GoldenGate = false
	f.llm.reflect = []string{
		"```json\n" + `{"self_assessment":"s","skills":[{"action":"create","dimension":"procedure","skill_id":"","name":"db-migrations","description":"x","category":"db","content":"Write a down migration.","source_urls":[],"reason":"r"}],"rules":[{"action":"create","dimension":"procedure","rule_id":"","name":"migrations-first","content":"Load the ` + "`schema-rollbacks`" + ` skill before migrating.","priority":1,"reason":"r"}],"memories":[],"reverts":[]}` + "\n```",
		"```json\n" + `{"self_assessment":"s","skills":[{"action":"create","dimension":"procedure","skill_id":"","name":"db-migrations","description":"When a change adds or alters a migration, load this","category":"db","content":"Write a down migration.","source_urls":[],"reason":"r"}],"rules":[{"action":"create","dimension":"procedure","rule_id":"","name":"migrations-first","content":"Load the ` + "`db-migrations`" + ` skill before migrating.","priority":1,"reason":"r"}],"memories":[],"reverts":[]}` + "\n```",
	}

	got := f.run(t)

	if n := f.llm.count(isReflection); n != 2 {
		t.Fatalf("reflection calls = %d, want the first answer and one revision", n)
	}
	var feedback string
	for _, r := range f.llm.requests {
		if isReflection(r) && len(r.Messages) > 2 {
			feedback = r.Messages[len(r.Messages)-1].Content
		}
	}
	if !strings.Contains(feedback, "cannot be used as written") || !strings.Contains(feedback, "schema-rollbacks") {
		t.Fatalf("the revision request did not carry the problems:\n%s", feedback)
	}
	if len(f.mgr.created) != 1 || len(f.mgr.rcreated) != 1 {
		t.Fatalf("written skills=%v rules=%v, want the corrected pair", f.mgr.created, f.mgr.rcreated)
	}
	for _, c := range got.Decision.Changes {
		if c.Outcome != domain.ReflectionOutcomeApplied {
			t.Fatalf("outcome %+v, want everything applied after the fix", c)
		}
	}
}

// What is still wrong after the one revision is dropped and recorded, never written.
func TestValidationDropsWhatTheRevisionStillGetsWrong(t *testing.T) {
	f := newProbeFixture(t)
	f.svc.cfg.GoldenGate = false
	bad := "```json\n" + `{"self_assessment":"s","skills":[{"action":"create","dimension":"domain","skill_id":"","name":"vague","description":"misc","category":"c","content":"body","source_urls":[],"reason":"r"}],"rules":[],"memories":[],"reverts":[]}` + "\n```"
	f.llm.reflect = []string{bad, bad}

	got := f.run(t)

	if len(f.mgr.created) != 0 {
		t.Fatalf("an invalid skill was written: %v", f.mgr.created)
	}
	out := outcomeFor(t, got.Decision, "vague")
	if out.Outcome != domain.ReflectionOutcomeRejected || !strings.Contains(out.Detail, "when to load it") {
		t.Fatalf("outcome = %+v, want rejected with the description problem", out)
	}
}

func TestValidatePlan(t *testing.T) {
	keep := domain.Skill{ID: uuid.New(), Name: "db-migrations", Description: "When a change alters the schema, load this", Enabled: true}
	ruleID := uuid.New()
	rules := []domain.OrchestratorRule{{ID: ruleID, Name: "migrations-first", Content: "Load the `db-migrations` skill first.", Enabled: true}}
	s := &Service{cfg: domain.EvolutionConfig{MaxSkillChanges: 5, MaxRuleChanges: 5, MaxSkillsPerAgent: 10, MaxRulesPerAgent: 10, MaxAlwaysOnChars: 60}}

	plan, _ := s.planCatalogChanges(domain.ReflectionOutput{
		Skills: []domain.ReflectionSkillChange{{Action: "delete", SkillID: keep.ID.String()}},
		Rules:  []domain.ReflectionRuleChange{{Action: "create", Name: "long", Content: strings.Repeat("x", 80)}},
	}, []domain.Skill{keep}, rules)
	problems := s.validatePlan([]domain.Skill{keep}, rules, plan)

	if len(problems) != 2 {
		t.Fatalf("problems = %+v, want the delete that strands a rule and the rule that blows the always-on cap", problems)
	}
	codes := map[string]bool{}
	for _, p := range problems {
		codes[p.Code] = true
	}
	if !codes[problemDeleteBreaksRef] || !codes[problemAlwaysOnOverLimit] {
		t.Fatalf("codes = %v", codes)
	}
	for _, p := range problems {
		if text := renderProblem(p); !strings.Contains(text, p.Name) {
			t.Fatalf("rendered problem does not name the change: %q", text)
		}
	}
}

// The plan must decide exactly as the apply path would, or the probe rehearses something other than what gets written.
func TestPlanCatalogChangesMirrorsApply(t *testing.T) {
	s := &Service{cfg: domain.EvolutionConfig{MaxSkillChanges: 2, MaxSkillsPerAgent: 2, MaxRuleChanges: 2, MaxRulesPerAgent: 10}}
	existing := []domain.Skill{{ID: uuid.New(), Name: "Alpha"}, {ID: uuid.New(), Name: "beta"}}

	plan, outcomes := s.planCatalogChanges(domain.ReflectionOutput{Skills: []domain.ReflectionSkillChange{
		{Action: "create", Name: "alpha", Description: "d", Content: "c"},
		{Action: "create", Name: "gamma", Description: "d", Content: "c"},
		{Action: "update", SkillID: uuid.NewString(), Name: "ghost"},
		{Action: "delete", SkillID: existing[1].ID.String()},
	}}, existing, nil)

	if len(plan) != 2 || plan[0].Action != "update" || plan[0].TargetID != existing[0].ID.String() || plan[1].Action != "delete" {
		t.Fatalf("plan = %+v, want the same-name create as an update, then the delete", plan)
	}
	want := map[string]string{"gamma": "skill budget full", "ghost": "unknown skill id"}
	for _, o := range outcomes {
		if want[o.Name] != o.Detail {
			t.Errorf("outcome %+v, want %q", o, want[o.Name])
		}
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %+v", outcomes)
	}
}

func TestChooseVariant(t *testing.T) {
	v := func(label string, changes, wins, losses int, accepted bool) domain.ReflectionProbeVariant {
		return domain.ReflectionProbeVariant{Label: label, Changes: changes, Wins: wins, Losses: losses, Accepted: accepted}
	}
	cases := []struct {
		name string
		in   []domain.ReflectionProbeVariant
		want string
	}{
		{"nothing better", []domain.ReflectionProbeVariant{v("all", 2, 0, 0, false)}, ""},
		{"single dimension", []domain.ReflectionProbeVariant{v("all", 1, 2, 0, true)}, "all"},
		{"one half wins, all ties it", []domain.ReflectionProbeVariant{v("procedure", 1, 2, 0, true), v("domain", 1, 0, 1, false), v("all", 2, 2, 0, true)}, "procedure"},
		{"both halves hold, all ties the best", []domain.ReflectionProbeVariant{v("procedure", 1, 1, 0, true), v("domain", 1, 1, 0, true), v("all", 2, 1, 0, true)}, "all"},
		{"synergy only together", []domain.ReflectionProbeVariant{v("procedure", 1, 0, 0, false), v("domain", 1, 0, 0, false), v("all", 2, 1, 0, true)}, "all"},
	}
	for _, tc := range cases {
		if got := chooseVariant(tc.in); got != tc.want {
			t.Errorf("%s: chooseVariant = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestChangeVerdictsMarkUnisolatedChangesConfounded(t *testing.T) {
	plan := []plannedChange{
		{Index: 0, Kind: "skill", Dimension: domain.ReflectionDimensionProcedure},
		{Index: 1, Kind: "rule", Dimension: domain.ReflectionDimensionProcedure},
	}
	variants := probeVariants(plan)
	verdicts := changeVerdicts(plan, variants, []domain.ReflectionProbeVariant{{Label: "all", Changes: 2, Wins: 2, Accepted: true}})
	if verdicts[0] != domain.ProbeVerdictConfounded || verdicts[1] != domain.ProbeVerdictConfounded {
		t.Fatalf("verdicts = %v, want both confounded: they were only ever tried together", verdicts)
	}
}

func TestRenderStepsKeepsErrorsAndTheLastWord(t *testing.T) {
	steps := []domain.SessionStep{
		{StepType: "tool_call_start", Payload: []byte(`{"tool":"run_terminal","arguments":"{\"cmd\":\"go test\"}"}`)},
		{StepType: "tool_call_result", Payload: []byte(`{"tool":"run_terminal","content":"FAIL orders","is_error":true}`)},
		{StepType: "iteration_start", Payload: []byte(`{"iteration":2}`)},
		{StepType: "assistant_message", Payload: []byte(`{"content":"Fixed and opened the PR."}`)},
	}
	out, errs, final := renderSteps(steps)
	if len(out) != 3 || out[0].Tag != "ACTION" || out[1].Tag != "ERROR" || out[2].Tag != "ASSISTANT" {
		t.Fatalf("steps = %+v", out)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "FAIL orders") || final != "Fixed and opened the PR." {
		t.Fatalf("errors = %v, final = %q", errs, final)
	}
}

// board_members is filled by nothing a user can reach, so the periodic sweep read an empty roster and never reflected on anyone.
func TestTickReflectsOnEveryEnabledAgentWhenNoRosterIsSet(t *testing.T) {
	agentRec := domain.Agent{ID: uuid.New(), Name: "dev", Enabled: true}
	store := &fakeEvolutionStore{}
	svc := &Service{
		store: store, board: &fakeBoardStore{}, catalog: &fakeCatalogStore{agents: []domain.Agent{agentRec, {ID: uuid.New(), Name: "off"}}},
		perf: fakePerfStore{}, sessions: fakeSessionStore{},
		runs:     &fakeRunStore{runs: []domain.TaskAgentRun{{AgentID: agentRec.ID, CreatedAt: time.Now()}}},
		llm:      &erroringLLM{err: errors.New("stop here")},
		cfg:      domain.EvolutionConfig{Enabled: true, ReflectInterval: time.Hour, ReflectionTimeout: time.Minute},
		inflight: map[string]bool{},
	}

	svc.tick(context.Background())
	svc.wg.Wait()

	if len(store.created) != 1 || store.created[0].AgentID != agentRec.ID || store.created[0].Trigger != domain.ReflectionTriggerPeriodic {
		t.Fatalf("created = %+v, want one periodic reflection for the enabled agent", store.created)
	}
}

func TestStartReflectionRefusedWhenEvolutionIsOff(t *testing.T) {
	store := &fakeEvolutionStore{}
	svc := &Service{store: store, cfg: domain.EvolutionConfig{Enabled: false}, inflight: map[string]bool{}}
	if _, err := svc.ReflectNow(context.Background(), uuid.New()); !errors.Is(err, ErrEvolutionDisabled) {
		t.Fatalf("err = %v, want ErrEvolutionDisabled", err)
	}
	if len(store.created) != 0 {
		t.Fatal("a reflection was created with evolution turned off")
	}
}

type erroringLLM struct{ err error }

func (e *erroringLLM) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, e.err
}
func (e *erroringLLM) ChatStream(context.Context, domain.AgentRequest, func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, e.err
}
func (e *erroringLLM) Models(context.Context) ([]string, error)                 { return nil, nil }
func (e *erroringLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

// Harnessing-level recursion: what an earlier probed change set taught reaches the next reflection, next to each skill's actual text rather than only its name.
func TestEvidenceCarriesEarlierLessonsAndSkillText(t *testing.T) {
	f := newProbeFixture(t)
	f.catalog.skills = []domain.Skill{{ID: uuid.New(), Name: "db-migrations", Description: "When the schema changes", Content: "Write the down migration first.", Enabled: true}}
	f.store.reflections = []domain.AgentReflection{{
		ID: uuid.New(), AgentID: f.agent.ID, Status: domain.ReflectionStatusCompleted, CreatedAt: time.Now().Add(-48 * time.Hour),
		Decision: &domain.ReflectionDecision{Lesson: &domain.ReflectionLesson{
			Cue: "a reviewer keeps asking for the same missing step", Lesson: "put that step in the skill for the work",
			Evidence: "won 2 of 2", Confidence: "medium",
		}},
	}}

	bundle, err := f.svc.gatherEvidence(context.Background(), f.agent, f.reflection(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Lessons from earlier tested changes", "a reviewer keeps asking for the same missing step", "Write the down migration first."} {
		if !strings.Contains(bundle.Report, want) {
			t.Errorf("evidence is missing %q:\n%s", want, bundle.Report)
		}
	}
}

func TestEvidenceFilesLayout(t *testing.T) {
	skills := []domain.Skill{{ID: uuid.New(), Name: "DB Migrations!", Description: "When the schema changes", Content: "Write the down migration.", Enabled: true}}
	rules := []domain.OrchestratorRule{{ID: uuid.New(), Name: "tests-first", Content: "Run the tests.", Priority: 2, Enabled: true}}
	runs := []runEvidence{{Title: "ORD-1 Paginate", Status: "completed", Request: "Task: Paginate", Steps: []trajectoryStep{{Tag: "ACTION", Text: "read_file x"}}}}

	files := evidenceFiles("# report", skills, rules, runs)
	for rel, want := range map[string]string{
		"EVIDENCE.md":                 "# report",
		"skills/001-db-migrations.md": "Write the down migration.",
		"rules/001-tests-first.md":    "priority: 2",
		"runs/01-ord-1-paginate.md":   "[ACTION] read_file x",
	} {
		if !strings.Contains(files[rel], want) {
			t.Errorf("%s does not contain %q:\n%s", rel, want, files[rel])
		}
	}
}

// The in-process loop reads the evidence with read_file, so it gets a directory on this disk — on its own run context only, removed when the reflection ends.
type capturingLoop struct {
	mu       sync.Mutex
	dirs     []string
	sawFile  bool
	tools    []string
	response string
}

func (l *capturingLoop) Run(ctx context.Context, _ []domain.Message, _ string, _ domain.LLMProviderType, policy domain.ToolPolicy, _ ...agent.RunOption) (domain.AgentResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	dir := registry.WorkspaceDirFromContext(ctx)
	l.dirs = append(l.dirs, dir)
	if _, err := os.Stat(filepath.Join(dir, "EVIDENCE.md")); dir != "" && err == nil {
		l.sawFile = true
	}
	l.tools = policy.AllowTools
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: l.response}}, nil
}
func (l *capturingLoop) RunTask(ctx context.Context, m []domain.Message, model string, p domain.LLMProviderType, policy domain.ToolPolicy, opts ...agent.RunOption) (domain.AgentResponse, error) {
	return l.Run(ctx, m, model, p, policy, opts...)
}
func (l *capturingLoop) RunStream(ctx context.Context, m []domain.Message, model string, p domain.LLMProviderType, policy domain.ToolPolicy, _ func(string), opts ...agent.RunOption) (domain.AgentResponse, error) {
	return l.Run(ctx, m, model, p, policy, opts...)
}

func TestReflectionOnTheLoopReadsEvidenceFromARunLocalDirectory(t *testing.T) {
	f := newProbeFixture(t)
	f.svc.cfg.AllowWebResearch = true
	loop := &capturingLoop{response: `{"self_assessment":"ok","skills":[],"rules":[],"memories":[],"reverts":[]}`}
	f.svc.agentLoop = loop

	f.run(t)

	if len(loop.dirs) != 1 || loop.dirs[0] == "" || !loop.sawFile {
		t.Fatalf("the loop run had no evidence directory: dirs=%v saw=%v", loop.dirs, loop.sawFile)
	}
	if !slices.Contains(loop.tools, "read_file") {
		t.Fatalf("tools = %v, want read_file so the run can open the files", loop.tools)
	}
	if _, err := os.Stat(loop.dirs[0]); !os.IsNotExist(err) {
		t.Fatalf("the evidence directory outlived the reflection: %v", err)
	}
}
