package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/kpi"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func kpiComposite(kpis []domain.AgentKPI, results []domain.AgentKPIResult) float64 {
	return kpi.CompositeScore(kpis, results)
}

type retryNotJSONInput struct{ ParseError string }

var retryNotJSONKey = prompt.Define("evolution.retry_not_json", retryNotJSONInput{ParseError: "unexpected end of JSON input"})

// retryNotJSONMessage asks for a clean retry after an unparsable JSON
// response; shared by reflect.go's reflection call and promote.go's
// classification calls, both of which hit this failure mode the same way.
// See catalog/system/prompts/evolution/retry_not_json.md.
func retryNotJSONMessage(parseErr error) string {
	return retryNotJSONKey.Render(retryNotJSONInput{ParseError: parseErr.Error()})
}

type reflectionSystemPromptInput struct {
	AgentName            string
	SelfEvolutionEnabled bool
	MaxSkillChanges      int
	MaxRuleChanges       int
	HasBudget            bool
	MaxSkillsPerAgent    int
	MaxRulesPerAgent     int
	GoldenGate           bool
	AllowWebResearch     bool
	MaxMemoryChanges     int
	MaxAlwaysOnChars     int
	EvidenceFiles        bool
}

var reflectionSystemKey = prompt.Define("evolution.reflection_system", reflectionSystemPromptInput{
	AgentName: "sample-agent", SelfEvolutionEnabled: true,
	MaxSkillChanges: 2, MaxRuleChanges: 1, HasBudget: true,
	MaxSkillsPerAgent: 20, MaxRulesPerAgent: 10,
	GoldenGate: true, AllowWebResearch: true, MaxMemoryChanges: 5,
	MaxAlwaysOnChars: 8000, EvidenceFiles: true,
})

// reflectionSystemPrompt frames the reflection call: what the agent may
// change and the JSON shape it must answer in; see
// catalog/system/prompts/evolution/reflection_system.md.
func reflectionSystemPrompt(agentRec domain.Agent, cfg domain.EvolutionConfig) string {
	return reflectionSystemPromptFor(agentRec, cfg, false)
}

func reflectionSystemPromptFor(agentRec domain.Agent, cfg domain.EvolutionConfig, evidenceFiles bool) string {
	return reflectionSystemKey.Render(reflectionSystemPromptInput{
		AgentName:            agentRec.Name,
		SelfEvolutionEnabled: agentRec.SelfEvolutionEnabled,
		MaxSkillChanges:      cfg.MaxSkillChanges,
		MaxRuleChanges:       cfg.MaxRuleChanges,
		HasBudget:            cfg.MaxSkillsPerAgent > 0 || cfg.MaxRulesPerAgent > 0,
		MaxSkillsPerAgent:    cfg.MaxSkillsPerAgent,
		MaxRulesPerAgent:     cfg.MaxRulesPerAgent,
		GoldenGate:           cfg.GoldenGate,
		AllowWebResearch:     cfg.AllowWebResearch,
		MaxMemoryChanges:     cfg.MaxMemoryChanges,
		MaxAlwaysOnChars:     cfg.MaxAlwaysOnChars,
		EvidenceFiles:        evidenceFiles,
	})
}

var fencedBlockRe = regexp.MustCompile("(?s)```[a-zA-Z]*[ \\t]*\\r?\\n?(.*?)```")

