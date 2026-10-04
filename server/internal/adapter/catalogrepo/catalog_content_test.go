package catalogrepo

import (
	"context"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Repo-root catalog read through the production reader, mirroring
// application/catalog's repoCatalogAgents fixture, so these expectations
// cannot drift from what actually ships.
const monorepoCatalog = "../../../../catalog"

func repoCatalogAgents(t *testing.T) map[string]domain.UpstreamAgent {
	t.Helper()
	agents, _, err := (&Reader{Source: monorepoCatalog}).ReadCatalog(context.Background())
	if err != nil {
		t.Skipf("monorepo catalog unavailable (%v); regenerate it and rerun", err)
	}
	bySlug := make(map[string]domain.UpstreamAgent, len(agents))
	for _, a := range agents {
		bySlug[a.Slug] = a
	}
	return bySlug
}

func columnInstruction(t *testing.T, agents map[string]domain.UpstreamAgent, slug string, column domain.TaskColumn) string {
	t.Helper()
	agent, ok := agents[slug]
	if !ok {
		t.Fatalf("catalog is missing agent %q", slug)
	}
	for _, ci := range agent.ColumnInstructions {
		if ci.Column == column {
			return ci.Instruction
		}
	}
	t.Fatalf("agent %q has no column instruction for %q", slug, column)
	return ""
}

// The standing acceptance criteria (build/suite/unit-test rule) that used to
// live in board.standingCriteriaMessage now live in each developer's own
// todo/in_progress/need_revision column md.
func TestDeveloperColumnsCarryTheStandingAcceptanceCriteria(t *testing.T) {
	agents := repoCatalogAgents(t)
	for _, slug := range []string{"backend-developer", "frontend-developer", "mobile-developer"} {
		for _, col := range []domain.TaskColumn{
			domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
		} {
			got := columnInstruction(t, agents, slug, col)
			for _, want := range []string{
				"Standing acceptance criteria",
				"The project builds.",
				"The whole test suite passes",
				"New or changed behaviour comes with a unit test",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("%s/%s: missing %q:\n%s", slug, col, want, got)
				}
			}
		}
	}
}

// The need_revision column additionally has to tell the developer to read
// both the task comments and the PR review comments before touching code —
// this used to be part of board.columnInstruction's need_revision case.
func TestDeveloperNeedRevisionReadsBothCommentSources(t *testing.T) {
	agents := repoCatalogAgents(t)
	for _, slug := range []string{"backend-developer", "frontend-developer", "mobile-developer"} {
		got := columnInstruction(t, agents, slug, domain.TaskColumnNeedRevision)
		for _, want := range []string{"list_task_comments", "get_task_pull_request"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s/need_revision: missing %q:\n%s", slug, want, got)
			}
		}
	}
}

// board.analizNoPushMessage and the analiz workflow prompt migration 143
// seeded into workflow_stages.instructions (and migration 166 cleared) now
// live entirely in system-architect's analiz column md files.
func TestSystemArchitectCatalogCarriesTheAnalizWorkflow(t *testing.T) {
	agents := repoCatalogAgents(t)
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		got := columnInstruction(t, agents, "system-architect", col)
		for _, want := range []string{
			"never push it",
			"no `git push`",
			"add_task_document",
			"analiz_review",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("system-architect/%s: missing %q:\n%s", col, want, got)
			}
		}
	}

	done := columnInstruction(t, agents, "system-architect", domain.TaskColumnDone)
	for _, want := range []string{"decompose", "list_team", "released"} {
		if !strings.Contains(done, want) {
			t.Errorf("system-architect/done: missing %q:\n%s", want, done)
		}
	}
}

// The code_review reviewer rules (never fix, never run the app/builds/tests,
// a missing test is Important not a nit) now live in the architect's own
// code_review.md rather than board.columnInstruction's switch case.
func TestSystemArchitectCodeReviewCarriesTheReviewerRules(t *testing.T) {
	agents := repoCatalogAgents(t)
	got := columnInstruction(t, agents, "system-architect", domain.TaskColumnCodeReview)
	for _, want := range []string{
		"never edit the code you are reviewing",
		"never run a build or a test suite",
		"Important, not a nit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system-architect/code_review: missing %q:\n%s", want, got)
		}
	}
}

// The QA execution rules (approve only what was executed and observed) that
// used to be board.qaExecutionInstruction/humanRequirementsQA now live in
// qa-agent's own ready_for_qa.md and in_qa.md.
func TestQACatalogCarriesTheExecutionRule(t *testing.T) {
	agents := repoCatalogAgents(t)
	for _, col := range []domain.TaskColumn{domain.TaskColumnReadyForQA, domain.TaskColumnInQA} {
		got := columnInstruction(t, agents, "qa-agent", col)
		for _, want := range []string{"review_criterion", "approve only what you executed and observed"} {
			if !strings.Contains(got, want) {
				t.Errorf("qa-agent/%s: missing %q:\n%s", col, want, got)
			}
		}
	}
}

