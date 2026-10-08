package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"golang.org/x/sync/errgroup"
)

const maxTaskRetries = 2

const persistTimeout = 10 * time.Second

// Detaches a terminal status write from its run, already cancelled on drain.
func persistCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

const dependencyTruncateNote = "\n[truncated — use run_terminal to read full output]"

type boardWriteNotLandedReasonData struct{ Declared string }
type plannedSkillFocusData struct{ Names string }
type priorAttemptNoteData struct{ ErrorText, Digest, WorkDone, FailurePattern string }
type taskPromptHeaderData struct {
	HasPurpose                        bool
	Purpose, Goal, Title, Description string
	SkillFocus                        string
}
type dependencyResultLabelData struct{ DepID string }
type actionDigestData struct {
	Prefix string
	Lines  []string
}

var (
	startedNotFinishedReasonKey  = prompt.Define[struct{}]("orchestrator.started_not_finished_reason", struct{}{})
	boardWriteNotLandedReasonKey = prompt.Define("orchestrator.board_write_not_landed_reason", boardWriteNotLandedReasonData{Declared: "create_board_task"})
	plannedSkillFocusKey         = prompt.Define("orchestrator.planned_skill_focus", plannedSkillFocusData{Names: "sample-skill"})
	priorAttemptNoteKey          = prompt.Define("orchestrator.prior_attempt_note", priorAttemptNoteData{ErrorText: "err"})
	taskPromptHeaderKey          = prompt.Define("orchestrator.task_prompt_header", taskPromptHeaderData{Title: "t", Description: "d"})
	actionDigestKey              = prompt.Define("session.action_digest", actionDigestData{
		Prefix: domain.SessionActionDigestPrefix,
		Lines:  []string{"- board_task created TT-1 (id=11111111-1111-1111-1111-111111111111)"},
	})
	dependencyResultLabelKey = prompt.Define("orchestrator.dependency_result_label", dependencyResultLabelData{DepID: "t1"})
)

type SessionActionReader interface {
	ListActions(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionAction, error)
}

type Executor struct {
	// Router in production: host-executed provider subtasks run on that host's CLI.
	agentLoop      agent.Runner
	catalog        port.CatalogStore
	cfg            domain.OrchestrationConfig
	contextBuilder *ContextBuilder
	actions        SessionActionReader
}

func NewExecutor(agentLoop agent.Runner, catalog port.CatalogStore, cfg domain.OrchestrationConfig) *Executor {
	if cfg.MaxParallelTasks <= 0 {
		cfg.MaxParallelTasks = 3
	}
	if cfg.DependencyOutputMaxChars <= 0 {
		cfg.DependencyOutputMaxChars = 4000
	}
	if cfg.SubtaskHistoryMode == "" {
		cfg.SubtaskHistoryMode = domain.SubtaskHistoryModeIsolated
	}
	return &Executor{agentLoop: agentLoop, catalog: catalog, cfg: cfg}
}

type taskContext struct {
	intake      domain.GoalIntake
	plannerTask domain.PlannerTask
	planTask    domain.PlanTask
}

