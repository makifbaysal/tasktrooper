package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The minimum activity store runPipeline needs: it refuses to start without a run id.
type runRecorderStore struct{ port.ActivityStore }

func (runRecorderStore) CreateRun(_ context.Context, sessionID *uuid.UUID, requestID, model string) (domain.SessionRun, error) {
	return domain.SessionRun{ID: uuid.New(), SessionID: sessionID, RequestID: requestID, Model: model}, nil
}

func (runRecorderStore) AppendStep(_ context.Context, _ uuid.UUID, _ string, _ []byte) error {
	return nil
}

func (runRecorderStore) CompleteRun(_ context.Context, _ uuid.UUID, _ string) error { return nil }

// A store with no trace is the "no findings to carry" retry case.
func (runRecorderStore) ListStepsByRunSince(context.Context, uuid.UUID, time.Time) ([]domain.SessionStep, error) {
	return nil, nil
}

func (runRecorderStore) ListStepsByRun(_ context.Context, _ uuid.UUID) ([]domain.SessionStep, error) {
	return nil, nil
}

// No tools, so a subtask is one request and one answer.
type toollessRegistry struct{ port.ToolRegistry }

func (toollessRegistry) DefinitionsForPolicy(_ domain.ToolPolicy) []domain.ToolDefinition { return nil }

// In-memory plan store that records every status write.
type pipelineCatalog struct {
	port.CatalogStore
	mu           sync.Mutex
	agent        domain.Agent
	tasks        []domain.PlanTask
	planStatuses []string
	appendedKeys []string
}

func (c *pipelineCatalog) ListAgents(_ context.Context) ([]domain.Agent, error) {
	return []domain.Agent{c.agent}, nil
}

func (c *pipelineCatalog) GetAgent(_ context.Context, _ uuid.UUID) (domain.Agent, error) {
	return c.agent, nil
}

func (c *pipelineCatalog) ListSkillsByAgent(_ context.Context, _ uuid.UUID) ([]domain.Skill, error) {
	return nil, nil
}

func (c *pipelineCatalog) ListEnabledRulesByAgent(_ context.Context, _ uuid.UUID) ([]domain.OrchestratorRule, error) {
	return nil, nil
}

func (c *pipelineCatalog) store(planID uuid.UUID, tasks []domain.PlanTask) []domain.PlanTask {
	stored := make([]domain.PlanTask, 0, len(tasks))
	for _, t := range tasks {
		t.ID = uuid.New()
		t.PlanID = planID
		stored = append(stored, t)
	}
	c.tasks = append(c.tasks, stored...)
	return stored
}

func (c *pipelineCatalog) CreatePlan(_ context.Context, plan domain.OrchestrationPlan, tasks []domain.PlanTask) (domain.OrchestrationPlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	plan.ID = uuid.New()
	c.store(plan.ID, tasks)
	return plan, nil
}

func (c *pipelineCatalog) AppendPlanTasks(_ context.Context, planID uuid.UUID, tasks []domain.PlanTask) ([]domain.PlanTask, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range tasks {
		c.appendedKeys = append(c.appendedKeys, t.TaskKey)
	}
	return c.store(planID, tasks), nil
}

func (c *pipelineCatalog) ListPlanTasks(_ context.Context, _ uuid.UUID) ([]domain.PlanTask, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]domain.PlanTask, len(c.tasks))
	copy(out, c.tasks)
	return out, nil
}

func (c *pipelineCatalog) UpdatePlanStatus(_ context.Context, _ uuid.UUID, status string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.planStatuses = append(c.planStatuses, status)
	return nil
}

func (c *pipelineCatalog) UpdatePlanJSON(_ context.Context, _ uuid.UUID, _ []byte) error { return nil }

func (c *pipelineCatalog) UpdateTaskStatus(_ context.Context, taskID uuid.UUID, status, result, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.tasks {
		if c.tasks[i].ID == taskID {
			c.tasks[i].Status = status
			if result != "" {
				c.tasks[i].Result = result
			}
		}
	}
	return nil
}

func (c *pipelineCatalog) finalPlanStatus() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.planStatuses) == 0 {
		return ""
	}
	return c.planStatuses[len(c.planStatuses)-1]
}