// "Move it to in_qa yourself when the automatic move was refused" used to be
// part of board.columnInstruction's ready_for_qa case (WP10); it now lives
// in qa-agent's own ready_for_qa.md.
func TestQACatalogCarriesTheMoveYourselfFallback(t *testing.T) {
	agents := repoCatalogAgents(t)
	got := columnInstruction(t, agents, "qa-agent", domain.TaskColumnReadyForQA)
	if !strings.Contains(got, "move it yourself") {
		t.Errorf("qa-agent/ready_for_qa: missing the move-it-yourself fallback:\n%s", got)
	}
}

// The proposed:true / auto_rollback-off handling used to exist only in
// board.columnInstruction's released case; it now lives in released.md too.
func TestReleaseEngineerReleasedCarriesTheProposedRollbackRule(t *testing.T) {
	agents := repoCatalogAgents(t)
	got := columnInstruction(t, agents, "release-engineer", domain.TaskColumnReleased)
	if !strings.Contains(got, "proposed: true") {
		t.Errorf("release-engineer/released: missing the proposed:true rollback handling:\n%s", got)
	}
}

// A human dragging a card to released while its release is still verifying
// used to strand the release: the verdict wake landed in released.md, which
// only knew health incidents, so the agent left the release open (T-75).
func TestReleaseEngineerReleasedStillGivesAPendingVerdict(t *testing.T) {
	agents := repoCatalogAgents(t)
	got := columnInstruction(t, agents, "release-engineer", domain.TaskColumnReleased)
	for _, want := range []string{"release_status: awaiting_verdict", "finish_release", "rollback_release"} {
		if !strings.Contains(got, want) {
			t.Errorf("release-engineer/released: missing %q for a verdict woken after a human moved the card:\n%s", want, got)
		}
	}
}

// The batch-release handling (joined draft release, a human cutting it
// later, no-bound-runtime-environment evidence, batch rollback only
// reverting the default branch, local_run.tail for a failed local run) used
// to live only in board.columnInstruction's done case (WP10); it now lives
// entirely in release-engineer's own done.md.
func TestReleaseEngineerDoneCoversBatchReleases(t *testing.T) {
	agents := repoCatalogAgents(t)
	got := columnInstruction(t, agents, "release-engineer", domain.TaskColumnDone)
	for _, want := range []string{
		"joined the component's draft release",
		"a human just cut",
		"no bound runtime environment",
		"only reverts the default branch",
		"local_run.tail",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("release-engineer/done: missing %q:\n%s", want, got)
		}
	}
}

// The "do not edit or commit code" rule used to live only in
// board.columnInstruction's done/released cases (WP10); it now lives in
// release-engineer's own done.md and released.md.
func TestReleaseEngineerDoneAndReleasedForbidEditingCode(t *testing.T) {
	agents := repoCatalogAgents(t)
	for _, col := range []domain.TaskColumn{domain.TaskColumnDone, domain.TaskColumnReleased} {
		got := columnInstruction(t, agents, "release-engineer", col)
		if !strings.Contains(got, "Do not edit or commit code") {
			t.Errorf("release-engineer/%s: missing the no-code-editing rule:\n%s", col, got)
		}
	}
}

// "If it is not relevant to your role, take no action" used to be part of
// board.columnInstruction's todo case (WP10); it now lives in each
// developer's own todo.md, matching system-architect's analiz todo.md.
func TestDeveloperTodoSkipsWhenNotRelevant(t *testing.T) {
	agents := repoCatalogAgents(t)
	for _, slug := range []string{"backend-developer", "frontend-developer", "mobile-developer"} {
		got := columnInstruction(t, agents, slug, domain.TaskColumnTodo)
		if !strings.Contains(got, "not relevant to your role, take no action") {
			t.Errorf("%s/todo: missing the not-relevant-take-no-action rule:\n%s", slug, got)
		}
	}
}

// board.buildTriggerMessage's generic bookkeeping rules ("claim/move is the
// opening action of a step, never a step of its own") moved out of code and
// into each board-running agent's own prompt.md (or, for the product
// manager, its existing board-column-flow skill).
func TestBoardRunningAgentsCarryTheBookkeepingRule(t *testing.T) {
	agents := repoCatalogAgents(t)
	for _, slug := range []string{
		"backend-developer", "frontend-developer", "mobile-developer",
		"qa-agent", "system-architect", "release-engineer",
	} {
		agent, ok := agents[slug]
		if !ok {
			t.Fatalf("catalog is missing agent %q", slug)
		}
		if !strings.Contains(agent.SystemPrompt, "never a step of its own") {
			t.Errorf("%s prompt.md is missing the bookkeeping-is-not-a-step rule", slug)
		}
	}

	pm, ok := agents["product-manager"]
	if !ok {
		t.Fatal("catalog is missing agent \"product-manager\"")
	}
	found := false
	for _, sk := range pm.Skills {
		if sk.Name == "board-column-flow" && strings.Contains(sk.Content, "never a step of its own") {
			found = true
		}
	}
	if !found {
		t.Error("product-manager's board-column-flow skill is missing the bookkeeping-is-not-a-step rule")
	}
}
