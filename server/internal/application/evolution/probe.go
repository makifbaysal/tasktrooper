package evolution

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// Harness²-style task-level probing, sized for a product: the plan is split into its procedure and domain halves, every variant (each half alone, and all of it) is rehearsed on the same cases as the current setup, an independent judge compares the answers blind, and only a variant that does better is adopted. Nothing is written until then.

const (
	probeBaseLabel   = "base"
	probeConcurrency = 2
	// Older work is still a fair rehearsal; the window only decides which cases come first.
	probeLookback = 30 * 24 * time.Hour
	// Chat turns shorter than this are usually "yes, go on" — meaningless without the conversation around them.
	probeMinChatChars = 80
)

type probeCase struct {
	GoldenID uuid.UUID
	Kind     string
	Title    string
	Prompt   string
	Request  string
	Feedback string
	Expected []string
}

type probeVariant struct {
	Label   string
	Members []int
}

type caseJudgement struct {
	Kind     string
	Title    string
	Measured bool
	Scores   map[string]int
	Reasons  map[string]string
	Passed   map[string]bool
}

type probeOutcome struct {
	Cases      []domain.ReflectionProbeCase
	Variants   []domain.ReflectionProbeVariant
	Adopted    string
	AdoptedSet map[int]bool
	Verdicts   map[int]string
	Unmeasured bool
	Unservable error
	BaseGolden *float64
	AfterGold  *float64
	Judgements []caseJudgement
}

func probeVariants(plan []plannedChange) []probeVariant {
	byDim := map[string][]int{}
	var all []int
	for _, c := range plan {
		byDim[c.Dimension] = append(byDim[c.Dimension], c.Index)
		all = append(all, c.Index)
	}
	var out []probeVariant
	if len(byDim) > 1 {
		for _, dim := range []string{domain.ReflectionDimensionProcedure, domain.ReflectionDimensionDomain} {
			if members := byDim[dim]; len(members) > 0 {
				out = append(out, probeVariant{Label: dim, Members: members})
			}
		}
	}
	return append(out, probeVariant{Label: domain.ProbeVariantAll, Members: all})
}

func (s *Service) probeCases(ctx context.Context, agentRec domain.Agent, reflection domain.AgentReflection) []probeCase {
	var cases []probeCase
	if s.golden != nil {
		if tasks, err := s.golden.ListByAgent(ctx, agentRec.ID); err == nil {
			for _, t := range tasks {
				if t.Enabled && strings.TrimSpace(t.Prompt) != "" {
					cases = append(cases, probeCase{
						GoldenID: t.ID, Kind: domain.ProbeCaseGolden, Title: t.Name, Prompt: t.Prompt, Request: t.Prompt, Expected: t.Expected,
					})
				}
			}
		}
	}
	replay := s.taskProbeCases(ctx, agentRec, reflection)
	if len(replay) < s.cfg.ProbeCases {
		replay = append(replay, s.chatProbeCases(ctx, agentRec, reflection, s.cfg.ProbeCases-len(replay))...)
	}
	return append(cases, replay...)
}