// Answers each stage by recognising its system prompt, not by call ordinal.
type pipelineLLM struct {
	mu            sync.Mutex
	intake        string
	plan          string
	replans       []string
	verifications []string
	subtaskReply  string
	// A really-failing subtask is the only route to the "process died" exit.
	subtaskErr     error
	subtaskPrompts []string
	replanCalls    int
	verifyCalls    int
}

func scripted(list []string, i int) string {
	if len(list) == 0 {
		return ""
	}
	if i >= len(list) {
		return list[len(list)-1]
	}
	return list[i]
}

func (l *pipelineLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	var system string
	if len(req.Messages) > 0 && req.Messages[0].Role == domain.RoleSystem {
		system = req.Messages[0].Content
	}
	answer := ""
	switch {
	case strings.Contains(system, "You extract the user's purpose"):
		answer = l.intake
	case strings.Contains(system, "You are an orchestration planner"):
		answer = l.plan
	case strings.Contains(system, "You are an orchestration replanner"):
		answer = scripted(l.replans, l.replanCalls)
		l.replanCalls++
	case strings.Contains(system, "You verify whether orchestration task results"):
		answer = scripted(l.verifications, l.verifyCalls)
		l.verifyCalls++
	default:
		// The whole conversation, not just the last turn: the agent loop appends its own system message.
		var sb strings.Builder
		for _, m := range req.Messages {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
		l.subtaskPrompts = append(l.subtaskPrompts, sb.String())
		if l.subtaskErr != nil {
			return domain.AgentResponse{}, l.subtaskErr
		}
		answer = l.subtaskReply
	}
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: answer}}, nil
}

func (l *pipelineLLM) ChatStream(_ context.Context, _ domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}
func (l *pipelineLLM) Models(_ context.Context) ([]string, error) { return nil, nil }
func (l *pipelineLLM) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return nil, nil
}

func (l *pipelineLLM) sawSubtaskPrompt(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range l.subtaskPrompts {
		if strings.Contains(p, substr) {
			return true
		}
	}
	return false
}

const (
	readyIntake      = `{"ready":true,"purpose":"p","goal":"g","constraints":[],"questions":[]}`
	failedVerdict    = `{"passed":false,"issues":["the check was never run"],"summary":"gap"}`
	passedVerdict    = `{"passed":true,"issues":[],"summary":"ok"}`
	emptyRepairPlan  = `{"summary":"repair","tasks":[]}`
	replanIterations = 1
)

func firstPlanJSON(agentID uuid.UUID) string {
	return `{"ready":true,"summary":"plan","questions":[],"tasks":[
		{"id":"t1","title":"Ship the change","description":"Do the work","agent_id":"` + agentID.String() +
		`","skill_ids":[],"tool_names":["run_terminal"],"subtask_rules":[],"depends_on":[],"parallel_group":0}]}`
}

// The repair plan the replanner prompt asks for: a new task dependent on the original plan's task.
func repairPlanJSON(agentID uuid.UUID) string {
	return `{"summary":"repair","tasks":[
		{"id":"r1","title":"Run the check","description":"Run it","agent_id":"` + agentID.String() +
		`","skill_ids":[],"tool_names":["run_terminal"],"subtask_rules":[],"depends_on":["t1"],"parallel_group":0}]}`
}

func runPipelineFixture(t *testing.T, llm *pipelineLLM, catalog *pipelineCatalog) (domain.AgentResponse, error) {
	t.Helper()
	loop := agent.NewLoop(llm, toollessRegistry{}, 3, 3, 4000)
	svc := orchestrator.NewService(llm, catalog, nil, loop, domain.OrchestrationConfig{
		Enabled:             true,
		MaxParallelTasks:    1,
		MaxPlanTasks:        10,
		VerificationEnabled: true,
		MaxReplanIterations: replanIterations,
	}, nil)

	ctx, _, err := activity.StartRun(context.Background(), runRecorderStore{}, nil, "req", "test-model")
	require.NoError(t, err)

	return svc.Run(ctx, "siteye android linki ekle", nil, "test-model", domain.ToolPolicy{}, "tr")
}

