package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// Compact before/after payloads stored on evolution events (embeddings intentionally excluded).
type skillSnapshot struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags,omitempty"`
	Content     string     `json:"content"`
	Enabled     bool       `json:"enabled"`
	TechStackID *uuid.UUID `json:"tech_stack_id,omitempty"`
	SourceURLs  []string   `json:"source_urls,omitempty"`
}

type ruleSnapshot struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Content  string    `json:"content"`
	Priority int       `json:"priority"`
	Enabled  bool      `json:"enabled"`
}

func snapshotSkill(sk domain.Skill, sources []string) json.RawMessage {
	raw, _ := json.Marshal(skillSnapshot{
		ID: sk.ID, Name: sk.Name, Description: sk.Description, Category: sk.Category,
		Tags: sk.Tags, Content: sk.Content, Enabled: sk.Enabled, TechStackID: sk.TechStackID,
		SourceURLs: sources,
	})
	return raw
}

func snapshotRule(r domain.OrchestratorRule) json.RawMessage {
	raw, _ := json.Marshal(ruleSnapshot{ID: r.ID, Name: r.Name, Content: r.Content, Priority: r.Priority, Enabled: r.Enabled})
	return raw
}

func (s *Service) processReflection(ctx context.Context, reflection domain.AgentReflection) {
	agentRec, err := s.catalog.GetAgent(ctx, reflection.AgentID)
	if err != nil {
		s.failReflection(ctx, reflection, "", fmt.Sprintf("agent not found: %v", err))
		return
	}

	withFiles := s.reflectionIsAgentic()
	bundle, err := s.gatherEvidence(ctx, agentRec, reflection, withFiles)
	if err != nil {
		s.failReflection(ctx, reflection, "", fmt.Sprintf("evidence gathering failed: %v", err))
		return
	}
	skills, rules := bundle.Skills, bundle.Rules
	var files map[string]string
	if withFiles {
		files = evidenceFiles(bundle.Full, bundle.Skills, bundle.Rules, bundle.Runs)
	}

	call := s.newReflectionCall(agentRec, bundle.Report, files)
	defer call.close()
	output, rawOutput, analysis, err := call.run(ctx)
	if err != nil {
		s.failReflection(ctx, reflection, rawOutput, err.Error())
		return
	}

	catalogBefore := domain.ReflectionCatalogCounts{
		Skills: len(skills), Rules: len(rules), Memories: s.countMemories(ctx, agentRec.ID),
		SkillBudget: s.cfg.MaxSkillsPerAgent, RuleBudget: s.cfg.MaxRulesPerAgent,
	}

	var outcomes []domain.ReflectionChangeOutcome
	var plan []plannedChange
	if agentRec.SelfEvolutionEnabled {
		var problems map[int]planProblem
		plan, outcomes, problems = s.planAndValidate(output, skills, rules)
		if len(problems) > 0 {
			// One chance to fix it, as with unparseable JSON: the reflection hears exactly what was wrong, and whatever it still gets wrong is dropped rather than retried forever.
			revised, raw2, analysis2, err := call.revise(ctx, validationFeedback(problems))
			if err == nil {
				output, rawOutput, analysis = revised, raw2, analysis2
				plan, outcomes, problems = s.planAndValidate(output, skills, rules)
			} else {
				log.Warn().Err(err).Str("agent", agentRec.Name).Msg("reflection revision after validation failed; dropping the invalid changes")
			}
			plan, outcomes = s.dropInvalid(plan, outcomes, problems, skills, rules)
		}
	}

	adopt := map[int]bool{}
	for _, c := range plan {
		adopt[c.Index] = true
	}
	var gateResult *domain.ReflectionGateResult
	var lesson *domain.ReflectionLesson
	var probe probeOutcome
	probed := s.cfg.GoldenGate && agentRec.SelfEvolutionEnabled && len(plan) > 0
	if probed {
		cases := s.probeCases(ctx, agentRec, reflection)
		if len(cases) == 0 {
			probe = probeOutcome{Unmeasured: true, AdoptedSet: map[int]bool{}, Verdicts: map[int]string{}}
		} else {
			probe = s.runProbe(ctx, agentRec, reflection, skills, rules, plan, cases)
		}
		adopt = probe.AdoptedSet
		gateResult = probeGateResult(probe, len(cases), plan)
		lesson = s.distillLesson(ctx, agentRec, plan, probe)
	}

	// With self-evolution off there is no plan; the untouched output goes through so every proposed skill/rule change is still recorded as skipped.
	adopted := output
	if agentRec.SelfEvolutionEnabled {
		adopted = adoptedOutput(output, plan, adopt)
	}
	applyLog, _, applied := s.applyOutput(ctx, agentRec, reflection, adopted, skills, rules)
	annotateOutcomes(applied, plan, probe.Verdicts)
	for _, c := range plan {
		if adopt[c.Index] {
			continue
		}
		o := c.outcome(domain.ReflectionOutcomeNotAdopted, notAdoptedDetail(probe))
		o.Verdict = probe.Verdicts[c.Index]
		outcomes = append(outcomes, o)
	}
	outcomes = append(applied, outcomes...)

	selfAssessment := strings.TrimSpace(output.SelfAssessment)
	summary := selfAssessment
	if summary == "" && analysis != "" {
		summary = truncate(analysis, 600)
	}
	if len(applyLog) > 0 {
		summary = summary + "\n\nApplied changes:\n- " + strings.Join(applyLog, "\n- ")
	}
	if gateResult != nil {
		summary += "\n\n" + probeSummaryLine(*gateResult)
	}
	if len(summary) > 4000 {
		summary = summary[:4000]
	}

	snapshot := s.captureSnapshot(ctx, reflection.AgentID)
	snapshot.GoldenPassRateBefore = probe.BaseGolden
	snapshot.GoldenPassRate = probe.AfterGold

	catalogAfter := domain.ReflectionCatalogCounts{
		Skills: catalogBefore.Skills, Rules: catalogBefore.Rules,
		SkillBudget: s.cfg.MaxSkillsPerAgent, RuleBudget: s.cfg.MaxRulesPerAgent,
	}
	if after, err := s.catalog.ListSkillsByAgent(ctx, agentRec.ID); err == nil {
		catalogAfter.Skills = len(after)
	}
	if after, err := s.catalog.ListRulesByAgent(ctx, agentRec.ID); err == nil {
		catalogAfter.Rules = len(after)
	}
	catalogAfter.Memories = s.countMemories(ctx, agentRec.ID)

	reflection.Status = domain.ReflectionStatusCompleted
	reflection.Summary = summary
	reflection.RawOutput = rawOutput
	reflection.PerformanceSnapshot = snapshot
	reflection.Decision = &domain.ReflectionDecision{
		Analysis:       analysis,
		SelfAssessment: selfAssessment,
		Baseline:       bundle.Baseline,
		CatalogBefore:  catalogBefore,
		CatalogAfter:   catalogAfter,
		Changes:        outcomes,
		Gate:           gateResult,
		Lesson:         lesson,
	}
	if err := s.store.CompleteReflection(ctx, reflection); err != nil {
		log.Warn().Err(err).Msg("complete reflection failed")
	}
	log.Info().Str("agent", agentRec.Name).Str("trigger", reflection.Trigger).Int("changes", len(applyLog)).Msg("reflection completed")
}