func (e *Executor) Execute(ctx context.Context, planID uuid.UUID, intake domain.GoalIntake, output domain.PlannerOutput, planTasks []domain.PlanTask, history []domain.Message, defaultModel string, policy domain.ToolPolicy, lang string, sessionID uuid.UUID, seedResults map[string]string) (string, map[string]string, error) {
	// seedResults are earlier rounds' results; a depends_on naming one is pre-satisfied.
	completed := make(map[string]bool, len(seedResults))
	for id := range seedResults {
		completed[id] = true
	}

	waves, err := TopologicalWavesWithCompleted(output.Tasks, completed)
	if err != nil {
		return "", nil, err
	}

	taskByKey := make(map[string]taskContext)
	for i, pt := range output.Tasks {
		taskByKey[pt.ID] = taskContext{intake: intake, plannerTask: pt, planTask: planTasks[i]}
	}

	results := make(map[string]string)
	for k, v := range seedResults {
		results[k] = v
	}
	var resultsMu sync.Mutex

	for _, wave := range waves {
		g, waveCtx := errgroup.WithContext(ctx)
		sem := make(chan struct{}, e.cfg.MaxParallelTasks)

		for _, taskID := range wave {
			tc := taskByKey[taskID]
			g.Go(func() error {
				sem <- struct{}{}
				defer func() { <-sem }()

				result, err := e.runTask(waveCtx, planID, tc, results, &resultsMu, history, defaultModel, policy, lang, sessionID)
				if err != nil {
					return fmt.Errorf("task %s: %w", taskID, err)
				}
				resultsMu.Lock()
				results[taskID] = result
				resultsMu.Unlock()
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return "", nil, err
		}
	}

	var sb strings.Builder
	sb.WriteString(output.Summary)
	sb.WriteString("\n\n")
	for _, t := range output.Tasks {
		if r, ok := results[t.ID]; ok {
			sb.WriteString("## ")
			sb.WriteString(t.Title)
			sb.WriteString("\n")
			sb.WriteString(r)
			sb.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(sb.String()), results, nil
}

// A run with an existing checkout works in it; only a bare workspace gets the per-subtask scratch dir.
func resolveSubtaskWorkspace(sessionWorkspace, taskKey string) (string, error) {
	if sessionWorkspace == "" {
		return "", nil
	}
	if workspace.IsRepoCheckout(sessionWorkspace) {
		return sessionWorkspace, nil
	}
	dir, err := workspace.SubtaskDir(sessionWorkspace, taskKey)
	if err != nil {
		return "", err
	}
	if err := workspace.EnsureDir(dir); err != nil {
		return "", fmt.Errorf("create subtask workspace: %w", err)
	}
	return dir, nil
}

func (e *Executor) enabledAgentSkills(ctx context.Context, agentID uuid.UUID) ([]domain.Skill, error) {
	all, err := e.catalog.ListSkillsByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	enabled := make([]domain.Skill, 0, len(all))
	for _, sk := range all {
		if sk.Enabled {
			enabled = append(enabled, sk)
		}
	}
	return enabled, nil
}

func (e *Executor) runTask(ctx context.Context, planID uuid.UUID, tc taskContext, priorResults map[string]string, mu *sync.Mutex, history []domain.Message, defaultModel string, policy domain.ToolPolicy, lang string, sessionID uuid.UUID) (string, error) {
	if tc.planTask.Status == domain.TaskStatusCompleted && tc.planTask.Result != "" {
		return tc.planTask.Result, nil
	}
	if tc.planTask.AgentID == nil {
		return "", fmt.Errorf("task has no agent")
	}

	agentRec, err := e.catalog.GetAgent(ctx, *tc.planTask.AgentID)
	if err != nil {
		return "", err
	}

	skills, err := e.enabledAgentSkills(ctx, agentRec.ID)
	if err != nil {
		return "", err
	}
	stacks, err := e.catalog.ListTechStacksByAgent(ctx, agentRec.ID)
	if err != nil {
		return "", err
	}

	// An analiz task never force-adds ask_user: asking there parks the report
	// in a throwaway chat instead of the report page, and analysts use
	// record_open_questions to ask the human instead.
	taskPolicy := domain.MergeToolPolicy(policy, agentRec.ToolPolicy)
	if !domain.TaskType(registry.TaskTypeFromContext(ctx)).IsDocumentWork() {
		taskPolicy = domain.EnsureAskUserTool(taskPolicy)
	}
	taskPolicy = domain.RestrictToPlannedTools(taskPolicy, tc.plannerTask.ToolNames)
	model := agentRec.Model
	if model == "" {
		model = defaultModel
	}
	// Utility calls stay on lightModel even when the subtask escalates to ModelHeavy.
	lightModel := model
	if tc.plannerTask.Difficulty == domain.TaskDifficultyHard && agentRec.ModelHeavy != "" {
		model = agentRec.ModelHeavy
	}
	provider := agentRec.ProviderType

	_ = e.catalog.UpdateTaskStatus(ctx, tc.planTask.ID, domain.TaskStatusRunning, "", "")

	subtaskWorkspace, err := resolveSubtaskWorkspace(registry.WorkspaceDirFromContext(ctx), tc.planTask.TaskKey)
	if err != nil {
		return "", err
	}
	taskCtx := registry.ContextWithSubtaskWorkspace(ctx, subtaskWorkspace)
	taskCtx = activity.WithStepTag(taskCtx, "task_key", tc.planTask.TaskKey)

	if rec := activity.FromContext(ctx); rec != nil {
		// model/provider are recorded together or an invalid-model name reads like a provider switch.
		rec.Step("subtask_started", map[string]string{
			"task_key": tc.planTask.TaskKey, "title": tc.planTask.Title, "agent": agentRec.Name, "working_dir": subtaskWorkspace,
			"model": model, "provider": string(provider),
		})
	}

	if registry.ToolUsageFromContext(taskCtx) == nil {
		taskCtx, _ = registry.ContextWithToolUsage(taskCtx)
	}
	usage := registry.ToolUsageFromContext(taskCtx)
	effectiveTools := tc.plannerTask.ToolNames
	if len(effectiveTools) == 0 {
		effectiveTools = taskPolicy.AllowTools
	}

	var lastErr error
	var lastResult string
	var incompleteReason string
	var prior priorAttempt
	for attempt := range maxTaskRetries + 1 {
		prior.Number = attempt
		messages := e.buildTaskMessages(ctx, sessionID, history, tc, skills, stacks, agentRec, priorResults, mu, prior, subtaskWorkspace, lang)
		before := usage.Snapshot()
		resp, err := e.agentLoop.RunTask(taskCtx, messages, model, provider, taskPolicy,
			agent.WithLightModel(lightModel),
			agent.WithSessionLimits(agentRec.MaxTurns, agentRec.Effort),
			agent.WithCLILabel(tc.planTask.TaskKey, tc.planTask.Title))
		if err != nil {
			lastErr = err
			prior.Err = err
			prior.Stats, _ = agent.StatsFromError(err)
			prior.Used = registry.UsageDelta(before, usage.Snapshot())
			// A spent budget is not transient: re-running spends it again, so break and resume.
			var budgetErr *agent.BudgetExhaustedError
			if errors.As(err, &budgetErr) {
				break
			}
			prior.Digest = subtaskFindingsDigest(taskCtx, tc.planTask.TaskKey, "")
			continue
		}
		if resp.Clarification != nil {
			e.markTaskBlocked(taskCtx, tc.planTask, *resp.Clarification, resp.Message.Content)
			return "", ClarificationNeededError{Request: *resp.Clarification}
		}
		lastResult = resp.Message.Content
		delta := registry.UsageDelta(before, usage.Snapshot())
		incompleteReason = startedNotFinishedReason(delta, effectiveTools)
		if incompleteReason == "" {
			incompleteReason = boardWriteNotLandedReason(delta, effectiveTools)
		}
		if incompleteReason != "" && attempt < maxTaskRetries {
			lastErr = errors.New(incompleteReason)
			prior.Err = lastErr
			prior.Stats = agent.RunStats{}
			prior.Used = delta
			prior.Digest = subtaskFindingsDigest(taskCtx, tc.planTask.TaskKey, resp.Message.Content)
			continue
		}
		lastErr = nil
		break
	}

	if lastErr != nil {
		e.finishTask(ctx, tc.planTask.ID, domain.TaskStatusFailed, "", lastErr.Error())
		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("subtask_failed", map[string]string{
				"task_key": tc.planTask.TaskKey, "error": lastErr.Error(),
			})
		}
		return "", lastErr
	}

	// Out of attempts: record the honest status but hand the result back rather than failing the run.
	if incompleteReason != "" {
		e.finishTask(ctx, tc.planTask.ID, domain.TaskStatusIncomplete, lastResult, incompleteReason)
		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("subtask_incomplete", map[string]string{
				"task_key": tc.planTask.TaskKey, "reason": incompleteReason,
			})
		}
		return lastResult, nil
	}

	e.finishTask(ctx, tc.planTask.ID, domain.TaskStatusCompleted, lastResult, "")
	if rec := activity.FromContext(ctx); rec != nil {
		preview := lastResult
		if len(preview) > 500 {
			preview = domain.TruncateHead(preview, 500)
		}
		rec.Step("subtask_completed", map[string]string{
			"task_key": tc.planTask.TaskKey, "result": preview,
		})
	}
	return lastResult, nil
}

// What the failed attempt already did, read from the run's activity trace so a retry continues past it.
func subtaskFindingsDigest(ctx context.Context, taskKey, summary string) string {
	rec := activity.FromContext(ctx)
	if rec == nil {
		return ""
	}
	return agent.DigestFromSteps(stepsSinceSubtaskStart(rec.Steps(ctx), taskKey), summary, 0)
}

func stepsSinceSubtaskStart(steps []domain.SessionStep, taskKey string) []domain.SessionStep {
	if taskKey == "" {
		return nil
	}
	start := -1
	for i, step := range steps {
		if step.StepType != "subtask_started" {
			continue
		}
		var payload struct {
			TaskKey string `json:"task_key"`
		}
		if json.Unmarshal(step.Payload, &payload) == nil && payload.TaskKey == taskKey {
			start = i
		}
	}
	if start < 0 {
		return nil
	}
	return steps[start+1:]
}

func (e *Executor) finishTask(ctx context.Context, planTaskID uuid.UUID, status, result, errMsg string) {
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	_ = e.catalog.UpdateTaskStatus(pctx, planTaskID, status, result, errMsg)
}

// Failed is the resumable status, so a blocked subtask re-runs once the answer arrives.
func (e *Executor) markTaskBlocked(ctx context.Context, planTask domain.PlanTask, req domain.ClarificationRequest, partial string) {
	reason := clarificationBlockedReason(req)
	e.finishTask(ctx, planTask.ID, domain.TaskStatusFailed, partial, reason)
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("subtask_failed", map[string]string{
			"task_key": planTask.TaskKey, "error": reason,
		})
	}
}

type clarificationBlockedReasonData struct{ Detail string }

var clarificationBlockedReasonKey = prompt.Define("orchestrator.clarification_blocked_reason", clarificationBlockedReasonData{Detail: ""})

func clarificationBlockedReason(req domain.ClarificationRequest) string {
	detail := req.Context
	if len(req.Questions) > 0 && req.Questions[0].Prompt != "" {
		detail = req.Questions[0].Prompt
	}
	if strings.TrimSpace(detail) == "" {
		detail = ""
	}
	return clarificationBlockedReasonKey.Render(clarificationBlockedReasonData{Detail: detail})
}

// Only empty-ledger and board-only declared subtasks pass; a subtask that claimed and moved stops short.
func startedNotFinishedReason(delta map[string]int, effectiveTools []string) string {
	if len(delta) == 0 {
		return ""
	}
	for name := range delta {
		if !domain.IsBoardProgressTool(name) {
			return ""
		}
	}
	if !hasWorkTool(effectiveTools) {
		return ""
	}
	return prompt.Text(startedNotFinishedReasonKey)
}

// A pure-bookkeeping subtask whose declared board write never landed is not finished.
func boardWriteNotLandedReason(delta map[string]int, effectiveTools []string) string {
	if hasWorkTool(effectiveTools) {
		return ""
	}
	var declared []string
	for _, name := range effectiveTools {
		if domain.IsBoardWriteTool(name) {
			declared = append(declared, name)
		}
	}
	if len(declared) == 0 {
		return ""
	}
	for _, name := range declared {
		if delta[name] > 0 {
			return ""
		}
	}
	return boardWriteNotLandedReasonKey.Render(boardWriteNotLandedReasonData{Declared: strings.Join(declared, ", ")})
}

// Planner picks are a hint, never a cap on the agent's other skills.
func plannedSkillFocus(skillIDs []string, skills []domain.Skill) string {
	if len(skillIDs) == 0 || len(skills) == 0 {
		return ""
	}
	picked := make(map[string]bool, len(skillIDs))
	for _, id := range skillIDs {
		picked[strings.TrimSpace(id)] = true
	}
	names := make([]string, 0, len(skillIDs))
	for _, sk := range skills {
		if picked[sk.ID.String()] {
			names = append(names, sk.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return plannedSkillFocusKey.Render(plannedSkillFocusData{Names: strings.Join(names, ", ")})
}

func hasWorkTool(effectiveTools []string) bool {
	for _, name := range effectiveTools {
		if domain.IsBoardProgressTool(name) || name == domain.AskUserToolName {
			continue
		}
		return true
	}
	return false
}

// What the failed attempt left behind, so a retry becomes a continuation.
type priorAttempt struct {
	Number int
	Err    error
	Stats  agent.RunStats
	// Per-tool call counts even when the loop returned no stats.
	Used map[string]int
	// What the attempt touched, rendered from the run's activity trace.
	Digest string
}

func (p priorAttempt) note() string {
	if p.Number == 0 || p.Err == nil {
		return ""
	}
	return priorAttemptNoteKey.Render(priorAttemptNoteData{
		ErrorText:      p.Err.Error(),
		Digest:         p.Digest,
		WorkDone:       p.workDone(),
		FailurePattern: p.Stats.FailurePattern(),
	})
}

func (p priorAttempt) workDone() string {
	counts := p.Stats.ByTool
	if len(counts) == 0 {
		counts = p.Used
	}
	if len(counts) == 0 {
		return ""
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s×%d", name, counts[name]))
	}
	out := "- tool calls: " + strings.Join(parts, ", ")
	if len(p.Stats.LastCalls) > 0 {
		out += "\n- it stopped on: " + strings.Join(p.Stats.LastCalls, " → ")
	}
	return out
}

func (e *Executor) buildTaskMessages(ctx context.Context, sessionID uuid.UUID, history []domain.Message, tc taskContext, skills []domain.Skill, stacks []domain.TechStack, agentRec domain.Agent, priorResults map[string]string, mu *sync.Mutex, prior priorAttempt, subtaskWorkspace, lang string) []domain.Message {
	systemContent := prompt.BuildSystemPrompt(agentRec, skills, stacks, tc.plannerTask.SubtaskRules, lang)

	var taskPrompt strings.Builder
	taskPrompt.WriteString(taskPromptHeaderKey.Render(taskPromptHeaderData{
		HasPurpose:  tc.intake.Purpose != "" || tc.intake.Goal != "",
		Purpose:     tc.intake.Purpose,
		Goal:        tc.intake.Goal,
		Title:       tc.plannerTask.Title,
		Description: tc.plannerTask.Description,
		SkillFocus:  plannedSkillFocus(tc.plannerTask.SkillIDs, skills),
	}))
	taskPrompt.WriteString(prompt.AskUserTaskGuidance())
	if subtaskWorkspace != "" {
		taskPrompt.WriteString(prompt.SubtaskWorkspaceNote(subtaskWorkspace))
	}

	mu.Lock()
	for _, dep := range tc.plannerTask.DependsOn {
		if r, ok := priorResults[dep]; ok {
			if agentRec.SubagentType == "generalPurpose" {
				if e.contextBuilder != nil {
					taskPrompt.WriteString("\n\n")
					taskPrompt.WriteString(e.contextBuilder.FormatExplorerFindings(dep, dep, truncateDependencyOutput(r, e.cfg.DependencyOutputMaxChars)))
					continue
				}
			}
			taskPrompt.WriteString(dependencyResultLabelKey.Render(dependencyResultLabelData{DepID: dep}))
			taskPrompt.WriteString(truncateDependencyOutput(r, e.cfg.DependencyOutputMaxChars))
		}
	}
	mu.Unlock()

	taskPrompt.WriteString(prior.note())

	taskHistory := history
	if e.cfg.SubtaskHistoryMode == domain.SubtaskHistoryModeIsolated {
		taskHistory = isolatedSubtaskHistory(history)
	}
	taskHistory = e.withFreshActionDigest(ctx, sessionID, taskHistory)

	messages := make([]domain.Message, 0, len(taskHistory)+3)
	messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: systemContent})

	if e.contextBuilder != nil && sessionID != uuid.Nil {
		if agentRec.SubagentType == "explore" {
			if ctxMsgs, err := e.contextBuilder.BuildExplorerContext(ctx, sessionID, tc.plannerTask.Description); err == nil {
				for _, m := range ctxMsgs {
					if m.Role == domain.RoleSystem {
						messages = append(messages, m)
					}
				}
			}
		}
	}

	messages = append(messages, taskHistory...)
	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: taskPrompt.String()})
	return messages
}