// Verification fails, the repair task runs against the original task, and the run repairs itself.
func TestRunPipeline_RepairTaskMayDependOnTheOriginalPlan(t *testing.T) {
	agentID := uuid.New()
	catalog := &pipelineCatalog{agent: domain.Agent{ID: agentID, Name: "backend-developer", Enabled: true}}
	llm := &pipelineLLM{
		intake:        readyIntake,
		plan:          firstPlanJSON(agentID),
		replans:       []string{repairPlanJSON(agentID)},
		verifications: []string{failedVerdict, passedVerdict},
		subtaskReply:  "done",
	}

	resp, err := runPipelineFixture(t, llm, catalog)

	require.NoError(t, err)
	assert.Equal(t, 1, llm.replanCalls, "the repair plan is accepted on the first attempt, not retried against itself")
	assert.Contains(t, catalog.appendedKeys, "r1", "the repair task has to reach the plan")
	assert.True(t, llm.sawSubtaskPrompt("Result from dependency t1"),
		"a repair task whose dependency already completed runs immediately, with that task's result in hand")
	assert.Equal(t, domain.PlanStatusCompleted, catalog.finalPlanStatus(),
		"the repair ran and verification passed")
	assert.NotEmpty(t, resp.Message.Content)
}

// When the repair iteration gives up, results still ship, but the plan must not claim it finished.
func TestRunPipeline_AbandonedRepairDoesNotReportCompleted(t *testing.T) {
	agentID := uuid.New()
	catalog := &pipelineCatalog{agent: domain.Agent{ID: agentID, Name: "backend-developer", Enabled: true}}
	llm := &pipelineLLM{
		intake:        readyIntake,
		plan:          firstPlanJSON(agentID),
		replans:       []string{emptyRepairPlan},
		verifications: []string{failedVerdict},
		subtaskReply:  "done",
	}

	resp, err := runPipelineFixture(t, llm, catalog)

	require.NoError(t, err, "completed work is still returned; only the plan status changes")
	assert.NotEmpty(t, resp.Message.Content)
	assert.Empty(t, catalog.appendedKeys, "no repair task was ever produced")
	assert.Equal(t, domain.PlanStatusIncomplete, catalog.finalPlanStatus(),
		"a verification issue nothing repaired is a plan that ran without being confirmed, not one that broke")
}

// Exhaustion: every repair iteration ran and the verifier still rejects the goal.
func TestRunPipeline_ExhaustedRepairIterationsDoNotReportCompleted(t *testing.T) {
	agentID := uuid.New()
	catalog := &pipelineCatalog{agent: domain.Agent{ID: agentID, Name: "backend-developer", Enabled: true}}
	llm := &pipelineLLM{
		intake:        readyIntake,
		plan:          firstPlanJSON(agentID),
		replans:       []string{repairPlanJSON(agentID)},
		verifications: []string{failedVerdict},
		subtaskReply:  "done",
	}

	resp, err := runPipelineFixture(t, llm, catalog)

	require.NoError(t, err, "completed work is still returned; only the plan status changes")
	assert.NotEmpty(t, resp.Message.Content)
	assert.Equal(t, replanIterations, llm.replanCalls, "the repair budget was really spent, not abandoned")
	assert.Contains(t, catalog.appendedKeys, "r1", "the repair task ran — this is exhaustion, not a broken pipeline")
	assert.Equal(t, domain.PlanStatusIncomplete, catalog.finalPlanStatus(),
		"a run the verifier still rejects after every repair round is not a completed plan — and nothing broke, so it is not a failed one either")
}

// A verifier that never produced a verdict is inconclusive, not a failure.
func TestRunPipeline_InconclusiveVerificationStillCompletes(t *testing.T) {
	agentID := uuid.New()
	catalog := &pipelineCatalog{agent: domain.Agent{ID: agentID, Name: "backend-developer", Enabled: true}}
	llm := &pipelineLLM{
		intake:        readyIntake,
		plan:          firstPlanJSON(agentID),
		verifications: []string{"not json at all"},
		subtaskReply:  "done",
	}

	resp, err := runPipelineFixture(t, llm, catalog)

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Message.Content)
	assert.Equal(t, 0, llm.replanCalls, "no verdict means no issues to repair")
	assert.Equal(t, domain.PlanStatusCompleted, catalog.finalPlanStatus(),
		"an unparseable verifier reply is inconclusive, not a verdict against the run")
}