func (s *Service) reflectionIsAgentic() bool {
	return s.cfg.AllowWebResearch && s.agentLoop != nil
}

func (s *Service) planAndValidate(
	output domain.ReflectionOutput,
	skills []domain.Skill,
	rules []domain.OrchestratorRule,
) ([]plannedChange, []domain.ReflectionChangeOutcome, map[int]planProblem) {
	plan, outcomes := s.planCatalogChanges(output, skills, rules)
	return plan, outcomes, s.validatePlan(skills, rules, plan)
}

// Dropping one change can break another (a rule that pointed at a skill whose create was invalid), so the rest is checked again until it stands on its own.
func (s *Service) dropInvalid(
	plan []plannedChange,
	outcomes []domain.ReflectionChangeOutcome,
	problems map[int]planProblem,
	skills []domain.Skill,
	rules []domain.OrchestratorRule,
) ([]plannedChange, []domain.ReflectionChangeOutcome) {
	for round := 0; len(problems) > 0 && round < 4; round++ {
		var kept []plannedChange
		for _, c := range plan {
			if p, bad := problems[c.Index]; bad {
				outcomes = append(outcomes, c.outcome(domain.ReflectionOutcomeRejected, renderProblem(p)))
				continue
			}
			kept = append(kept, c)
		}
		plan = kept
		problems = s.validatePlan(skills, rules, plan)
	}
	if len(problems) > 0 {
		for _, c := range plan {
			outcomes = append(outcomes, c.outcome(domain.ReflectionOutcomeRejected, renderProblem(problems[c.Index])))
		}
		return nil, outcomes
	}
	return plan, outcomes
}

// The output with its skill and rule changes narrowed to the adopted part of the plan, already normalized (create-by-existing-name turned into update), so the apply path writes exactly what was rehearsed. Memories and reverts pass through: memories are not what the probe measures, and a revert is a deliberate repair of something already judged.
func adoptedOutput(output domain.ReflectionOutput, plan []plannedChange, adopt map[int]bool) domain.ReflectionOutput {
	out := domain.ReflectionOutput{
		SelfAssessment: output.SelfAssessment, Memories: output.Memories, Reverts: output.Reverts,
	}
	for _, c := range plan {
		if !adopt[c.Index] {
			continue
		}
		switch c.Kind {
		case "skill":
			out.Skills = append(out.Skills, c.Skill)
		case "rule":
			out.Rules = append(out.Rules, c.Rule)
		}
	}
	return out
}

func annotateOutcomes(outcomes []domain.ReflectionChangeOutcome, plan []plannedChange, verdicts map[int]string) {
	byKey := map[string]plannedChange{}
	for _, c := range plan {
		byKey[c.Kind+"|"+c.Action+"|"+nameKey(c.Name)] = c
	}
	for i := range outcomes {
		c, ok := byKey[outcomes[i].Kind+"|"+outcomes[i].Action+"|"+nameKey(outcomes[i].Name)]
		if !ok {
			continue
		}
		outcomes[i].Dimension = c.Dimension
		outcomes[i].Verdict = verdicts[c.Index]
	}
}

func notAdoptedDetail(probe probeOutcome) string {
	switch {
	case probe.Unmeasured && probe.Unservable != nil:
		return "the agent's engine could not run the rehearsal, so nothing showed this change helps"
	case probe.Unmeasured:
		return "there was nothing to rehearse it on (no golden tasks, recent tasks or chats), so nothing showed this change helps"
	case probe.Adopted == "":
		return "no variant of the change set did better than the current setup"
	default:
		return fmt.Sprintf("the %s variant was adopted instead; this change's half did not hold up on its own", probe.Adopted)
	}
}