func (s *Service) taskProbeCases(ctx context.Context, agentRec domain.Agent, reflection domain.AgentReflection) []probeCase {
	if s.runs == nil || s.tasks == nil || s.cfg.ProbeCases <= 0 {
		return nil
	}
	runs, err := s.runs.ListRecent(ctx, 200)
	if err != nil {
		return nil
	}
	revised := map[uuid.UUID]bool{}
	if s.perf != nil {
		if events, err := s.perf.EventsInWindow(ctx, agentRec.ID, reflection.WindowStart, reflection.WindowEnd); err == nil {
			for _, e := range events {
				if e.Delta < 0 && e.TaskID != nil {
					revised[*e.TaskID] = true
				}
			}
		}
	}
	type candidate struct {
		taskID   uuid.UUID
		at       time.Time
		inWindow bool
	}
	seen := map[uuid.UUID]bool{}
	var candidates []candidate
	floor := reflection.WindowEnd.Add(-probeLookback)
	for _, r := range runs {
		if r.AgentID != agentRec.ID || seen[r.TaskID] || r.CreatedAt.Before(floor) || r.CreatedAt.After(reflection.WindowEnd) {
			continue
		}
		seen[r.TaskID] = true
		candidates = append(candidates, candidate{taskID: r.TaskID, at: r.CreatedAt, inWindow: !r.CreatedAt.Before(reflection.WindowStart)})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if revised[a.taskID] != revised[b.taskID] {
			return revised[a.taskID]
		}
		if a.inWindow != b.inWindow {
			return a.inWindow
		}
		return a.at.After(b.at)
	})
	var cases []probeCase
	for _, c := range candidates {
		if len(cases) >= s.cfg.ProbeCases {
			break
		}
		task, err := s.tasks.GetByID(ctx, c.taskID)
		if err != nil || strings.TrimSpace(task.Title+task.Description) == "" {
			continue
		}
		request := probeTaskRequestKey.Render(probeTaskRequestInput{
			Title: task.Title, Description: truncate(task.Description, 4000),
			TechnicalDescription: truncate(task.TechnicalDescription, 3000),
		})
		cases = append(cases, probeCase{
			Kind: domain.ProbeCaseTask, Title: orDefault(task.Key, task.Title) + " " + truncate(task.Title, 80),
			Prompt:   probeRehearsalKey.Render(probeRehearsalInput{Request: request}),
			Request:  request,
			Feedback: s.taskFeedback(ctx, agentRec, task.ID),
		})
	}
	return cases
}