// A run the verifier passes first time never enters the repair loop.
func TestRunPipeline_PassingVerificationStillCompletes(t *testing.T) {
	agentID := uuid.New()
	catalog := &pipelineCatalog{agent: domain.Agent{ID: agentID, Name: "backend-developer", Enabled: true}}
	llm := &pipelineLLM{
		intake:        readyIntake,
		plan:          firstPlanJSON(agentID),
		verifications: []string{passedVerdict},
		subtaskReply:  "done",
	}

	_, err := runPipelineFixture(t, llm, catalog)

	require.NoError(t, err)
	assert.Equal(t, 0, llm.replanCalls)
	assert.Equal(t, domain.PlanStatusCompleted, catalog.finalPlanStatus())
}

// Which terminal path settles which status: failed = stopped early, incomplete = reached the end unverified.
func TestRunPipeline_TerminalPlanStatus(t *testing.T) {
	cases := []struct {
		name    string
		llm     func(agentID uuid.UUID) *pipelineLLM
		want    string
		wantErr bool
		why     string
	}{
		{
			name: "verifier passes",
			llm: func(agentID uuid.UUID) *pipelineLLM {
				return &pipelineLLM{intake: readyIntake, plan: firstPlanJSON(agentID),
					verifications: []string{passedVerdict}, subtaskReply: "done"}
			},
			want: domain.PlanStatusCompleted,
			why:  "the run reached the end and the judge confirmed it",
		},
		{
			name: "verifier never produced a verdict",
			llm: func(agentID uuid.UUID) *pipelineLLM {
				return &pipelineLLM{intake: readyIntake, plan: firstPlanJSON(agentID),
					verifications: []string{"not json at all"}, subtaskReply: "done"}
			},
			want: domain.PlanStatusCompleted,
			why:  "inconclusive is not a verdict against the run; a judge that never spoke convicts nobody",
		},
		{
			name: "verifier rejects and the repair is abandoned",
			llm: func(agentID uuid.UUID) *pipelineLLM {
				return &pipelineLLM{intake: readyIntake, plan: firstPlanJSON(agentID),
					replans: []string{emptyRepairPlan}, verifications: []string{failedVerdict}, subtaskReply: "done"}
			},
			want: domain.PlanStatusIncomplete,
			why:  "it ran to the end; nothing confirmed the result",
		},
		{
			name: "verifier rejects through every repair iteration",
			llm: func(agentID uuid.UUID) *pipelineLLM {
				return &pipelineLLM{intake: readyIntake, plan: firstPlanJSON(agentID),
					replans: []string{repairPlanJSON(agentID)}, verifications: []string{failedVerdict}, subtaskReply: "done"}
			},
			want: domain.PlanStatusIncomplete,
			why:  "exhaustion is the same thing to the stakeholder as abandonment: unverified, not broken",
		},
		{
			name: "the executor dies",
			llm: func(agentID uuid.UUID) *pipelineLLM {
				return &pipelineLLM{intake: readyIntake, plan: firstPlanJSON(agentID),
					verifications: []string{passedVerdict}, subtaskErr: errors.New("provider went away")}
			},
			want:    domain.PlanStatusFailed,
			wantErr: true,
			why:     "the process stopped before the end — this is the one thing 'failed' still means",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentID := uuid.New()
			catalog := &pipelineCatalog{agent: domain.Agent{ID: agentID, Name: "backend-developer", Enabled: true}}

			_, err := runPipelineFixture(t, tc.llm(agentID), catalog)

			if tc.wantErr {
				require.Error(t, err, "a run whose process died must not report success")
			} else {
				require.NoError(t, err, "work that was produced is still returned; only the plan status carries the verdict")
			}
			assert.Equal(t, tc.want, catalog.finalPlanStatus(), tc.why)
		})
	}
}

func (c *pipelineCatalog) ListTechStacksByAgent(_ context.Context, _ uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}