func probeGateResult(probe probeOutcome, cases int, plan []plannedChange) *domain.ReflectionGateResult {
	notAdopted := 0
	for _, c := range plan {
		if !probe.AdoptedSet[c.Index] {
			notAdopted++
		}
	}
	r := &domain.ReflectionGateResult{
		Probed: true, Unmeasured: probe.Unmeasured, Adopted: probe.Adopted, Keep: probe.Adopted != "",
		BeforeRate: probe.BaseGolden, AfterRate: probe.AfterGold,
		Cases: probe.Cases, Variants: probe.Variants, NotAdopted: notAdopted,
	}
	switch {
	case probe.Unmeasured && probe.Unservable != nil:
		r.Reason = "the agent's engine could not run the rehearsal: " + probe.Unservable.Error()
	case probe.Unmeasured && cases == 0:
		r.Reason = "nothing to rehearse on: no golden tasks and no recent tasks or chats"
	case probe.Unmeasured:
		r.Reason = "no rehearsal produced an answer from the current setup to compare against"
	case probe.Adopted != "":
		for _, v := range probe.Variants {
			if v.Label == probe.Adopted {
				r.Reason = fmt.Sprintf("adopted %s: %d better, %d worse, %d even against the current setup", v.Label, v.Wins, v.Losses, v.Ties)
			}
		}
	default:
		r.Reason = "no variant did better than the current setup"
	}
	return r
}

type probeSummaryInput struct {
	Cases              int
	Unmeasured         bool
	Adopted            string
	Wins, Losses, Ties int
	NotAdopted         int
}

var probeSummaryKey = prompt.Define("evolution.probe_summary", probeSummaryInput{
	Cases: 3, Adopted: "procedure", Wins: 2, Ties: 1, NotAdopted: 1,
})

func probeSummaryLine(g domain.ReflectionGateResult) string {
	in := probeSummaryInput{Cases: len(g.Cases), Unmeasured: g.Unmeasured, Adopted: g.Adopted, NotAdopted: g.NotAdopted}
	for _, v := range g.Variants {
		if v.Label == g.Adopted {
			in.Wins, in.Losses, in.Ties = v.Wins, v.Losses, v.Ties
		}
	}
	return probeSummaryKey.Render(in)
}

type validationFeedbackInput struct{ Problems []string }

var (
	validationProblemKey = prompt.Define("evolution.validation_problem", planProblem{
		Code: problemMissingSkillRef, Kind: "rule", Action: "create", Name: "migrations-first",
		Owner: `rule "migrations-first"`, Ref: "db-migrations",
	})
	validationFeedbackKey = prompt.Define("evolution.validation_feedback", validationFeedbackInput{
		Problems: []string{`rule create "migrations-first": rule "migrations-first" names the ` + "`db-migrations`" + ` skill, which does not exist after this change set.`},
	})
)

func renderProblem(p planProblem) string {
	return validationProblemKey.Render(p)
}

func validationFeedback(problems map[int]planProblem) string {
	var lines []string
	for _, p := range sortedProblems(problems) {
		lines = append(lines, renderProblem(p))
	}
	return validationFeedbackKey.Render(validationFeedbackInput{Problems: lines})
}

// Mirrors gatherEvidence's own listing (every scope, one agent) but with a high limit since this is a count, not evidence to show.
func (s *Service) countMemories(ctx context.Context, agentID uuid.UUID) int {
	if s.memories == nil {
		return 0
	}
	mems, err := s.memories.List(ctx, domain.MemoryQuery{
		AgentID: agentID, Owner: domain.MemoryOwnerAgent, Repo: domain.MemoryRepoScopeAny, Limit: 1000,
	})
	if err != nil {
		return 0
	}
	return len(mems)
}

func (s *Service) failReflection(ctx context.Context, reflection domain.AgentReflection, rawOutput, errMsg string) {
	reflection.Status = domain.ReflectionStatusFailed
	reflection.RawOutput = rawOutput
	reflection.Error = errMsg
	if err := s.store.CompleteReflection(ctx, reflection); err != nil {
		log.Warn().Err(err).Msg("fail reflection persist failed")
	}
	log.Warn().Str("reflection_id", reflection.ID.String()).Str("error", errMsg).Msg("reflection failed")
}

func (s *Service) captureSnapshot(ctx context.Context, agentID uuid.UUID) *domain.PerformanceSnapshot {
	snap := &domain.PerformanceSnapshot{CapturedAt: time.Now(), KPIs: map[string]float64{}}
	if score, err := s.perf.GetScore(ctx, agentID); err == nil {
		snap.Score = score.Score
	}
	if s.kpis != nil {
		kpis, err := s.kpis.ListKPIs(ctx, agentID)
		if err == nil {
			results, _ := s.kpis.LatestResults(ctx, agentID)
			snap.KPIComposite = kpiComposite(kpis, results)
			byID := map[uuid.UUID]string{}
			for _, k := range kpis {
				byID[k.ID] = k.MetricKey
			}
			for _, r := range results {
				if key, ok := byID[r.KPIID]; ok {
					snap.KPIs[key] = r.Attainment
				}
			}
		}
	}
	return snap
}

// One reflection conversation: the first answer, with one retry when it is not JSON, and later revisions on the same history — so feedback on the proposal reaches the model that made it.
type reflectionCall struct {
	s        *Service
	agentRec domain.Agent
	model    string
	provider domain.LLMProviderType
	messages []domain.Message
	files    map[string]string
	localDir string
}