// What people (and other agents) said about the agent's work on this task — the closest thing the probe has to a grader's notes, and only ever shown to the judge.
func (s *Service) taskFeedback(ctx context.Context, agentRec domain.Agent, taskID uuid.UUID) string {
	if s.comments == nil {
		return ""
	}
	comments, err := s.comments.ListByTask(ctx, taskID)
	if err != nil {
		return ""
	}
	var lines []string
	total := 0
	for _, c := range comments {
		if c.AuthorID == agentRec.ID.String() || strings.TrimSpace(c.Content) == "" {
			continue
		}
		line := "- " + truncate(c.Content, 600)
		total += len(line)
		if total > 3000 {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (s *Service) chatProbeCases(ctx context.Context, agentRec domain.Agent, reflection domain.AgentReflection, want int) []probeCase {
	if s.sessions == nil || want <= 0 {
		return nil
	}
	sessions, err := s.sessions.ListByAgent(ctx, agentRec.ID, 10, 0)
	if err != nil {
		return nil
	}
	floor := reflection.WindowEnd.Add(-probeLookback)
	var cases []probeCase
	for _, sess := range sessions {
		if len(cases) >= want {
			break
		}
		if sess.UpdatedAt.Before(floor) {
			continue
		}
		msgs, err := s.sessions.ListMessages(ctx, sess.ID)
		if err != nil {
			continue
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			if m.Role != domain.RoleUser || len(strings.TrimSpace(m.Content)) < probeMinChatChars || m.CreatedAt.After(reflection.WindowEnd) {
				continue
			}
			request := truncate(m.Content, 4000)
			cases = append(cases, probeCase{
				Kind: domain.ProbeCaseChat, Title: truncate(strings.TrimSpace(m.Content), 80),
				Prompt:  probeRehearsalKey.Render(probeRehearsalInput{Request: request}),
				Request: request,
			})
			break
		}
	}
	return cases
}

// The agent as a variant would stand: its own system prompt, the variant's rules, the skill index a real run gets, and the bodies of the skills this plan touches — inlined for every variant alike, since a rehearsal has no load_skill to call.
func (s *Service) rehearsalSystemPrompt(agentRec domain.Agent, vc virtualCatalog) string {
	evalAgent := agentRec
	evalAgent.SelfEvolutionEnabled = false
	var enabled []domain.Skill
	for _, sk := range vc.Skills {
		if sk.Enabled {
			enabled = append(enabled, sk)
		}
	}
	var ruleTexts []string
	for _, r := range vc.Rules {
		if r.Enabled {
			ruleTexts = append(ruleTexts, r.Content)
		}
	}
	system := prompt.BuildSystemPrompt(evalAgent, enabled, nil, ruleTexts, "")
	var bodies []probeSkillBody
	for _, sk := range enabled {
		if vc.Touched[nameKey(sk.Name)] && strings.TrimSpace(sk.Content) != "" {
			bodies = append(bodies, probeSkillBody{Name: sk.Name, Content: sk.Content})
		}
	}
	if len(bodies) > 0 {
		system += "\n\n" + probeSkillBodiesKey.Render(probeSkillBodiesInput{Skills: bodies})
	}
	return system
}

func (s *Service) rehearsalRouting(agentRec domain.Agent) (string, domain.LLMProviderType) {
	model, provider := agentRec.Model, agentRec.ProviderType
	if s.cfg.Model != "" {
		model = s.cfg.Model
	}
	if s.cfg.ProviderType != "" {
		provider = domain.LLMProviderType(s.cfg.ProviderType)
	}
	return model, provider
}

type probeAnswer struct {
	text string
	err  error
}

func (s *Service) runProbe(
	ctx context.Context,
	agentRec domain.Agent,
	reflection domain.AgentReflection,
	skills []domain.Skill,
	rules []domain.OrchestratorRule,
	plan []plannedChange,
	cases []probeCase,
) probeOutcome {
	variants := probeVariants(plan)
	labels := []string{probeBaseLabel}
	systems := map[string]string{
		probeBaseLabel: s.rehearsalSystemPrompt(agentRec, buildVirtualCatalog(skills, rules, plan, func(plannedChange) bool { return false })),
	}
	for _, v := range variants {
		members := indexSet(v.Members)
		labels = append(labels, v.Label)
		systems[v.Label] = s.rehearsalSystemPrompt(agentRec, buildVirtualCatalog(skills, rules, plan, func(c plannedChange) bool { return members[c.Index] }))
	}

	model, provider := s.rehearsalRouting(agentRec)
	answers := make([]map[string]probeAnswer, len(cases))
	for i := range answers {
		answers[i] = map[string]probeAnswer{}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, probeConcurrency)
	for i, c := range cases {
		for _, label := range labels {
			wg.Add(1)
			go func(i int, c probeCase, label string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				text, err := s.ask(ctx, askRequest{
					Model: model, Provider: provider,
					Messages: []domain.Message{
						{Role: domain.RoleSystem, Content: systems[label]},
						{Role: domain.RoleUser, Content: c.Prompt},
					},
					Label: "probe:" + agentRec.Name, Title: "self-evolution rehearsal",
				})
				mu.Lock()
				answers[i][label] = probeAnswer{text: text, err: err}
				mu.Unlock()
			}(i, c, label)
		}
	}
	wg.Wait()

	out := probeOutcome{AdoptedSet: map[int]bool{}, Verdicts: map[int]string{}}
	tally := map[string]*domain.ReflectionProbeVariant{}
	for _, v := range variants {
		tally[v.Label] = &domain.ReflectionProbeVariant{Label: v.Label, Changes: len(v.Members)}
	}
	passed := map[string]int{}
	goldenMeasured := 0
	for i, c := range cases {
		j := s.judgeCase(ctx, agentRec, reflection, i, c, labels, answers[i], &out)
		out.Judgements = append(out.Judgements, j)
		out.Cases = append(out.Cases, domain.ReflectionProbeCase{Kind: c.Kind, Title: c.Title, Measured: j.Measured})
		if !j.Measured {
			continue
		}
		if c.Kind == domain.ProbeCaseGolden {
			goldenMeasured++
			for _, label := range labels {
				if j.Passed[label] {
					passed[label]++
				}
			}
		}
		for _, v := range variants {
			t := tally[v.Label]
			var vs, bs int
			if c.Kind == domain.ProbeCaseGolden {
				vs, bs = boolScore(j.Passed[v.Label]), boolScore(j.Passed[probeBaseLabel])
				if vs < bs {
					t.GoldenLosses++
				}
			} else {
				score, ok := j.Scores[v.Label]
				if !ok {
					score = -1
				}
				vs, bs = score, j.Scores[probeBaseLabel]
			}
			switch {
			case vs > bs:
				t.Wins++
			case vs < bs:
				t.Losses++
			default:
				t.Ties++
			}
		}
	}

	measured := 0
	for _, c := range out.Cases {
		if c.Measured {
			measured++
		}
	}
	for _, v := range variants {
		t := tally[v.Label]
		t.Accepted = measured > 0 && variantAccepted(*t)
		out.Variants = append(out.Variants, *t)
	}
	if measured == 0 {
		out.Unmeasured = true
		return out
	}

	out.Adopted = chooseVariant(out.Variants)
	if out.Adopted != "" {
		for _, v := range variants {
			if v.Label == out.Adopted {
				out.AdoptedSet = indexSet(v.Members)
			}
		}
	}
	if goldenMeasured > 0 {
		base := float64(passed[probeBaseLabel]) / float64(goldenMeasured)
		out.BaseGolden = &base
		after := base
		if out.Adopted != "" {
			after = float64(passed[out.Adopted]) / float64(goldenMeasured)
		}
		out.AfterGold = &after
	}
	s.saveGoldenResults(ctx, agentRec, reflection, cases, out)
	out.Verdicts = changeVerdicts(plan, variants, out.Variants)
	return out
}

func boolScore(b bool) int {
	if b {
		return 1
	}
	return 0
}

func indexSet(idx []int) map[int]bool {
	out := make(map[int]bool, len(idx))
	for _, i := range idx {
		out[i] = true
	}
	return out
}

// Better on balance and never worse on a golden task — the floor the old gate held, kept: a golden task that passed before and fails now is a regression no rehearsal win buys back. A tie is not a reason to change; keeping the current setup is a real answer.
func variantAccepted(v domain.ReflectionProbeVariant) bool {
	return v.GoldenLosses == 0 && v.Wins-v.Losses > 0
}

// Composition from probed evidence only: "all" is adopted when it beats every half that was tried on its own, or ties the best one while every half also held up alone; otherwise the best half that held up; otherwise nothing.
func chooseVariant(variants []domain.ReflectionProbeVariant) string {
	var all *domain.ReflectionProbeVariant
	var best *domain.ReflectionProbeVariant
	everyHalfAccepted := true
	halves := 0
	for i := range variants {
		v := &variants[i]
		if v.Label == domain.ProbeVariantAll {
			all = v
			continue
		}
		halves++
		if !v.Accepted {
			everyHalfAccepted = false
			continue
		}
		if best == nil || v.Wins-v.Losses > best.Wins-best.Losses ||
			(v.Wins-v.Losses == best.Wins-best.Losses && v.Changes < best.Changes) {
			best = v
		}
	}
	if all != nil && all.Accepted {
		net := all.Wins - all.Losses
		if best == nil || net > best.Wins-best.Losses || (net == best.Wins-best.Losses && halves > 0 && everyHalfAccepted) {
			return all.Label
		}
	}
	if best != nil {
		return best.Label
	}
	return ""
}

// A change's verdict is its own half's result when the halves were tried apart, otherwise the whole set's; a change that only ever ran alongside others gets "confounded" instead of credit or blame it may not deserve.
func changeVerdicts(plan []plannedChange, variants []probeVariant, results []domain.ReflectionProbeVariant) map[int]string {
	byLabel := map[string]domain.ReflectionProbeVariant{}
	for _, r := range results {
		byLabel[r.Label] = r
	}
	memberOf := map[int]probeVariant{}
	for _, v := range variants {
		for _, idx := range v.Members {
			if cur, ok := memberOf[idx]; !ok || cur.Label == domain.ProbeVariantAll {
				memberOf[idx] = v
			}
		}
	}
	out := map[int]string{}
	for _, c := range plan {
		v, ok := memberOf[c.Index]
		if !ok {
			continue
		}
		r := byLabel[v.Label]
		verdict := domain.ProbeVerdictNoEffect
		switch {
		case variantAccepted(r):
			verdict = domain.ProbeVerdictHelped
		case r.GoldenLosses > 0 || r.Wins < r.Losses:
			verdict = domain.ProbeVerdictHurt
		}
		if len(v.Members) > 1 && verdict != domain.ProbeVerdictNoEffect {
			verdict = domain.ProbeVerdictConfounded
		}
		out[c.Index] = verdict
	}
	return out
}

// A case is measured only when the current setup answered it: without that there is nothing to compare a variant to. Golden cases are graded by their expected substrings; the rest by a judge that sees the answers under shuffled letters, never which one is the current setup.
func (s *Service) judgeCase(
	ctx context.Context,
	agentRec domain.Agent,
	reflection domain.AgentReflection,
	caseIndex int,
	c probeCase,
	labels []string,
	answers map[string]probeAnswer,
	out *probeOutcome,
) caseJudgement {
	j := caseJudgement{Kind: c.Kind, Title: c.Title, Scores: map[string]int{}, Reasons: map[string]string{}, Passed: map[string]bool{}}
	base := answers[probeBaseLabel]
	if base.err != nil || strings.TrimSpace(base.text) == "" {
		if base.err != nil && errors.Is(base.err, domain.ErrHostExecutedUnservable) && out.Unservable == nil {
			out.Unservable = base.err
		}
		if base.err != nil {
			log.Warn().Err(base.err).Str("agent", agentRec.Name).Str("case", c.Title).Msg("probe: the current setup could not answer; case not measured")
		}
		return j
	}
	if c.Kind == domain.ProbeCaseGolden {
		j.Measured = true
		for _, label := range labels {
			a := answers[label]
			j.Passed[label] = a.err == nil && containsAll(a.text, c.Expected)
		}
		return j
	}

	var entries []probeJudgeResponse
	letterOf := map[string]string{}
	order := make([]string, 0, len(labels))
	for _, label := range labels {
		if a := answers[label]; a.err == nil && strings.TrimSpace(a.text) != "" {
			order = append(order, label)
		}
	}
	shuffleLabels(order, reflection.ID, caseIndex)
	for i, label := range order {
		letter := string(rune('A' + i))
		letterOf[letter] = label
		entries = append(entries, probeJudgeResponse{Label: letter, Text: truncate(answers[label].text, 6000)})
	}
	model, provider := s.judgeRouting(agentRec)
	raw, err := s.ask(ctx, askRequest{
		Model: model, Provider: provider,
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: prompt.Text(probeJudgeSystemKey)},
			{Role: domain.RoleUser, Content: probeJudgeUserKey.Render(probeJudgeUserInput{
				Request: c.Request, Feedback: c.Feedback, Responses: entries,
			})},
		},
		Schema: &probeScoresSchemaKey,
		Label:  "probe-judge:" + agentRec.Name, Title: "self-evolution probe judge",
	})
	if err != nil {
		if errors.Is(err, domain.ErrHostExecutedUnservable) && out.Unservable == nil {
			out.Unservable = err
		}
		log.Warn().Err(err).Str("agent", agentRec.Name).Str("case", c.Title).Msg("probe: judge failed; case not measured")
		return j
	}
	var scores probeScores
	if err := parsePromotionJSON(raw, &scores); err != nil {
		log.Warn().Err(err).Str("agent", agentRec.Name).Str("case", c.Title).Msg("probe: judge verdict unparseable; case not measured")
		return j
	}
	for _, sc := range scores.Scores {
		label, ok := letterOf[strings.ToUpper(strings.TrimSpace(sc.Label))]
		if !ok {
			continue
		}
		j.Scores[label] = sc.Score
		j.Reasons[label] = sc.Reason
	}
	if _, ok := j.Scores[probeBaseLabel]; !ok {
		return j
	}
	j.Measured = true
	return j
}

// The suite's history keeps the before/after pair it always had: the current setup, and what was adopted (the current setup again when nothing was).
func (s *Service) saveGoldenResults(ctx context.Context, agentRec domain.Agent, reflection domain.AgentReflection, cases []probeCase, out probeOutcome) {
	if s.golden == nil {
		return
	}
	after := out.Adopted
	if after == "" {
		after = probeBaseLabel
	}
	reflectionID := reflection.ID
	for i, c := range cases {
		if c.Kind != domain.ProbeCaseGolden || i >= len(out.Judgements) || !out.Judgements[i].Measured {
			continue
		}
		for _, pair := range [][2]string{{goldenPhaseBefore, probeBaseLabel}, {goldenPhaseAfter, after}} {
			ok := out.Judgements[i].Passed[pair[1]]
			detail := pair[0] + ": ok"
			if !ok {
				detail = pair[0] + ": missing expected output"
			}
			if _, err := s.golden.SaveResult(ctx, domain.GoldenResult{
				GoldenID: c.GoldenID, AgentID: agentRec.ID, ReflectionID: &reflectionID, Passed: ok, Detail: detail,
			}); err != nil {
				log.Warn().Err(err).Msg("golden result persist failed")
			}
		}
	}
}

func containsAll(answer string, expected []string) bool {
	lower := strings.ToLower(answer)
	for _, exp := range expected {
		if !strings.Contains(lower, strings.ToLower(exp)) {
			return false
		}
	}
	return true
}

// Seeded by the reflection and the case so a rerun of the same record shuffles the same way, while the base answer is not always "A".
func shuffleLabels(labels []string, reflectionID uuid.UUID, caseIndex int) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s|%d", reflectionID, caseIndex)))
	r := rand.New(rand.NewSource(int64(h.Sum64())))
	r.Shuffle(len(labels), func(i, j int) { labels[i], labels[j] = labels[j], labels[i] })
}

