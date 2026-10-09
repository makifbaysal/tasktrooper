package issuesync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fixture struct {
	svc     *Service
	tasks   *fakeTasks
	repos   *fakeRepos
	links   *fakeLinks
	imports *fakeImports
	gh      *fakeGitHubIssues
	jira    *fakeJiraIssueStore
	sync    *fakeSyncSettings
	repoID  uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	repoID := uuid.New()
	repo := domain.Repository{ID: repoID, Name: "widget", RemoteURL: "https://github.com/acme/widget.git"}
	fx := &fixture{
		tasks:   newFakeTasks(),
		repos:   newFakeRepos(repo),
		links:   newFakeLinks(),
		imports: newFakeImports(),
		gh:      newFakeGitHubIssues(),
		jira:    newFakeJiraIssueStore(),
		sync:    &fakeSyncSettings{},
		repoID:  repoID,
	}
	fx.svc = NewService(Deps{
		Tasks:        cascadingTasks{fakeTasks: fx.tasks, links: fx.links},
		Repositories: fx.repos,
		TaskTypes:    &fakeTaskTypes{valid: map[domain.TaskType]bool{"bug": true, "task": true}},
		Columns:      &fakeColumns{labels: map[string]string{"in_progress": "In Progress", "done": "Done", "released": "Released"}},
		Links:        fx.links,
		Imports:      fx.imports,
		GitHubTokens: &fakeGitHubTokens{token: "tok"},
		GitHubIssues: fx.gh,
		JiraSettings: &fakeJiraSettings{site: "https://acme.atlassian.net", email: "a@acme.com", token: "jtok"},
		SyncSettings: fx.sync,
		JiraFactory:  fakeJiraFactory(fx.jira),
	})
	return fx
}

func (fx *fixture) importGitHub(t *testing.T, key, actor string) Imported {
	t.Helper()
	out, err := fx.svc.Import(context.Background(), domain.IssueProviderGitHub, key, fx.repoID, orUser(actor), actor)
	if err != nil {
		t.Fatalf("Import %s: %v", key, err)
	}
	return out
}

func orUser(actor string) string {
	if actor == "" {
		return "user"
	}
	return actor
}

func TestImportGitHubHappyPath(t *testing.T) {
	fx := newFixture(t)
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{
		Key: "acme/widget#12", Title: "Crash on save", Body: "Steps to repro",
		URL: "https://github.com/acme/widget/issues/12", State: "open", Labels: []string{"bug"},
	})

	got := fx.importGitHub(t, "acme/widget#12", "user-1")

	if got.Task.Title != "Crash on save" {
		t.Errorf("title = %q", got.Task.Title)
	}
	if got.Task.TaskType != "bug" {
		t.Errorf("task type = %q, want bug", got.Task.TaskType)
	}
	if got.Task.CreatedBy != "user-1" {
		t.Errorf("created by = %q", got.Task.CreatedBy)
	}
	wantDesc := "Source: [acme/widget#12](https://github.com/acme/widget/issues/12)\n\nSteps to repro"
	if got.Task.Description != wantDesc {
		t.Errorf("description = %q, want %q", got.Task.Description, wantDesc)
	}
	if got.Link.ExternalKey != "acme/widget#12" || got.Link.ImportedBy != "user-1" {
		t.Errorf("link = %+v", got.Link)
	}
	if got.Import.ConversionStatus != domain.IssueConversionNone {
		t.Errorf("conversion status = %q, want none while no product manager is wired", got.Import.ConversionStatus)
	}
	if got.Import.IntakeTaskID == nil || *got.Import.IntakeTaskID != got.Task.ID {
		t.Errorf("import intake = %v, want %v", got.Import.IntakeTaskID, got.Task.ID)
	}
	if _, err := fx.links.GetByTask(context.Background(), got.Task.ID); err != nil {
		t.Errorf("link not recorded: %v", err)
	}
}