func (s *Service) newReflectionCall(agentRec domain.Agent, evidence string, files map[string]string) *reflectionCall {
	// Reflection rewrites the agent's own instructions, the hardest work it owns, so it runs on the agent's ModelHeavy like a "hard" orchestrator subtask; evolution.model/provider_type stay the operator override.
	model := agentRec.Model
	if agentRec.ModelHeavy != "" {
		model = agentRec.ModelHeavy
	}
	provider := agentRec.ProviderType
	if s.cfg.Model != "" {
		model = s.cfg.Model
	}
	if s.cfg.ProviderType != "" {
		provider = domain.LLMProviderType(s.cfg.ProviderType)
	}
	return &reflectionCall{
		s: s, agentRec: agentRec, model: model, provider: provider, files: files,
		messages: []domain.Message{
			{Role: domain.RoleSystem, Content: reflectionSystemPromptFor(agentRec, s.cfg, len(files) > 0)},
			{Role: domain.RoleUser, Content: evidence},
		},
	}
}

func (r *reflectionCall) close() {
	if r.localDir == "" {
		return
	}
	if err := os.RemoveAll(r.localDir); err != nil {
		log.Warn().Err(err).Str("dir", r.localDir).Msg("could not remove the reflection evidence directory")
	}
	r.localDir = ""
}

// The in-process loop reads the evidence with read_file, which is scoped to the run's workspace, so it gets a directory on this disk; only that run's context ever carries it.
func (r *reflectionCall) evidenceDir() string {
	if r.localDir != "" {
		return r.localDir
	}
	dir, err := os.MkdirTemp("", "tt-reflect-")
	if err != nil {
		log.Warn().Err(err).Msg("reflection evidence directory could not be created; the report alone goes in")
		return ""
	}
	if err := writeEvidenceDir(dir, r.files); err != nil {
		log.Warn().Err(err).Msg("reflection evidence files could not be written; the report alone goes in")
		_ = os.RemoveAll(dir)
		return ""
	}
	r.localDir = dir
	return dir
}

func (r *reflectionCall) call(ctx context.Context, msgs []domain.Message) (string, error) {
	s := r.s
	if s.reflectionIsAgentic() {
		tools := []string{"web_search", "fetch_url"}
		// s.agentLoop is the router, so an agent on a host-executed provider reflects through that host's CLI, in a scratch directory the router creates and seeds with the evidence files — wherever the session actually runs. A background reflection has no repository of its own.
		opts := []agent.RunOption{
			agent.WithSessionLimits(r.agentRec.MaxTurns, r.agentRec.Effort),
			agent.WithCLILabel("reflect:"+r.agentRec.Name, "agent self-reflection"),
			agent.WithScratchWorkspace(),
		}
		if len(r.files) > 0 {
			if domain.RequiresHostExecutor(r.provider) {
				opts = append(opts, agent.WithScratchFiles(r.files))
			} else if dir := r.evidenceDir(); dir != "" {
				ctx = registry.ContextWithWorkspaceDir(ctx, dir)
				tools = append(tools, "read_file", "get_repo_tree", "grep_code")
			}
		}
		resp, err := s.agentLoop.Run(ctx, msgs, r.model, r.provider, domain.ToolPolicy{AllowTools: tools}, opts...)
		if err != nil {
			return "", err
		}
		return resp.Message.Content, nil
	}
	resp, err := s.llm.Chat(ctx, domain.AgentRequest{
		Messages: msgs, ProviderType: r.provider, Model: r.model,
		ResponseFormat: domain.JSONSchemaResponseFormat(agentReflectionSchemaKey.Name(), agentReflectionSchemaKey.Map()),
	})
	if err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}

func (r *reflectionCall) ask(ctx context.Context, msgs []domain.Message) (domain.ReflectionOutput, string, string, error) {
	raw, err := r.call(ctx, msgs)
	if err != nil {
		return domain.ReflectionOutput{}, "", "", fmt.Errorf("reflection llm call failed: %w", err)
	}
	output, analysis, parseErr := parseReflectionOutput(raw)
	if parseErr == nil {
		r.messages = append(msgs, domain.Message{Role: domain.RoleAssistant, Content: raw})
		return output, raw, analysis, nil
	}
	retryMsgs := append(append([]domain.Message(nil), msgs...),
		domain.Message{Role: domain.RoleAssistant, Content: raw},
		domain.Message{Role: domain.RoleUser, Content: retryNotJSONMessage(parseErr)},
	)
	raw2, err := r.call(ctx, retryMsgs)
	if err != nil {
		return domain.ReflectionOutput{}, raw, "", fmt.Errorf("reflection retry failed: %w", err)
	}
	output, analysis, parseErr = parseReflectionOutput(raw2)
	if parseErr != nil {
		return domain.ReflectionOutput{}, raw2, "", fmt.Errorf("reflection output unparseable after retry: %w", parseErr)
	}
	r.messages = append(retryMsgs, domain.Message{Role: domain.RoleAssistant, Content: raw2})
	return output, raw2, analysis, nil
}

func (r *reflectionCall) run(ctx context.Context) (domain.ReflectionOutput, string, string, error) {
	return r.ask(ctx, append([]domain.Message(nil), r.messages...))
}

