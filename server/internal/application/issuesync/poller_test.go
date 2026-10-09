package issuesync

import (
	"context"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestPollDoesNothingWhenAutoImportOff(t *testing.T) {
	fx := newFixture(t)
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{
		Key: "acme/widget#1", Title: "One", URL: "u", Labels: []string{"tasktrooper"},
	})

	fx.svc.RunPollOnce(context.Background())

	if fx.tasks.count() != 0 {
		t.Fatalf("expected no tasks created, got %d", fx.tasks.count())
	}
}

func TestPollImportsOnlyLabelledMissingGitHubIssues(t *testing.T) {
	fx := newFixture(t)
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{
		Key: "acme/widget#1", Title: "Labelled", URL: "u", Labels: []string{"tasktrooper"},
	})
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#2", Title: "Unlabelled", URL: "u"})
	fx.sync.use(domain.IssueSyncSettings{Label: "tasktrooper", GitHubAutoImport: true, WriteBack: true})

	fx.svc.RunPollOnce(context.Background())

	if fx.tasks.count() != 1 {
		t.Fatalf("expected exactly 1 task from the labelled issue, got %d", fx.tasks.count())
	}
	imp := fx.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#1")
	if imp.RepositoryID != fx.repoID {
		t.Errorf("import repository = %v, want %v", imp.RepositoryID, fx.repoID)
	}

	fx.svc.RunPollOnce(context.Background())
	if fx.tasks.count() != 1 {
		t.Fatalf("second pass created another task: now %d", fx.tasks.count())
	}
}

// Deleting an automatically imported task means "not this one": the poller
// must not bring it back while the issue keeps its label.
func TestPollDoesNotReimportADeletedTask(t *testing.T) {
	fx := newFixture(t)
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{
		Key: "acme/widget#1", Title: "Labelled", URL: "u", Labels: []string{"tasktrooper"},
	})
	fx.sync.use(domain.IssueSyncSettings{Label: "tasktrooper", GitHubAutoImport: true})
	fx.svc.RunPollOnce(context.Background())
	imp := fx.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#1")
	if err := fx.svc.tasks.DeleteTask(context.Background(), fx.repoID, *imp.IntakeTaskID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	fx.svc.RunPollOnce(context.Background())

	if fx.tasks.count() != 0 {
		t.Fatalf("the poller re-imported a deleted task: %d tasks", fx.tasks.count())
	}
}

func TestPollImportsJiraProjectMapping(t *testing.T) {
	fx := newFixture(t)
	issue := domain.ExternalIssue{Key: "PROJ-1", Title: "From Jira", URL: "u"}
	fx.jira.search = []domain.ExternalIssue{issue}
	fx.jira.byKey["PROJ-1"] = issue
	fx.sync.use(domain.IssueSyncSettings{
		Label: "tasktrooper", JiraAutoImport: true, WriteBack: true,
		JiraProjects: []domain.JiraProjectMapping{{ProjectKey: "PROJ", RepositoryID: fx.repoID}},
	})

	fx.svc.RunPollOnce(context.Background())

	if fx.tasks.count() != 1 {
		t.Fatalf("expected 1 task from jira poll, got %d", fx.tasks.count())
	}
}
