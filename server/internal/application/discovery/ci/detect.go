// Package ci maps GitHub Actions jobs onto the components they verify.
package ci

import (
	"sort"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Result struct {
	Checks   []domain.DetectedCheck
	Warnings []string
}

func Detect(tree *inventory.Tree, components []domain.DetectedComponent) Result {
	sorted := make([]domain.DetectedComponent, len(components))
	copy(sorted, components)
	sort.Slice(sorted, func(i, k int) bool { return sorted[i].Path < sorted[k].Path })

	workflows, warnings := parseWorkflows(tree)

	var checks []domain.DetectedCheck
	for _, wf := range workflows {
		plans := make(map[string]jobPlan, len(wf.Jobs))
		for _, j := range wf.Jobs {
			plans[j.Key] = planJob(wf, j, sorted)
		}
		for _, j := range wf.Jobs {
			plan := plans[j.Key]
			if plan.deferred {
				c, ok := componentFromNeighbours(wf, j, plans)
				if !ok {
					continue
				}
				plan.matches = []componentMatch{{Component: c, Confidence: domain.ConfidenceMedium}}
			}
			checks = append(checks, buildChecks(wf, j, plan)...)
		}
	}

	sort.Slice(checks, func(i, k int) bool {
		a, b := checks[i], checks[k]
		if a.ComponentPath != b.ComponentPath {
			return a.ComponentPath < b.ComponentPath
		}
		if a.Workflow != b.Workflow {
			return a.Workflow < b.Workflow
		}
		return a.JobKey < b.JobKey
	})

	return Result{Checks: checks, Warnings: warnings}
}

type jobPlan struct {
	purpose  domain.CheckPurpose
	jc       jobCommands
	matches  []componentMatch
	deferred bool
}

func verificationPurpose(p domain.CheckPurpose) bool {
	switch p {
	case domain.CheckLint, domain.CheckTypecheck, domain.CheckTest, domain.CheckBuild, domain.CheckE2E:
		return true
	}
	return false
}

// planJob decides which components own a job. A verification job may fan out
// to every component it touches; a release/deploy job builds from several
// directories but belongs to one, so it is pinned to its default working
// directory, else its first match, else deferred to its needs-graph
// neighbours.
func planJob(wf workflow, j job, components []domain.DetectedComponent) jobPlan {
	plan := jobPlan{purpose: classifyPurpose(j), jc: extractJobCommands(j, wf.WorkingDir)}
	plan.matches = mapJob(wf, j, components, plan.jc)
	if len(plan.matches) == 0 || verificationPurpose(plan.purpose) {
		return plan
	}
	for _, dir := range []string{j.WorkingDir, wf.WorkingDir} {
		if dir == "" {
			continue
		}
		if c, ok := deepestForDir(components, cleanPath(dir)); ok {
			plan.matches = []componentMatch{{Component: c, Confidence: domain.ConfidenceHigh}}
			return plan
		}
	}
	if plan.matches[0].FanOut {
		plan.matches = nil
		plan.deferred = true
		return plan
	}
	plan.matches = plan.matches[:1]
	return plan
}

func componentFromNeighbours(wf workflow, j job, plans map[string]jobPlan) (domain.DetectedComponent, bool) {
	found := map[string]domain.DetectedComponent{}
	consider := func(key string) {
		p, ok := plans[key]
		if !ok || p.deferred || len(p.matches) != 1 {
			return
		}
		found[p.matches[0].Component.Path] = p.matches[0].Component
	}
	for _, need := range j.Needs {
		consider(need)
	}
	for _, other := range wf.Jobs {
		for _, need := range other.Needs {
			if need == j.Key {
				consider(other.Key)
			}
		}
	}
	if len(found) != 1 {
		return domain.DetectedComponent{}, false
	}
	for _, c := range found {
		return c, true
	}
	return domain.DetectedComponent{}, false
}

func buildChecks(wf workflow, j job, plan jobPlan) []domain.DetectedCheck {
	matches := plan.matches
	if len(matches) == 0 {
		return nil
	}

	reusableNoSteps := j.Uses != "" && len(j.Steps) == 0
	if reusableNoSteps {
		for i := range matches {
			matches[i].Confidence = domain.ConfidenceLow
		}
	}

	purpose := plan.purpose
	runnable := purpose != domain.CheckDeploy && purpose != domain.CheckRelease

	var commandsByComponent map[string][]domain.LocalCommand
	if runnable {
		commandsByComponent = assignCommands(matches, plan.jc.Commands)
	}

	var steps []domain.CheckStep
	for _, s := range j.Steps {
		steps = append(steps, domain.CheckStep{
			Name:             s.Name,
			Run:              s.Run,
			Uses:             s.Uses,
			WorkingDirectory: s.WorkingDir,
		})
	}

	var environment domain.DeployEnvironment
	if purpose == domain.CheckDeploy {
		environment = deployEnvironment(wf, j)
	}

	checks := make([]domain.DetectedCheck, 0, len(matches))
	for _, m := range matches {
		check := domain.DetectedCheck{
			ComponentPath: m.Component.Path,
			Workflow:      wf.File,
			WorkflowName:  wf.Name,
			JobKey:        j.Key,
			JobName:       j.Name,
			Purpose:       purpose,
			Environment:   environment,
			Triggers:      wf.Triggers,
			PathFilters:   wf.PathFilters,
			Steps:         steps,
			Dispatchable:  wf.Dispatchable,
			Confidence:    m.Confidence,
		}
		if runnable {
			check.LocalCommands = commandsByComponent[m.Component.Path]
		}
		checks = append(checks, check)
	}
	return checks
}