func (r *reflectionCall) revise(ctx context.Context, feedback string) (domain.ReflectionOutput, string, string, error) {
	msgs := append(append([]domain.Message(nil), r.messages...), domain.Message{Role: domain.RoleUser, Content: feedback})
	return r.ask(ctx, msgs)
}

func (s *Service) runLLM(ctx context.Context, agentRec domain.Agent, evidence string) (domain.ReflectionOutput, string, string, error) {
	call := s.newReflectionCall(agentRec, evidence, nil)
	defer call.close()
	return call.run(ctx)
}

func (s *Service) applyOutput(
	ctx context.Context,
	agentRec domain.Agent,
	reflection domain.AgentReflection,
	output domain.ReflectionOutput,
	skills []domain.Skill,
	rules []domain.OrchestratorRule,
) ([]string, []domain.AgentEvolutionEvent, []domain.ReflectionChangeOutcome) {
	var applied []string
	var events []domain.AgentEvolutionEvent
	var outcomes []domain.ReflectionChangeOutcome
	scoreAt := 100.0
	if score, err := s.perf.GetScore(ctx, agentRec.ID); err == nil {
		scoreAt = score.Score
	}
	// Everything the reflection writes into the catalog is attributed to it in version history, so a human reading a skill's history can tell a self-evolution rewrite from their own edit.
	ctx = catalog.WithVersionSource(ctx, catalog.VersionSource{
		Source: domain.CatalogVersionSourceEvolution, ReflectionID: &reflection.ID,
	})
	// Returns the persisted event so callers attach its id to the outcome they record for the same change — the two records must point at each other for the golden gate to later mark outcomes rolled_back by matching event ids.
	recordEvent := func(e domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error) {
		e.ReflectionID = &reflection.ID
		e.AgentID = agentRec.ID
		e.ScoreAtChange = scoreAt
		created, err := s.store.CreateEvent(ctx, e)
		if err != nil {
			log.Warn().Err(err).Str("change", e.ChangeType).Msg("evolution event persist failed")
			return domain.AgentEvolutionEvent{}, err
		}
		events = append(events, created)
		return created, nil
	}
	recordOutcome := func(o domain.ReflectionChangeOutcome) {
		outcomes = append(outcomes, o)
	}
	skillByID := map[string]domain.Skill{}
	for _, sk := range skills {
		skillByID[sk.ID.String()] = sk
	}
	ruleByID := map[string]domain.OrchestratorRule{}
	for _, r := range rules {
		ruleByID[r.ID.String()] = r
	}

	if agentRec.SelfEvolutionEnabled {
		applied = append(applied, s.applySkillChanges(ctx, agentRec, output.Skills, skillByID, recordEvent, recordOutcome)...)
		applied = append(applied, s.applyRuleChanges(ctx, agentRec, output.Rules, ruleByID, recordEvent, recordOutcome)...)
		applied = append(applied, s.applyReverts(ctx, agentRec, output.Reverts, recordEvent, recordOutcome)...)
	} else {
		if len(output.Skills) > 0 || len(output.Rules) > 0 || len(output.Reverts) > 0 {
			log.Info().Str("agent", agentRec.Name).Msg("skill/rule changes proposed but self_evolution_enabled=false; skipped")
		}
		for _, ch := range output.Skills {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "skill", Action: ch.Action, Name: ch.Name, TargetID: ch.SkillID, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "self-evolution disabled for this agent",
			})
		}
		for _, ch := range output.Rules {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "rule", Action: ch.Action, Name: ch.Name, TargetID: ch.RuleID, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "self-evolution disabled for this agent",
			})
		}
		for _, rv := range output.Reverts {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "revert", Action: "revert", TargetID: rv.EvolutionEventID, Reason: rv.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "self-evolution disabled for this agent",
			})
		}
	}

	maxMem := s.cfg.MaxMemoryChanges
	for i, mc := range output.Memories {
		if i >= maxMem {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "memory", Action: mc.Action, Name: truncate(mc.Content, 60), TargetID: mc.MemoryID, Reason: mc.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "memory change cap reached",
			})
			continue
		}
		switch mc.Action {
		case "create":
			if s.memories == nil {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "create", Name: truncate(mc.Content, 60), Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "memory service unavailable",
				})
				continue
			}
			// Same bar as save_memory: a note tied to one card is a run log and the card already holds it. Skipped rather than failed — the rest of the reflection is still worth applying.
			if code := domain.MemoryRunLogCode(mc.Content); code != "" {
				reason := prompt.MemoryRunLogReasonText(code)
				log.Info().Str("agent", agentRec.Name).Str("reason", reason).
					Str("content", truncate(mc.Content, 80)).Msg("reflection memory skipped: run log, not a durable lesson")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "create", Name: truncate(mc.Content, 60), Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: reason,
				})
				continue
			}
			// Reflection reasons over runs from every repository at once, so what it saves is global; repo-specific lessons come from save_memory during the run, where the repository is known.
			mem, err := s.memories.Save(ctx, agentRec.ID, nil, mc.Content, mc.Category, domain.MemorySourceReflection)
			if err != nil {
				log.Warn().Err(err).Msg("reflection memory save failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "create", Name: truncate(mc.Content, 60), Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			after, _ := json.Marshal(map[string]string{"content": mc.Content, "category": mc.Category})
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeMemoryCreated, TargetKind: domain.EvolutionTargetMemory,
				TargetID: &mem.ID, TargetName: truncate(mc.Content, 80), After: after,
				Impact: domain.EvolutionImpactNeutral,
			})
			applied = append(applied, "memory saved: "+truncate(mc.Content, 60))
			outcome := domain.ReflectionChangeOutcome{
				Kind: "memory", Action: "create", Name: truncate(mc.Content, 60), TargetID: mem.ID.String(),
				Reason: mc.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "delete":
			if s.memories == nil || mc.MemoryID == "" {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "delete", TargetID: mc.MemoryID, Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "missing memory id",
				})
				continue
			}
			memID, err := uuid.Parse(mc.MemoryID)
			if err != nil {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "delete", TargetID: mc.MemoryID, Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "invalid memory id",
				})
				continue
			}
			if err := s.memories.Delete(ctx, agentRec.ID, memID); err != nil {
				log.Warn().Err(err).Msg("reflection memory delete failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "memory", Action: "delete", TargetID: mc.MemoryID, Reason: mc.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeMemoryDeleted, TargetKind: domain.EvolutionTargetMemory,
				TargetID: &memID, TargetName: mc.MemoryID, Impact: domain.EvolutionImpactNeutral,
			})
			applied = append(applied, "memory deleted: "+mc.MemoryID)
			outcome := domain.ReflectionChangeOutcome{
				Kind: "memory", Action: "delete", TargetID: mc.MemoryID, Reason: mc.Reason,
				Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		}
	}
	return applied, events, outcomes
}