// The CLI path (no schema to constrain it) free-writes markdown plus a JSON block; a plain Unmarshal silently zeroes unrecognized keys, no error, so the reflection completed empty and the retry never fired. This locates the block, aliases model-invented key names onto the canonical ones, and returns the surrounding prose as analysis so it is not lost.
func parseReflectionOutput(raw string) (domain.ReflectionOutput, string, error) {
	block, analysis, err := extractReflectionJSON(raw)
	if err != nil {
		return domain.ReflectionOutput{}, "", err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(block, &fields); err != nil {
		return domain.ReflectionOutput{}, "", fmt.Errorf("invalid JSON: %w", err)
	}
	normalizeReflectionFields(fields)
	if !hasAnyReflectionField(fields) {
		return domain.ReflectionOutput{}, "", fmt.Errorf("no reflection fields found (self_assessment/skills/rules/memories/reverts)")
	}

	var out domain.ReflectionOutput
	if raw, ok := fields["self_assessment"]; ok {
		_ = json.Unmarshal(raw, &out.SelfAssessment)
	}
	if raw, ok := fields["skills"]; ok {
		_ = json.Unmarshal(raw, &out.Skills)
	}
	if raw, ok := fields["rules"]; ok {
		_ = json.Unmarshal(raw, &out.Rules)
	}
	if raw, ok := fields["memories"]; ok {
		_ = json.Unmarshal(raw, &out.Memories)
	}
	if raw, ok := fields["reverts"]; ok {
		_ = json.Unmarshal(raw, &out.Reverts)
	}
	return out, analysis, nil
}

// Last fenced block wins: a model that writes example JSON in its analysis before its real answer would otherwise have the example taken as the answer.
func extractReflectionJSON(raw string) (json.RawMessage, string, error) {
	if matches := fencedBlockRe.FindAllStringSubmatchIndex(raw, -1); len(matches) > 0 {
		for i := len(matches) - 1; i >= 0; i-- {
			m := matches[i]
			content := strings.TrimSpace(raw[m[2]:m[3]])
			if looksLikeJSONObject(content) {
				analysis := strings.TrimSpace(raw[:m[0]] + raw[m[1]:])
				return json.RawMessage(content), analysis, nil
			}
		}
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, "", fmt.Errorf("no JSON object found")
	}
	content := raw[start : end+1]
	if !json.Valid([]byte(content)) {
		return nil, "", fmt.Errorf("no valid JSON object found")
	}
	analysis := strings.TrimSpace(raw[:start] + raw[end+1:])
	return json.RawMessage(content), analysis, nil
}

func looksLikeJSONObject(s string) bool {
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return false
	}
	return json.Valid([]byte(s))
}

func normalizeReflectionFields(fields map[string]json.RawMessage) {
	aliasReflectionKey(fields, "self_assessment", "summary", "reasoning", "analysis", "assessment")
	aliasReflectionKey(fields, "skills", "skill_changes")
	aliasReflectionKey(fields, "rules", "rule_changes")
	aliasReflectionKey(fields, "memories", "memory_changes")
	aliasReflectionKey(fields, "reverts", "revert", "reverted")

	if raw, ok := fields["skills"]; ok {
		fields["skills"] = renameArrayItemKeys(raw, map[string]string{"id": "skill_id", "justification": "reason", "rationale": "reason"})
	}
	if raw, ok := fields["rules"]; ok {
		fields["rules"] = renameArrayItemKeys(raw, map[string]string{"id": "rule_id", "justification": "reason", "rationale": "reason"})
	}
	if raw, ok := fields["memories"]; ok {
		fields["memories"] = renameArrayItemKeys(raw, map[string]string{"id": "memory_id", "justification": "reason", "rationale": "reason"})
	}
	if raw, ok := fields["reverts"]; ok {
		fields["reverts"] = renameArrayItemKeys(raw, map[string]string{"event_id": "evolution_event_id", "id": "evolution_event_id", "justification": "reason", "rationale": "reason"})
	}
}

// Only when canonical is absent — a model that gets the real key right must never be second-guessed by a synonym it also emitted.
func aliasReflectionKey(fields map[string]json.RawMessage, canonical string, aliases ...string) {
	if _, ok := fields[canonical]; ok {
		return
	}
	for _, alias := range aliases {
		if raw, ok := fields[alias]; ok {
			fields[canonical] = raw
			return
		}
	}
}

// A malformed item passes through unchanged, surfacing later as a decode into the wrong Go field — no worse than an untouched key.
func renameArrayItemKeys(raw json.RawMessage, renames map[string]string) json.RawMessage {
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw
	}
	for _, item := range items {
		for from, to := range renames {
			v, ok := item[from]
			if !ok {
				continue
			}
			if _, exists := item[to]; !exists {
				item[to] = v
			}
			delete(item, from)
		}
	}
	out, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	return out
}