type probeScores struct {
	Scores []probeScore `json:"scores"`
}

type probeScore struct {
	Label  string `json:"label"`
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

type probeJudgeResponse struct {
	Label string
	Text  string
}

type probeJudgeUserInput struct {
	Request   string
	Feedback  string
	Responses []probeJudgeResponse
}

type probeRehearsalInput struct{ Request string }

type probeTaskRequestInput struct{ Title, Description, TechnicalDescription string }

type probeSkillBody struct{ Name, Content string }

type probeSkillBodiesInput struct{ Skills []probeSkillBody }

var (
	probeRehearsalKey   = prompt.Define("evolution.probe_rehearsal", probeRehearsalInput{Request: "Add pagination to the orders list."})
	probeTaskRequestKey = prompt.Define("evolution.probe_task_request", probeTaskRequestInput{
		Title: "Paginate orders", Description: "The orders page loads every order at once.", TechnicalDescription: "Use cursor pagination.",
	})
	probeSkillBodiesKey = prompt.Define("evolution.probe_skill_bodies", probeSkillBodiesInput{
		Skills: []probeSkillBody{{Name: "db-migrations", Content: "Write a down migration for every up migration."}},
	})
	probeJudgeSystemKey = prompt.Define[struct{}]("evolution.probe_judge_system", struct{}{})
	probeJudgeUserKey   = prompt.Define("evolution.probe_judge_user", probeJudgeUserInput{
		Request: "Add pagination to the orders list.", Feedback: "- the page size must be configurable",
		Responses: []probeJudgeResponse{{Label: "A", Text: "Plan A"}, {Label: "B", Text: "Plan B"}},
	})
)