func (s *Service) applySkillChanges(
	ctx context.Context,
	agentRec domain.Agent,
	changes []domain.ReflectionSkillChange,
	skillByID map[string]domain.Skill,
	recordEvent func(domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error),
	recordOutcome func(domain.ReflectionChangeOutcome),
) []string {
	var applied []string
	count := 0
	// Budget is the standing catalog size, tracked across this batch so a reflection cannot slip three creates past a cap it was already at.
	budget := len(skillByID)
	byName := map[string]domain.Skill{}
	for _, sk := range skillByID {
		byName[strings.ToLower(strings.TrimSpace(sk.Name))] = sk
	}
	for _, ch := range changes {
		if count >= s.cfg.MaxSkillChanges {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "skill", Action: ch.Action, Name: ch.Name, TargetID: ch.SkillID, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "max skill changes per reflection reached",
			})
			continue
		}
		// A create that names an existing skill is the model trying to replace it; treat it as the update it meant, instead of adding a second same-named skill whose content contradicts the first.
		if ch.Action == "create" {
			if existing, ok := byName[strings.ToLower(strings.TrimSpace(ch.Name))]; ok {
				ch.Action = "update"
				ch.SkillID = existing.ID.String()
			}
		}
		if ch.Action == "create" && s.cfg.MaxSkillsPerAgent > 0 && budget >= s.cfg.MaxSkillsPerAgent {
			log.Info().Str("agent", agentRec.Name).Str("skill", ch.Name).Int("budget", s.cfg.MaxSkillsPerAgent).
				Msg("skill create rejected: agent is at its skill budget")
			applied = append(applied, "skill create rejected (budget full, merge into an existing skill instead): "+ch.Name)
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "skill", Action: "create", Name: ch.Name, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeRejected, Detail: "skill budget full",
			})
			continue
		}
		switch ch.Action {
		case "create":
			created, err := s.manager.CreateSkillForAgent(ctx, agentRec.ID, domain.CreateSkillRequest{
				Name: ch.Name, Description: ch.Description, Category: ch.Category, Content: ch.Content, Enabled: true,
			})
			if err != nil {
				log.Warn().Err(err).Str("skill", ch.Name).Msg("skill create failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "create", Name: ch.Name, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeSkillCreated, TargetKind: domain.EvolutionTargetSkill,
				TargetID: &created.ID, TargetName: created.Name, After: snapshotSkill(created, ch.SourceURLs),
			})
			applied = append(applied, "skill created: "+created.Name)
			count++
			budget++
			outcome := domain.ReflectionChangeOutcome{
				Kind: "skill", Action: "create", Name: created.Name, TargetID: created.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "update":
			existing, ok := skillByID[ch.SkillID]
			if !ok {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "update", Name: ch.Name, TargetID: ch.SkillID, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown skill id",
				})
				continue
			}
			before := snapshotSkill(existing, nil)
			updated, err := s.manager.UpdateSkillForAgent(ctx, agentRec.ID, existing.ID, domain.UpdateSkillRequest{
				Name: orDefault(ch.Name, existing.Name), Description: orDefault(ch.Description, existing.Description),
				Category: orDefault(ch.Category, existing.Category), Tags: existing.Tags,
				Content: orDefault(ch.Content, existing.Content), Enabled: existing.Enabled,
				TechStackID: existing.TechStackID,
			})
			if err != nil {
				log.Warn().Err(err).Str("skill", existing.Name).Msg("skill update failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "update", Name: existing.Name, TargetID: existing.ID.String(), Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeSkillUpdated, TargetKind: domain.EvolutionTargetSkill,
				TargetID: &updated.ID, TargetName: updated.Name, Before: before, After: snapshotSkill(updated, ch.SourceURLs),
			})
			applied = append(applied, "skill updated: "+updated.Name)
			count++
			outcome := domain.ReflectionChangeOutcome{
				Kind: "skill", Action: "update", Name: updated.Name, TargetID: updated.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "delete":
			existing, ok := skillByID[ch.SkillID]
			if !ok {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "delete", TargetID: ch.SkillID, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown skill id",
				})
				continue
			}
			before := snapshotSkill(existing, nil)
			if err := s.manager.DeleteSkillForAgent(ctx, agentRec.ID, existing.ID); err != nil {
				log.Warn().Err(err).Str("skill", existing.Name).Msg("skill delete failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "skill", Action: "delete", Name: existing.Name, TargetID: existing.ID.String(), Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeSkillDeleted, TargetKind: domain.EvolutionTargetSkill,
				TargetID: &existing.ID, TargetName: existing.Name, Before: before,
			})
			applied = append(applied, "skill deleted: "+existing.Name)
			count++
			budget--
			outcome := domain.ReflectionChangeOutcome{
				Kind: "skill", Action: "delete", Name: existing.Name, TargetID: existing.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		}
	}
	return applied
}

