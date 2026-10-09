package issuesync

import (
	"context"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestWriteBackCommentsOnImport(t *testing.T) {
	fx := newFixture(t)
	fx.sync.use(domain.IssueSyncSettings{WriteBack: true})
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#1", Title: "One", URL: "u"})

	got := fx.importGitHub(t, "acme/widget#1", "user-1")

	waitFor(t, func() bool { return len(fx.gh.Comments()) == 1 })
	want := "acme/widget#1: Tracked in TaskTrooper as " + got.Task.Key + "."
	if fx.gh.Comments()[0] != want {
		t.Errorf("comment = %q, want %q", fx.gh.Comments()[0], want)
	}
}

func TestWriteBackDisabledSkipsComment(t *testing.T) {
	fx := newFixture(t)
	fx.sync.use(domain.IssueSyncSettings{WriteBack: false})
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#1", Title: "One", URL: "u"})

	fx.importGitHub(t, "acme/widget#1", "user-1")

	time.Sleep(50 * time.Millisecond)
	if len(fx.gh.Comments()) != 0 {
		t.Fatalf("write-back is off, expected no comments, got %v", fx.gh.Comments())
	}
}

func TestWriteBackCommentsOncePerColumnChange(t *testing.T) {
	fx := newFixture(t)
	fx.sync.use(domain.IssueSyncSettings{WriteBack: true})
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#1", Title: "One", URL: "u"})
	got := fx.importGitHub(t, "acme/widget#1", "user-1")
	waitFor(t, func() bool { return len(fx.gh.Comments()) == 1 })

	moved := fx.tasks.setColumn(got.Task.ID, domain.TaskColumnInProgress)
	fx.svc.TaskMoved(context.Background(), moved)
	waitFor(t, func() bool { return len(fx.gh.Comments()) == 2 })
	want := "acme/widget#1: TaskTrooper: " + got.Task.Key + " moved to In Progress."
	if fx.gh.Comments()[1] != want {
		t.Errorf("comment = %q, want %q", fx.gh.Comments()[1], want)
	}

	fx.svc.TaskMoved(context.Background(), moved)
	time.Sleep(50 * time.Millisecond)
	if len(fx.gh.Comments()) != 2 {
		t.Fatalf("expected no extra comment for an unchanged column, got %v", fx.gh.Comments())
	}
}

func TestWriteBackClosesOnceOnDone(t *testing.T) {
	fx := newFixture(t)
	fx.sync.use(domain.IssueSyncSettings{WriteBack: true})
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#1", Title: "One", URL: "u"})
	got := fx.importGitHub(t, "acme/widget#1", "user-1")
	waitFor(t, func() bool { return len(fx.gh.Comments()) == 1 })

	done := fx.tasks.setColumn(got.Task.ID, domain.TaskColumnDone)
	fx.svc.TaskMoved(context.Background(), done)
	waitFor(t, func() bool { return len(fx.gh.Closed()) == 1 && len(fx.gh.Comments()) == 2 })

	link, err := fx.links.GetByTask(context.Background(), got.Task.ID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if link.ClosedAt == nil {
		t.Fatal("expected the link to be marked closed")
	}
	if imp := fx.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#1"); imp.ClosedAt == nil {
		t.Fatal("expected the import to be marked closed")
	}

	released := fx.tasks.setColumn(got.Task.ID, domain.TaskColumnReleased)
	fx.svc.TaskMoved(context.Background(), released)
	time.Sleep(50 * time.Millisecond)
	if len(fx.gh.Closed()) != 1 {
		t.Fatalf("expected exactly one close call, got %d", len(fx.gh.Closed()))
	}
	if len(fx.gh.Comments()) != 2 {
		t.Fatalf("expected no extra done comment, got %v", fx.gh.Comments())
	}
}

func TestWriteBackSkipsUnlinkedTasks(t *testing.T) {
	fx := newFixture(t)
	fx.sync.use(domain.IssueSyncSettings{WriteBack: true})
	task, err := fx.tasks.CreateTask(context.Background(), fx.repoID, domain.CreateBoardTaskRequest{Title: "Not linked"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	moved := fx.tasks.setColumn(task.ID, domain.TaskColumnInProgress)
	fx.svc.TaskMoved(context.Background(), moved)
	time.Sleep(50 * time.Millisecond)
	if len(fx.gh.Comments()) != 0 {
		t.Fatalf("expected no comments for an unlinked task, got %v", fx.gh.Comments())
	}
}

func TestWriteBackTransitionsJiraIssueOnDone(t *testing.T) {
	fx := newFixture(t)
	fx.sync.use(domain.IssueSyncSettings{WriteBack: true})
	fx.jira.byKey["PROJ-3"] = domain.ExternalIssue{Key: "PROJ-3", Title: "Jira one", URL: "https://acme.atlassian.net/browse/PROJ-3"}
	got, err := fx.svc.Import(context.Background(), domain.IssueProviderJira, "PROJ-3", fx.repoID, "user-1", "user-1")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	fx.svc.TaskMoved(context.Background(), fx.tasks.setColumn(got.Task.ID, domain.TaskColumnDone))

	waitFor(t, func() bool { return fx.jira.isDone("PROJ-3") })
}