// RenderActionDigest renders a session's action ledger as a system message,
// shared by application/session and application/orchestrator — the fix for
// the failure this ledger exists for: without it the model re-creates records
// it made in an earlier turn, because the tool trace that held their ids was
// never persisted. "" when there is nothing to report.
func RenderActionDigest(actions []domain.SessionAction) string {
	lines := domain.SessionActionDigestLines(actions)
	if len(lines) == 0 {
		return ""
	}
	return actionDigestKey.Render(actionDigestData{Prefix: domain.SessionActionDigestPrefix, Lines: lines})
}

// Re-reads the ledger so a subtask sees records its dependencies just created; stale is still kept.
func (e *Executor) withFreshActionDigest(ctx context.Context, sessionID uuid.UUID, history []domain.Message) []domain.Message {
	if e.actions == nil || sessionID == uuid.Nil {
		return history
	}
	actions, err := e.actions.ListActions(ctx, sessionID)
	if err != nil || len(actions) == 0 {
		return history
	}
	digest := RenderActionDigest(actions)
	if digest == "" {
		return history
	}
	out := make([]domain.Message, 0, len(history)+1)
	for _, m := range history {
		if m.Role == domain.RoleSystem && domain.IsSessionActionDigest(m.Content) {
			continue
		}
		out = append(out, m)
	}
	return append(out, domain.Message{Role: domain.RoleSystem, Content: digest})
}

// Keeps every user turn and the action ledger; drops tool chatter and rebuilt system prompts.
func isolatedSubtaskHistory(history []domain.Message) []domain.Message {
	var out []domain.Message
	for _, m := range history {
		switch m.Role {
		case domain.RoleSystem:
			if domain.IsSessionActionDigest(m.Content) {
				out = append(out, m)
			}
		case domain.RoleUser:
			out = append(out, m)
		case domain.RoleAssistant:
			if len(m.ToolCalls) == 0 && m.Content != "" {
				out = append(out, m)
			}
		}
	}
	return out
}

func truncateDependencyOutput(content string, maxChars int) string {
	if maxChars <= 0 || len(content) <= maxChars {
		return content
	}
	// Byte-safe: a raw slice can land mid-rune in Turkish output.
	return domain.TruncateHead(content, maxChars) + dependencyTruncateNote
}