func (s *Service) applyRuleChanges(
	ctx context.Context,
	agentRec domain.Agent,
	changes []domain.ReflectionRuleChange,
	ruleByID map[string]domain.OrchestratorRule,
	recordEvent func(domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error),
	recordOutcome func(domain.ReflectionChangeOutcome),
) []string {
	var applied []string
	count := 0
	budget := len(ruleByID)
	byName := map[string]domain.OrchestratorRule{}
	for _, r := range ruleByID {
		byName[strings.ToLower(strings.TrimSpace(r.Name))] = r
	}
	for _, ch := range changes {
		if count >= s.cfg.MaxRuleChanges {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "rule", Action: ch.Action, Name: ch.Name, TargetID: ch.RuleID, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "max rule changes per reflection reached",
			})
			continue
		}
		if ch.Action == "create" {
			if existing, ok := byName[strings.ToLower(strings.TrimSpace(ch.Name))]; ok {
				ch.Action = "update"
				ch.RuleID = existing.ID.String()
			}
		}
		if ch.Action == "create" && s.cfg.MaxRulesPerAgent > 0 && budget >= s.cfg.MaxRulesPerAgent {
			log.Info().Str("agent", agentRec.Name).Str("rule", ch.Name).Int("budget", s.cfg.MaxRulesPerAgent).
				Msg("rule create rejected: agent is at its rule budget")
			applied = append(applied, "rule create rejected (budget full, tighten an existing rule instead): "+ch.Name)
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "rule", Action: "create", Name: ch.Name, Reason: ch.Reason,
				Outcome: domain.ReflectionOutcomeRejected, Detail: "rule budget full",
			})
			continue
		}
		switch ch.Action {
		case "create":
			created, err := s.manager.CreateRuleForAgent(ctx, agentRec.ID, domain.CreateOrchestratorRuleRequest{
				Name: ch.Name, Content: ch.Content, Priority: ch.Priority, Enabled: true,
			})
			if err != nil {
				log.Warn().Err(err).Str("rule", ch.Name).Msg("rule create failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "create", Name: ch.Name, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeRuleCreated, TargetKind: domain.EvolutionTargetRule,
				TargetID: &created.ID, TargetName: created.Name, After: snapshotRule(created),
			})
			applied = append(applied, "rule created: "+created.Name)
			count++
			budget++
			outcome := domain.ReflectionChangeOutcome{
				Kind: "rule", Action: "create", Name: created.Name, TargetID: created.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "update":
			existing, ok := ruleByID[ch.RuleID]
			if !ok {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "update", Name: ch.Name, TargetID: ch.RuleID, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown rule id",
				})
				continue
			}
			before := snapshotRule(existing)
			priority := ch.Priority
			if priority == 0 {
				priority = existing.Priority
			}
			updated, err := s.manager.UpdateRuleForAgent(ctx, agentRec.ID, existing.ID, domain.UpdateOrchestratorRuleRequest{
				Name: orDefault(ch.Name, existing.Name), Content: orDefault(ch.Content, existing.Content),
				Priority: priority, Enabled: existing.Enabled,
			})
			if err != nil {
				log.Warn().Err(err).Str("rule", existing.Name).Msg("rule update failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "update", Name: existing.Name, TargetID: existing.ID.String(), Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeRuleUpdated, TargetKind: domain.EvolutionTargetRule,
				TargetID: &updated.ID, TargetName: updated.Name, Before: before, After: snapshotRule(updated),
			})
			applied = append(applied, "rule updated: "+updated.Name)
			count++
			outcome := domain.ReflectionChangeOutcome{
				Kind: "rule", Action: "update", Name: updated.Name, TargetID: updated.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		case "delete":
			existing, ok := ruleByID[ch.RuleID]
			if !ok {
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "delete", TargetID: ch.RuleID, Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown rule id",
				})
				continue
			}
			before := snapshotRule(existing)
			if err := s.manager.DeleteRuleForAgent(ctx, agentRec.ID, existing.ID); err != nil {
				log.Warn().Err(err).Str("rule", existing.Name).Msg("rule delete failed")
				recordOutcome(domain.ReflectionChangeOutcome{
					Kind: "rule", Action: "delete", Name: existing.Name, TargetID: existing.ID.String(), Reason: ch.Reason,
					Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
				})
				continue
			}
			ev, evErr := recordEvent(domain.AgentEvolutionEvent{
				ChangeType: domain.EvolutionChangeRuleDeleted, TargetKind: domain.EvolutionTargetRule,
				TargetID: &existing.ID, TargetName: existing.Name, Before: before,
			})
			applied = append(applied, "rule deleted: "+existing.Name)
			count++
			budget--
			outcome := domain.ReflectionChangeOutcome{
				Kind: "rule", Action: "delete", Name: existing.Name, TargetID: existing.ID.String(),
				Reason: ch.Reason, Outcome: domain.ReflectionOutcomeApplied,
			}
			if evErr == nil {
				id := ev.ID
				outcome.EventID = &id
			}
			recordOutcome(outcome)
		}
	}
	return applied
}