func hasAnyReflectionField(fields map[string]json.RawMessage) bool {
	for _, key := range []string{"self_assessment", "skills", "rules", "memories", "reverts"} {
		if _, ok := fields[key]; ok {
			return true
		}
	}
	return false
}

type evidenceHeaderInput struct{ From, To, Trigger string }

var evidenceHeaderKey = prompt.Define("evolution.evidence_header", evidenceHeaderInput{
	From: "2026-01-01 00:00", To: "2026-01-08 00:00", Trigger: "scheduled",
})

// evidenceHeader opens the evidence report; see
// catalog/system/prompts/evolution/evidence_header.md.
func evidenceHeader(from, to, trigger string) string {
	return evidenceHeaderKey.Render(evidenceHeaderInput{From: from, To: to, Trigger: trigger})
}

type evidenceBaselineInput struct{ Date, SnapshotJSON, Summary string }

var evidenceBaselineKey = prompt.Define("evolution.evidence_baseline", evidenceBaselineInput{
	Date: "2026-01-01", SnapshotJSON: `{"score":80}`, Summary: "Improved test coverage.",
})

// evidenceBaselineBlock reports the previous reflection's snapshot as the
// comparison baseline; see
// catalog/system/prompts/evolution/evidence_baseline.md. summary is already
// truncated by the caller; empty means the previous reflection had none.
func evidenceBaselineBlock(date, snapshotJSON, summary string) string {
	return evidenceBaselineKey.Render(evidenceBaselineInput{Date: date, SnapshotJSON: snapshotJSON, Summary: summary})
}

type evidenceCurrentPerformanceInput struct {
	Score                   string
	RunsPassed, RunsRevised int
}

var evidenceCurrentPerformanceKey = prompt.Define("evolution.evidence_current_performance", evidenceCurrentPerformanceInput{
	Score: "82.3", RunsPassed: 5, RunsRevised: 2,
})

// evidenceCurrentPerformance reports the agent's live score; see
// catalog/system/prompts/evolution/evidence_current_performance.md.
func evidenceCurrentPerformance(score string, runsPassed, runsRevised int) string {
	return evidenceCurrentPerformanceKey.Render(evidenceCurrentPerformanceInput{Score: score, RunsPassed: runsPassed, RunsRevised: runsRevised})
}

// evidenceKPILine is one enabled KPI's raw attainment data; the template
// renders its row (including the "full X / half Y" / "measured M →
// attainment A%" wording), not this package.
type evidenceKPILine struct {
	Name, MetricKey, Period      string
	TargetFull, TargetHalf       float64
	HasResult                    bool
	MeasuredValue, AttainmentPct float64
}

type evidenceKPIInput struct {
	Lines     []evidenceKPILine
	Composite string
}

var evidenceKPIKey = prompt.Define("evolution.evidence_kpi", evidenceKPIInput{
	Lines: []evidenceKPILine{{
		Name: "PR cycle time", MetricKey: "cycle_time", Period: "weekly",
		TargetFull: 24, TargetHalf: 48, HasResult: true, MeasuredValue: 30, AttainmentPct: 80,
	}},
	Composite: "80.0",
})

// evidenceKPISection reports KPI attainment; see
// catalog/system/prompts/evolution/evidence_kpi.md. composite is
// pre-formatted (e.g. "82.0").
func evidenceKPISection(lines []evidenceKPILine, composite string) string {
	return evidenceKPIKey.Render(evidenceKPIInput{Lines: lines, Composite: composite})
}

var (
	evidenceScoreEventsHeaderKey      = prompt.Define[struct{}]("evolution.evidence_score_events_header", struct{}{})
	evidenceRevisionFeedbackHeaderKey = prompt.Define[struct{}]("evolution.evidence_revision_feedback_header", struct{}{})
	evidenceTaskRunsHeaderKey         = prompt.Define[struct{}]("evolution.evidence_task_runs_header", struct{}{})
	evidenceChatMessagesHeaderKey     = prompt.Define[struct{}]("evolution.evidence_chat_messages_header", struct{}{})
	evidenceMemoriesHeaderKey         = prompt.Define[struct{}]("evolution.evidence_memories_header", struct{}{})
)

