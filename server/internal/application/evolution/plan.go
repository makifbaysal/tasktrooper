package evolution

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// One skill or rule change that survived the per-reflection caps, the standing budgets and id lookup — what would be written if it is adopted. Index is its position among the plan's changes, the handle probes and verdicts use.
type plannedChange struct {
	Index     int
	Kind      string
	Action    string
	Dimension string
	Skill     domain.ReflectionSkillChange
	Rule      domain.ReflectionRuleChange
	Name      string
	TargetID  string
	Reason    string
}

func (c plannedChange) outcome(result, detail string) domain.ReflectionChangeOutcome {
	return domain.ReflectionChangeOutcome{
		Kind: c.Kind, Action: c.Action, Name: c.Name, TargetID: c.TargetID, Reason: c.Reason,
		Dimension: c.Dimension, Outcome: result, Detail: detail,
	}
}

func normalizeDimension(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case domain.ReflectionDimensionDomain, "dom", "knowledge":
		return domain.ReflectionDimensionDomain
	default:
		return domain.ReflectionDimensionProcedure
	}
}

// Mirrors applySkillChanges/applyRuleChanges decision for decision but writes nothing, so the probe rehearses exactly what would be applied. The apply path keeps its own checks: it is the last word on what reaches the catalog.
func (s *Service) planCatalogChanges(
	output domain.ReflectionOutput,
	skills []domain.Skill,
	rules []domain.OrchestratorRule,
) ([]plannedChange, []domain.ReflectionChangeOutcome) {
	var planned []plannedChange
	var outcomes []domain.ReflectionChangeOutcome
	add := func(c plannedChange) {
		c.Index = len(planned)
		planned = append(planned, c)
	}

	skillByID := map[string]domain.Skill{}
	skillByName := map[string]domain.Skill{}
	for _, sk := range skills {
		skillByID[sk.ID.String()] = sk
		skillByName[nameKey(sk.Name)] = sk
	}
	count, budget := 0, len(skills)
	for _, ch := range output.Skills {
		dim := normalizeDimension(ch.Dimension)
		skip := func(action, detail string, result string) {
			outcomes = append(outcomes, domain.ReflectionChangeOutcome{
				Kind: "skill", Action: action, Name: ch.Name, TargetID: ch.SkillID, Reason: ch.Reason,
				Dimension: dim, Outcome: result, Detail: detail,
			})
		}
		if count >= s.cfg.MaxSkillChanges {
			skip(ch.Action, "max skill changes per reflection reached", domain.ReflectionOutcomeSkipped)
			continue
		}
		if ch.Action == "create" {
			if existing, ok := skillByName[nameKey(ch.Name)]; ok {
				ch.Action = "update"
				ch.SkillID = existing.ID.String()
			}
		}
		switch ch.Action {
		case "create":
			if s.cfg.MaxSkillsPerAgent > 0 && budget >= s.cfg.MaxSkillsPerAgent {
				skip("create", "skill budget full", domain.ReflectionOutcomeRejected)
				continue
			}
			budget++
		case "update":
			existing, ok := skillByID[ch.SkillID]
			if !ok {
				skip("update", "unknown skill id", domain.ReflectionOutcomeSkipped)
				continue
			}
			ch.Name = orDefault(ch.Name, existing.Name)
		case "delete":
			existing, ok := skillByID[ch.SkillID]
			if !ok {
				skip("delete", "unknown skill id", domain.ReflectionOutcomeSkipped)
				continue
			}
			ch.Name = existing.Name
			budget--
		default:
			skip(ch.Action, "unknown action", domain.ReflectionOutcomeSkipped)
			continue
		}
		count++
		add(plannedChange{
			Kind: "skill", Action: ch.Action, Dimension: dim, Skill: ch,
			Name: ch.Name, TargetID: ch.SkillID, Reason: ch.Reason,
		})
	}

	ruleByID := map[string]domain.OrchestratorRule{}
	ruleByName := map[string]domain.OrchestratorRule{}
	for _, r := range rules {
		ruleByID[r.ID.String()] = r
		ruleByName[nameKey(r.Name)] = r
	}
	count, budget = 0, len(rules)
	for _, ch := range output.Rules {
		dim := normalizeDimension(ch.Dimension)
		skip := func(action, detail string, result string) {
			outcomes = append(outcomes, domain.ReflectionChangeOutcome{
				Kind: "rule", Action: action, Name: ch.Name, TargetID: ch.RuleID, Reason: ch.Reason,
				Dimension: dim, Outcome: result, Detail: detail,
			})
		}
		if count >= s.cfg.MaxRuleChanges {
			skip(ch.Action, "max rule changes per reflection reached", domain.ReflectionOutcomeSkipped)
			continue
		}
		if ch.Action == "create" {
			if existing, ok := ruleByName[nameKey(ch.Name)]; ok {
				ch.Action = "update"
				ch.RuleID = existing.ID.String()
			}
		}
		switch ch.Action {
		case "create":
			if s.cfg.MaxRulesPerAgent > 0 && budget >= s.cfg.MaxRulesPerAgent {
				skip("create", "rule budget full", domain.ReflectionOutcomeRejected)
				continue
			}
			budget++
		case "update":
			existing, ok := ruleByID[ch.RuleID]
			if !ok {
				skip("update", "unknown rule id", domain.ReflectionOutcomeSkipped)
				continue
			}
			ch.Name = orDefault(ch.Name, existing.Name)
		case "delete":
			existing, ok := ruleByID[ch.RuleID]
			if !ok {
				skip("delete", "unknown rule id", domain.ReflectionOutcomeSkipped)
				continue
			}
			ch.Name = existing.Name
			budget--
		default:
			skip(ch.Action, "unknown action", domain.ReflectionOutcomeSkipped)
			continue
		}
		count++
		add(plannedChange{
			Kind: "rule", Action: ch.Action, Dimension: dim, Rule: ch,
			Name: ch.Name, TargetID: ch.RuleID, Reason: ch.Reason,
		})
	}
	return planned, outcomes
}

func nameKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// The agent's skills and rules as they would stand after a subset of the plan — what a probe rehearses on, built in memory so nothing reaches the catalog before the probe has spoken.
type virtualCatalog struct {
	Skills []domain.Skill
	Rules  []domain.OrchestratorRule
	// Touched names every skill any change in the plan creates, rewrites or removes, so each variant inlines the same set of bodies and none gets an unfair advantage from showing more of itself.
	Touched map[string]bool
}

func buildVirtualCatalog(skills []domain.Skill, rules []domain.OrchestratorRule, plan []plannedChange, include func(plannedChange) bool) virtualCatalog {
	vc := virtualCatalog{Touched: map[string]bool{}}
	vc.Skills = append([]domain.Skill(nil), skills...)
	vc.Rules = append([]domain.OrchestratorRule(nil), rules...)
	for _, c := range plan {
		if c.Kind == "skill" {
			vc.Touched[nameKey(c.Name)] = true
			if c.Action == "update" {
				for _, sk := range skills {
					if sk.ID.String() == c.TargetID {
						vc.Touched[nameKey(sk.Name)] = true
					}
				}
			}
		}
		if !include(c) {
			continue
		}
		switch c.Kind {
		case "skill":
			vc.Skills = applyVirtualSkill(vc.Skills, c)
		case "rule":
			vc.Rules = applyVirtualRule(vc.Rules, c)
		}
	}
	return vc
}

func applyVirtualSkill(skills []domain.Skill, c plannedChange) []domain.Skill {
	ch := c.Skill
	switch c.Action {
	case "create":
		return append(skills, domain.Skill{
			ID: uuid.New(), Name: ch.Name, Description: ch.Description, Category: ch.Category,
			Content: ch.Content, Enabled: true,
		})
	case "update":
		out := make([]domain.Skill, 0, len(skills))
		for _, sk := range skills {
			if sk.ID.String() == c.TargetID {
				sk.Name = orDefault(ch.Name, sk.Name)
				sk.Description = orDefault(ch.Description, sk.Description)
				sk.Category = orDefault(ch.Category, sk.Category)
				sk.Content = orDefault(ch.Content, sk.Content)
			}
			out = append(out, sk)
		}
		return out
	case "delete":
		out := make([]domain.Skill, 0, len(skills))
		for _, sk := range skills {
			if sk.ID.String() != c.TargetID {
				out = append(out, sk)
			}
		}
		return out
	}
	return skills
}

func applyVirtualRule(rules []domain.OrchestratorRule, c plannedChange) []domain.OrchestratorRule {
	ch := c.Rule
	switch c.Action {
	case "create":
		return append(rules, domain.OrchestratorRule{
			ID: uuid.New(), Name: ch.Name, Content: ch.Content, Priority: ch.Priority, Enabled: true,
		})
	case "update":
		out := make([]domain.OrchestratorRule, 0, len(rules))
		for _, r := range rules {
			if r.ID.String() == c.TargetID {
				r.Name = orDefault(ch.Name, r.Name)
				r.Content = orDefault(ch.Content, r.Content)
				if ch.Priority != 0 {
					r.Priority = ch.Priority
				}
			}
			out = append(out, r)
		}
		return out
	case "delete":
		out := make([]domain.OrchestratorRule, 0, len(rules))
		for _, r := range rules {
			if r.ID.String() != c.TargetID {
				out = append(out, r)
			}
		}
		return out
	}
	return rules
}

func enabledRuleChars(rules []domain.OrchestratorRule) int {
	n := 0
	for _, r := range rules {
		if r.Enabled {
			n += len(r.Content)
		}
	}
	return n
}

// A backticked name right before or after the word "skill": the way rules and skills point at another skill ("load the `db-migrations` skill"). Anything looser would flag ordinary code spans.
var skillRefRe = regexp.MustCompile("(?i)`([^`\\n]{2,80})`\\s+skill\\b|\\bskill\\s+`([^`\\n]{2,80})`")