func (s *Service) applyReverts(
	ctx context.Context,
	agentRec domain.Agent,
	reverts []domain.ReflectionRevert,
	recordEvent func(domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error),
	recordOutcome func(domain.ReflectionChangeOutcome),
) []string {
	var applied []string
	for _, rv := range reverts {
		eventID, err := uuid.Parse(rv.EvolutionEventID)
		if err != nil {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "revert", Action: "revert", TargetID: rv.EvolutionEventID, Reason: rv.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "invalid event id",
			})
			continue
		}
		original, err := s.store.GetEvent(ctx, eventID)
		if err != nil || original.AgentID != agentRec.ID {
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "revert", Action: "revert", TargetID: rv.EvolutionEventID, Reason: rv.Reason,
				Outcome: domain.ReflectionOutcomeSkipped, Detail: "unknown evolution event",
			})
			continue
		}
		restored, err := s.revertEvent(ctx, agentRec, original)
		if err != nil {
			log.Warn().Err(err).Str("event", eventID.String()).Msg("revert failed")
			recordOutcome(domain.ReflectionChangeOutcome{
				Kind: "revert", Action: "revert", Name: original.TargetName, TargetID: rv.EvolutionEventID, Reason: rv.Reason,
				Outcome: domain.ReflectionOutcomeFailed, Detail: err.Error(),
			})
			continue
		}
		ev, evErr := recordEvent(domain.AgentEvolutionEvent{
			ChangeType: domain.EvolutionChangeRevert, TargetKind: original.TargetKind,
			TargetID: original.TargetID, TargetName: original.TargetName,
			Before: original.After, After: original.Before, RevertedEventID: &original.ID,
		})
		applied = append(applied, "reverted: "+original.ChangeType+" "+original.TargetName+" ("+restored+")")
		outcome := domain.ReflectionChangeOutcome{
			Kind: "revert", Action: "revert", Name: original.TargetName, TargetID: rv.EvolutionEventID,
			Reason: rv.Reason, Outcome: domain.ReflectionOutcomeApplied, Detail: restored,
		}
		if evErr == nil {
			id := ev.ID
			outcome.EventID = &id
		}
		recordOutcome(outcome)
	}
	return applied
}

func (s *Service) revertEvent(ctx context.Context, agentRec domain.Agent, original domain.AgentEvolutionEvent) (string, error) {
	switch original.ChangeType {
	case domain.EvolutionChangeSkillCreated:
		if original.TargetID == nil {
			return "", fmt.Errorf("missing target id")
		}
		return "skill removed", s.manager.DeleteSkillForAgent(ctx, agentRec.ID, *original.TargetID)
	case domain.EvolutionChangeSkillUpdated:
		var snap skillSnapshot
		if err := json.Unmarshal(original.Before, &snap); err != nil {
			return "", fmt.Errorf("bad before snapshot: %w", err)
		}
		_, err := s.manager.UpdateSkillForAgent(ctx, agentRec.ID, snap.ID, domain.UpdateSkillRequest{
			Name: snap.Name, Description: snap.Description, Category: snap.Category,
			Tags: snap.Tags, Content: snap.Content, Enabled: snap.Enabled,
			TechStackID: snap.TechStackID,
		})
		return "skill restored", err
	case domain.EvolutionChangeSkillDeleted:
		var snap skillSnapshot
		if err := json.Unmarshal(original.Before, &snap); err != nil {
			return "", fmt.Errorf("bad before snapshot: %w", err)
		}
		_, err := s.manager.CreateSkillForAgent(ctx, agentRec.ID, domain.CreateSkillRequest{
			Name: snap.Name, Description: snap.Description, Category: snap.Category,
			Tags: snap.Tags, Content: snap.Content, Enabled: snap.Enabled,
			TechStackID: snap.TechStackID,
		})
		return "skill recreated", err
	case domain.EvolutionChangeRuleCreated:
		if original.TargetID == nil {
			return "", fmt.Errorf("missing target id")
		}
		return "rule removed", s.manager.DeleteRuleForAgent(ctx, agentRec.ID, *original.TargetID)
	case domain.EvolutionChangeRuleUpdated:
		var snap ruleSnapshot
		if err := json.Unmarshal(original.Before, &snap); err != nil {
			return "", fmt.Errorf("bad before snapshot: %w", err)
		}
		_, err := s.manager.UpdateRuleForAgent(ctx, agentRec.ID, snap.ID, domain.UpdateOrchestratorRuleRequest{
			Name: snap.Name, Content: snap.Content, Priority: snap.Priority, Enabled: snap.Enabled,
		})
		return "rule restored", err
	case domain.EvolutionChangeRuleDeleted:
		var snap ruleSnapshot
		if err := json.Unmarshal(original.Before, &snap); err != nil {
			return "", fmt.Errorf("bad before snapshot: %w", err)
		}
		_, err := s.manager.CreateRuleForAgent(ctx, agentRec.ID, domain.CreateOrchestratorRuleRequest{
			Name: snap.Name, Content: snap.Content, Priority: snap.Priority, Enabled: snap.Enabled,
		})
		return "rule recreated", err
	default:
		return "", fmt.Errorf("change type %s is not revertible", original.ChangeType)
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