// evidenceRegressionLine is one regressed change's raw data; the template
// renders its "performance DROPPED..." row, not this package.
type evidenceRegressionLine struct{ EventID, ChangeType, TargetName, Date string }

type evidenceRegressionsInput struct{ Lines []evidenceRegressionLine }

var evidenceRegressionsKey = prompt.Define("evolution.evidence_regressions", evidenceRegressionsInput{
	Lines: []evidenceRegressionLine{{
		EventID: "11111111-1111-1111-1111-111111111111", ChangeType: "skill_updated",
		TargetName: "sample-skill", Date: "2026-01-15",
	}},
})

// evidenceRegressionsSection reports regressed prior changes; see
// catalog/system/prompts/evolution/evidence_regressions.md. "" when there is
// nothing regressed, so the caller skips the section entirely rather than
// emitting a header over nothing.
func evidenceRegressionsSection(lines []evidenceRegressionLine) string {
	if len(lines) == 0 {
		return ""
	}
	return evidenceRegressionsKey.Render(evidenceRegressionsInput{Lines: lines})
}

type evidenceCountBudgetInput struct{ Count, Budget int }

var (
	evidenceSkillsHeaderKey = prompt.Define("evolution.evidence_skills_header", evidenceCountBudgetInput{Count: 3, Budget: 20})
	evidenceRulesHeaderKey  = prompt.Define("evolution.evidence_rules_header", evidenceCountBudgetInput{Count: 2, Budget: 10})
)