func skillReferences(text string) []string {
	var out []string
	for _, m := range skillRefRe.FindAllStringSubmatch(text, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

// One reason a planned change cannot be adopted as written. Data only: the wording lives in catalog/system/prompts/evolution/validation_problem.md, because the reflection reads it back as retry feedback.
type planProblem struct {
	Code   string
	Kind   string
	Action string
	Name   string
	Owner  string
	Ref    string
	Total  int
	Limit  int
}

const (
	problemSkillNoName        = "skill_no_name"
	problemSkillNoDescription = "skill_no_description"
	problemSkillNoContent     = "skill_no_content"
	problemRuleNoContent      = "rule_no_content"
	problemMissingSkillRef    = "missing_skill_ref"
	problemDeleteBreaksRef    = "delete_breaks_ref"
	problemAlwaysOnOverLimit  = "always_on_over_limit"
)

// The checks a proposal must pass before any rehearsal is paid for, keyed by plan index. They are the ways a change set fails at run time without anyone noticing: a skill no run will ever choose to load, a pointer to a skill that is gone, and always-on text that grows until it crowds out the work.
func (s *Service) validatePlan(skills []domain.Skill, rules []domain.OrchestratorRule, plan []plannedChange) map[int]planProblem {
	problems := map[int]planProblem{}
	flag := func(c plannedChange, p planProblem) {
		if _, ok := problems[c.Index]; ok {
			return
		}
		p.Kind, p.Action, p.Name = c.Kind, c.Action, c.Name
		problems[c.Index] = p
	}
	for _, c := range plan {
		if c.Kind == "skill" && c.Action != "delete" {
			if strings.TrimSpace(c.Name) == "" {
				flag(c, planProblem{Code: problemSkillNoName})
			}
			desc := c.Skill.Description
			if c.Action == "update" {
				for _, sk := range skills {
					if sk.ID.String() == c.TargetID {
						desc = orDefault(desc, sk.Description)
					}
				}
			}
			if len(strings.Fields(desc)) < 4 {
				flag(c, planProblem{Code: problemSkillNoDescription})
			}
			if c.Action == "create" && strings.TrimSpace(c.Skill.Content) == "" {
				flag(c, planProblem{Code: problemSkillNoContent})
			}
		}
		if c.Kind == "rule" && c.Action != "delete" && strings.TrimSpace(orDefault(c.Rule.Content, ruleContent(rules, c.TargetID))) == "" {
			flag(c, planProblem{Code: problemRuleNoContent})
		}
	}

	all := buildVirtualCatalog(skills, rules, plan, func(plannedChange) bool { return true })
	have := map[string]bool{}
	for _, sk := range all.Skills {
		have[nameKey(sk.Name)] = true
	}
	changedRule := map[string]plannedChange{}
	changedSkill := map[string]plannedChange{}
	var deletedSkills []plannedChange
	for _, c := range plan {
		switch {
		case c.Kind == "rule" && c.Action != "delete":
			changedRule[nameKey(c.Name)] = c
		case c.Kind == "skill" && c.Action != "delete":
			changedSkill[nameKey(c.Name)] = c
		case c.Kind == "skill" && c.Action == "delete":
			deletedSkills = append(deletedSkills, c)
		}
	}
	checkRefs := func(owner, text string, c plannedChange, changed bool) {
		for _, ref := range skillReferences(text) {
			if have[nameKey(ref)] {
				continue
			}
			if changed {
				flag(c, planProblem{Code: problemMissingSkillRef, Owner: owner, Ref: ref})
				continue
			}
			for _, d := range deletedSkills {
				if nameKey(d.Name) == nameKey(ref) {
					flag(d, planProblem{Code: problemDeleteBreaksRef, Owner: owner, Ref: ref})
				}
			}
		}
	}
	for _, r := range all.Rules {
		if !r.Enabled {
			continue
		}
		c, changed := changedRule[nameKey(r.Name)]
		checkRefs("rule "+strconvQuote(r.Name), r.Content, c, changed)
	}
	for _, sk := range all.Skills {
		c, changed := changedSkill[nameKey(sk.Name)]
		checkRefs("skill "+strconvQuote(sk.Name), sk.Content, c, changed)
	}

	if limit := s.cfg.MaxAlwaysOnChars; limit > 0 {
		before, after := enabledRuleChars(rules), enabledRuleChars(all.Rules)
		if after > limit && after > before {
			for _, c := range plan {
				if c.Kind != "rule" || c.Action == "delete" {
					continue
				}
				if len(c.Rule.Content) > len(ruleContent(rules, c.TargetID)) {
					flag(c, planProblem{Code: problemAlwaysOnOverLimit, Total: after, Limit: limit})
				}
			}
		}
	}
	return problems
}

func strconvQuote(s string) string { return fmt.Sprintf("%q", s) }

func ruleContent(rules []domain.OrchestratorRule, id string) string {
	for _, r := range rules {
		if r.ID.String() == id {
			return r.Content
		}
	}
	return ""
}

func sortedProblems(problems map[int]planProblem) []planProblem {
	idx := make([]int, 0, len(problems))
	for i := range problems {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	out := make([]planProblem, 0, len(idx))
	for _, i := range idx {
		out = append(out, problems[i])
	}
	return out
}