func TestImportDuplicateIsAlreadyImported(t *testing.T) {
	fx := newFixture(t)
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#12", Title: "Crash", URL: "u", State: "open"})
	fx.importGitHub(t, "acme/widget#12", "user-1")

	_, err := fx.svc.Import(context.Background(), domain.IssueProviderGitHub, "acme/widget#12", fx.repoID, "user-1", "user-1")
	var already *AlreadyImportedError
	if !errors.As(err, &already) {
		t.Fatalf("second import error = %v, want *AlreadyImportedError", err)
	}
	if already.TaskKey != "TASK-1" {
		t.Errorf("already imported task key = %q", already.TaskKey)
	}
	if fx.tasks.count() != 1 {
		t.Errorf("tasks = %d, want 1", fx.tasks.count())
	}
}

func TestImportAgainOnceEveryTaskWasDeleted(t *testing.T) {
	fx := newFixture(t)
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#12", Title: "Crash", URL: "u", State: "open"})
	first := fx.importGitHub(t, "acme/widget#12", "user-1")
	if err := fx.svc.tasks.DeleteTask(context.Background(), fx.repoID, first.Task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	second := fx.importGitHub(t, "acme/widget#12", "user-1")

	if second.Task.ID == first.Task.ID || second.Import.ID == first.Import.ID {
		t.Fatalf("expected a fresh task and import, got %+v", second)
	}
}

func TestImportGitHubRepoMismatchIsInvalid(t *testing.T) {
	fx := newFixture(t)
	fx.gh.addIssue("other", "repo", domain.ExternalIssue{Key: "other/repo#3", Title: "x", URL: "u"})

	_, err := fx.svc.Import(context.Background(), domain.IssueProviderGitHub, "other/repo#3", fx.repoID, "user-1", "user-1")
	if !errors.Is(err, ErrInvalidIssue) {
		t.Fatalf("err = %v, want ErrInvalidIssue", err)
	}
}

func TestImportGitHubMalformedKeyIsInvalid(t *testing.T) {
	fx := newFixture(t)
	_, err := fx.svc.Import(context.Background(), domain.IssueProviderGitHub, "not-a-key", fx.repoID, "user-1", "user-1")
	if !errors.Is(err, ErrInvalidIssue) {
		t.Fatalf("err = %v, want ErrInvalidIssue", err)
	}
}

func TestImportJiraHappyPathWithPriorityAndBugType(t *testing.T) {
	fx := newFixture(t)
	fx.jira.byKey["PROJ-7"] = domain.ExternalIssue{
		Key: "PROJ-7", Title: "NPE on login", Body: "stack trace",
		URL: "https://acme.atlassian.net/browse/PROJ-7", IssueType: "Bug", Priority: "High",
	}
	got, err := fx.svc.Import(context.Background(), domain.IssueProviderJira, "PROJ-7", fx.repoID, "user-1", "user-1")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got.Task.TaskType != "bug" {
		t.Errorf("task type = %q", got.Task.TaskType)
	}
	if got.Task.Priority != domain.TaskPriorityHigh {
		t.Errorf("priority = %q, want high", got.Task.Priority)
	}
}

func TestImportJiraInvalidKeyPattern(t *testing.T) {
	fx := newFixture(t)
	_, err := fx.svc.Import(context.Background(), domain.IssueProviderJira, "not-a-jira-key", fx.repoID, "user-1", "user-1")
	if !errors.Is(err, ErrInvalidIssue) {
		t.Fatalf("err = %v, want ErrInvalidIssue", err)
	}
}

func TestJiraPriorityMapping(t *testing.T) {
	cases := map[string]domain.TaskPriority{
		"Highest": domain.TaskPriorityCritical,
		"High":    domain.TaskPriorityHigh,
		"Medium":  domain.TaskPriorityMedium,
		"Low":     domain.TaskPriorityLow,
		"Lowest":  domain.TaskPriorityLow,
	}
	for name, want := range cases {
		got, ok := jiraPriorityToTaskPriority(name)
		if !ok || got != want {
			t.Errorf("jiraPriorityToTaskPriority(%q) = (%q, %v), want (%q, true)", name, got, ok, want)
		}
	}
	if _, ok := jiraPriorityToTaskPriority("Weird"); ok {
		t.Errorf("unrecognised priority should not map")
	}
}

func TestSearchMarksAlreadyImportedIssues(t *testing.T) {
	fx := newFixture(t)
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#1", Title: "One", URL: "u"})
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#2", Title: "Two", URL: "u"})
	fx.importGitHub(t, "acme/widget#1", "user-1")

	issues, err := fx.svc.Search(context.Background(), domain.IssueProviderGitHub, &fx.repoID, "", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var one, two *domain.ExternalIssue
	for i := range issues {
		switch issues[i].Key {
		case "acme/widget#1":
			one = &issues[i]
		case "acme/widget#2":
			two = &issues[i]
		}
	}
	if one == nil || one.ImportedTask == nil || one.ImportedTask.Key != "TASK-1" {
		t.Fatalf("issue #1 should be marked imported as TASK-1: %+v", one)
	}
	if two == nil || two.ImportedTask != nil {
		t.Fatalf("issue #2 should not be marked imported: %+v", two)
	}
}

func TestAutomaticImportLeavesImportedByEmpty(t *testing.T) {
	fx := newFixture(t)
	fx.sync.use(domain.IssueSyncSettings{Label: "tasktrooper", GitHubAutoImport: true, WriteBack: true})
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{
		Key: "acme/widget#9", Title: "Auto", URL: "u", Labels: []string{"tasktrooper"},
	})

	fx.svc.RunPollOnce(context.Background())

	imp := fx.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#9")
	if imp.ImportedBy != "" {
		t.Errorf("automatic import ImportedBy = %q, want empty", imp.ImportedBy)
	}
	links, _ := fx.links.ListByIssue(context.Background(), domain.IssueProviderGitHub, "acme/widget#9")
	if len(links) != 1 || links[0].ImportedBy != "" {
		t.Errorf("links = %+v", links)
	}
}

func TestResolveTaskTypeFallsBackWhenBugTypeInvalid(t *testing.T) {
	fx := newFixture(t)
	fx.svc.taskTypes = &fakeTaskTypes{valid: map[domain.TaskType]bool{}}
	got, err := fx.svc.resolveTaskType(context.Background(), true)
	if err != nil {
		t.Fatalf("resolveTaskType: %v", err)
	}
	if got != "" {
		t.Errorf("task type = %q, want empty (default)", got)
	}
}

func TestHandleGitHubIssuesEventImportsLabelledIssue(t *testing.T) {
	fx := newFixture(t)
	fx.sync.use(domain.IssueSyncSettings{Label: "tasktrooper", GitHubAutoImport: true})
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{Key: "acme/widget#5", Title: "Hook", URL: "u"})

	if ok, reason := fx.svc.HandleGitHubIssuesEvent(context.Background(), fx.repoID, "labeled", "acme/widget#5", []string{"other"}); ok {
		t.Fatalf("an issue without the label was imported (%s)", reason)
	}
	ok, reason := fx.svc.HandleGitHubIssuesEvent(context.Background(), fx.repoID, "labeled", "acme/widget#5", []string{"TaskTrooper"})
	if !ok {
		t.Fatalf("labelled issue not imported: %s", reason)
	}
	if ok, reason := fx.svc.HandleGitHubIssuesEvent(context.Background(), fx.repoID, "opened", "acme/widget#5", []string{"tasktrooper"}); ok || reason != "already imported" {
		t.Fatalf("second delivery = (%v, %q), want (false, already imported)", ok, reason)
	}
}

// waitFor polls cond: write-back and late conversion completions run on their
// own goroutines so the board operation they follow is never held up.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out")
}