// evidenceLinesBlock renders one of the evidence report's repeated
// "## Header\n- line\n- line\n\n" sections, shared by every list-shaped
// section (score events, revision feedback, task runs, chat messages,
// skills, rules, memories, regressions); see
// catalog/system/prompts/evolution/evidence_lines_block.md. header already
// carries its own trailing newline(s); an empty lines is valid (a header
// with nothing under it).
func evidenceLinesBlock(header string, lines []string) string {
	var b strings.Builder
	b.WriteString(header)
	if len(lines) > 0 {
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

type evidenceBundle struct {
	// Report is what the reflection reads in its prompt, cut to evidence_max_chars; Full is the same report uncut, for the evidence files.
	Report   string
	Full     string
	Skills   []domain.Skill
	Rules    []domain.OrchestratorRule
	Baseline *domain.PerformanceSnapshot
	Runs     []runEvidence
}

// How much of a skill's body the report shows; the whole body is in the evidence files when the reflection can open them.
const skillExcerptChars = 240

// Only window data, excluding previously reviewed material; the previous reflection's snapshot is the comparison baseline.
func (s *Service) gatherEvidence(
	ctx context.Context,
	agentRec domain.Agent,
	reflection domain.AgentReflection,
	withFiles bool,
) (evidenceBundle, error) {
	var b strings.Builder
	from, to := reflection.WindowStart, reflection.WindowEnd

	b.WriteString(evidenceHeader(from.Format("2006-01-02 15:04"), to.Format("2006-01-02 15:04"), reflection.Trigger))

	var baseline *domain.PerformanceSnapshot
	if prev, err := s.store.LatestCompletedReflection(ctx, agentRec.ID); err == nil && prev != nil {
		if prev.PerformanceSnapshot != nil {
			baseline = prev.PerformanceSnapshot
			snapJSON, _ := json.Marshal(prev.PerformanceSnapshot)
			summary := ""
			if prev.Summary != "" {
				summary = truncate(prev.Summary, 600)
			}
			b.WriteString(evidenceBaselineBlock(prev.CompletedAt.Format("2006-01-02"), string(snapJSON), summary))
		}
	}
	b.WriteString(evidenceLessonsSection(s.earlierLessons(ctx, agentRec.ID)))

	score, _ := s.perf.GetScore(ctx, agentRec.ID)
	b.WriteString(evidenceCurrentPerformance(fmt.Sprintf("%.1f", score.Score), score.RunsPassed, score.RunsRevised))

	if s.kpis != nil {
		kpis, err := s.kpis.ListKPIs(ctx, agentRec.ID)
		if err == nil && len(kpis) > 0 {
			results, _ := s.kpis.LatestResults(ctx, agentRec.ID)
			byKPI := map[uuid.UUID]domain.AgentKPIResult{}
			for _, r := range results {
				byKPI[r.KPIID] = r
			}
			var lines []evidenceKPILine
			for _, k := range kpis {
				if !k.Enabled {
					continue
				}
				line := evidenceKPILine{Name: k.Name, MetricKey: k.MetricKey, Period: k.Period, TargetFull: k.TargetFull, TargetHalf: k.TargetHalf}
				if r, ok := byKPI[k.ID]; ok {
					line.HasResult = true
					line.MeasuredValue = r.MeasuredValue
					line.AttainmentPct = r.Attainment * 100
				}
				lines = append(lines, line)
			}
			b.WriteString(evidenceKPISection(lines, fmt.Sprintf("%.1f", kpiComposite(kpis, results))))
		}
	}

	events, _ := s.perf.EventsInWindow(ctx, agentRec.ID, from, to)
	if len(events) > 0 {
		var lines []string
		for _, e := range events {
			lines = append(lines, fmt.Sprintf("- %s %s (%+.0f): %s", e.CreatedAt.Format("01-02 15:04"), e.EventType, e.Delta, e.Reason))
		}
		b.WriteString(evidenceLinesBlock(prompt.Text(evidenceScoreEventsHeaderKey), lines))
	}

	revisionTaskIDs := map[uuid.UUID]bool{}
	for _, e := range events {
		if e.Delta < 0 && e.TaskID != nil {
			revisionTaskIDs[*e.TaskID] = true
		}
	}
	if len(revisionTaskIDs) > 0 && s.comments != nil {
		var lines []string
		n := 0
		for taskID := range revisionTaskIDs {
			if n >= 5 {
				break
			}
			comments, err := s.comments.ListByTask(ctx, taskID)
			if err != nil {
				continue
			}
			for _, c := range comments {
				if c.CreatedAt.Before(from) || c.CreatedAt.After(to) {
					continue
				}
				lines = append(lines, fmt.Sprintf("- [task %s, %s] %s", taskID.String()[:8], c.AuthorType, truncate(c.Content, 400)))
			}
			n++
		}
		b.WriteString(evidenceLinesBlock(prompt.Text(evidenceRevisionFeedbackHeaderKey), lines))
	}

	if runs, err := s.runs.ListRecent(ctx, 200); err == nil {
		var lines []string
		for _, r := range runs {
			if r.AgentID != agentRec.ID || r.CreatedAt.Before(from) || r.CreatedAt.After(to) {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s %s: %s", r.CreatedAt.Format("01-02 15:04"), r.Status, truncate(r.Summary, 200)))
			if len(lines) >= 20 {
				break
			}
		}
		if len(lines) > 0 {
			b.WriteString(evidenceLinesBlock(prompt.Text(evidenceTaskRunsHeaderKey), lines))
		}
	}
	runLimit := runsInDigest
	if withFiles {
		runLimit = runsInFiles
	}
	recentRuns := s.recentRunEvidence(ctx, agentRec, reflection, runLimit)
	b.WriteString(evidenceTrajectoriesSection(recentRuns, withFiles))

	s.appendChatEvidence(ctx, &b, agentRec.ID, reflection)

	skills, _ := s.catalog.ListSkillsByAgent(ctx, agentRec.ID)
	if len(skills) > 0 {
		stacks, _ := s.catalog.ListTechStacksByAgent(ctx, agentRec.ID)
		stackNames := make(map[uuid.UUID]string, len(stacks))
		for _, st := range stacks {
			stackNames[st.ID] = st.Name
		}
		var lines []string
		for _, sk := range skills {
			stack := "general"
			if sk.TechStackID != nil {
				if name, ok := stackNames[*sk.TechStackID]; ok {
					stack = name
				}
			}
			line := fmt.Sprintf("- %s | %s | %s | %v — %s", sk.ID, sk.Name, stack, sk.Enabled, truncate(sk.Description, 120))
			if excerpt := strings.Join(strings.Fields(sk.Content), " "); excerpt != "" {
				line += " :: " + truncate(excerpt, skillExcerptChars)
			}
			lines = append(lines, line)
		}
		header := evidenceSkillsHeaderKey.Render(evidenceCountBudgetInput{Count: len(skills), Budget: s.cfg.MaxSkillsPerAgent})
		b.WriteString(evidenceLinesBlock(header, lines))
	}
	rules, _ := s.catalog.ListRulesByAgent(ctx, agentRec.ID)
	if len(rules) > 0 {
		var lines []string
		for _, r := range rules {
			lines = append(lines, fmt.Sprintf("- %s | %s | %d | %v — %s", r.ID, r.Name, r.Priority, r.Enabled, truncate(r.Content, 120)))
		}
		header := evidenceRulesHeaderKey.Render(evidenceCountBudgetInput{Count: len(rules), Budget: s.cfg.MaxRulesPerAgent})
		b.WriteString(evidenceLinesBlock(header, lines))
	}
	if s.memories != nil {
		// Reads every scope, otherwise it would propose deleting memories it cannot see.
		q := domain.MemoryQuery{AgentID: agentRec.ID, Owner: domain.MemoryOwnerAgent, Repo: domain.MemoryRepoScopeAny, Limit: 20}
		if mems, err := s.memories.List(ctx, q); err == nil && len(mems) > 0 {
			var lines []string
			for _, m := range mems {
				lines = append(lines, fmt.Sprintf("- %s | %s | %s", m.ID, m.Scope, truncate(m.Content, 150)))
			}
			b.WriteString(evidenceLinesBlock(prompt.Text(evidenceMemoriesHeaderKey), lines))
		}
	}

	s.appendRegressionReport(ctx, &b, agentRec.ID)

	full := b.String()
	evidence := full
	if s.cfg.EvidenceMaxChars > 0 && len(evidence) > s.cfg.EvidenceMaxChars {
		evidence = evidence[:s.cfg.EvidenceMaxChars] + "\n…(truncated)"
	}
	return evidenceBundle{
		Report: evidence, Full: full, Skills: skills, Rules: rules, Baseline: baseline, Runs: recentRuns,
	}, nil
}

func (s *Service) appendChatEvidence(ctx context.Context, b *strings.Builder, agentID uuid.UUID, reflection domain.AgentReflection) {
	sessions, err := s.sessions.ListByAgent(ctx, agentID, 10, 0)
	if err != nil || len(sessions) == 0 {
		return
	}
	from, to := reflection.WindowStart, reflection.WindowEnd
	var lines []string
	total := 0
	for _, sess := range sessions {
		if sess.UpdatedAt.Before(from) {
			continue
		}
		msgs, err := s.sessions.ListMessages(ctx, sess.ID)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if m.CreatedAt.Before(from) || m.CreatedAt.After(to) || m.Role == domain.RoleSystem {
				continue
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s: %s", sess.ID.String()[:8], m.Role, truncate(m.Content, 300)))
			total++
			if total >= 60 {
				break
			}
		}
		if total >= 60 {
			break
		}
	}
	if len(lines) > 0 {
		b.WriteString(evidenceLinesBlock(prompt.Text(evidenceChatMessagesHeaderKey), lines))
	}
}

func (s *Service) appendRegressionReport(ctx context.Context, b *strings.Builder, agentID uuid.UUID) {
	allEvents, err := s.store.ListEvents(ctx, agentID, 100)
	if err != nil {
		return
	}
	reverted := map[uuid.UUID]bool{}
	for _, e := range allEvents {
		if e.RevertedEventID != nil {
			reverted[*e.RevertedEventID] = true
		}
	}
	var lines []evidenceRegressionLine
	for _, e := range allEvents {
		if e.Impact != domain.EvolutionImpactRegressed || reverted[e.ID] || e.ChangeType == domain.EvolutionChangeRevert {
			continue
		}
		lines = append(lines, evidenceRegressionLine{
			EventID: e.ID.String(), ChangeType: e.ChangeType, TargetName: e.TargetName,
			Date: e.CreatedAt.Format("2006-01-02"),
		})
	}
	b.WriteString(evidenceRegressionsSection(lines))
}
