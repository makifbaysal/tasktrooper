package issuesync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeSessions stands in for session.Service. SendMessage puts the session on
// the context the way its prepareRunContext does, then runs turn as the
// product manager: whatever turn opens through fakeTasks reaches the
// service's TaskCreated, as a create_board_task call reaches it through
// repository.Service.
type fakeSessions struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]domain.Session
	requests []domain.CreateSessionRequest
	messages []domain.SessionMessageRequest
	turn     func(ctx context.Context, sessionID uuid.UUID) (domain.AgentResponse, error)
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{sessions: map[uuid.UUID]domain.Session{}}
}

func (f *fakeSessions) Create(_ context.Context, req domain.CreateSessionRequest) (domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sess := domain.Session{ID: uuid.New(), Title: req.Title, ProjectID: req.ProjectID, AgentID: req.AgentID}
	f.sessions[sess.ID] = sess
	f.requests = append(f.requests, req)
	return sess, nil
}

func (f *fakeSessions) Get(_ context.Context, id uuid.UUID) (domain.Session, []domain.SessionMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sess, ok := f.sessions[id]
	if !ok {
		return domain.Session{}, nil, port.ErrNotFound
	}
	return sess, nil, nil
}

func (f *fakeSessions) SendMessage(ctx context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest, _ domain.ToolPolicy) (domain.AgentResponse, error) {
	f.mu.Lock()
	f.messages = append(f.messages, req)
	turn := f.turn
	f.mu.Unlock()
	if turn == nil {
		return domain.AgentResponse{}, nil
	}
	return turn(registry.ContextWithSessionID(ctx, sessionID), sessionID)
}

func (f *fakeSessions) sent() []domain.SessionMessageRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.SessionMessageRequest(nil), f.messages...)
}

type fakeRoles struct {
	pm *uuid.UUID
}

func (f *fakeRoles) RoleByKey(_ context.Context, key string) (domain.AgentRole, error) {
	if key != domain.RoleKeyProductManager {
		return domain.AgentRole{}, port.ErrNotFound
	}
	return domain.AgentRole{ID: uuid.New(), Key: key}, nil
}

func (f *fakeRoles) AgentForRole(context.Context, uuid.UUID, string) (*uuid.UUID, error) {
	return f.pm, nil
}

type fakeAgents struct {
	agents map[uuid.UUID]domain.Agent
}

func (f *fakeAgents) GetAgent(_ context.Context, id uuid.UUID) (domain.Agent, error) {
	a, ok := f.agents[id]
	if !ok {
		return domain.Agent{}, port.ErrNotFound
	}
	return a, nil
}

type conversionFixture struct {
	*fixture
	sessions *fakeSessions
	roles    *fakeRoles
	pmID     uuid.UUID
}

func newConversionFixture(t *testing.T) *conversionFixture {
	t.Helper()
	fx := newFixture(t)
	fx.tasks.observer = fx.svc
	pmID := uuid.New()
	cf := &conversionFixture{
		fixture:  fx,
		sessions: newFakeSessions(),
		roles:    &fakeRoles{pm: &pmID},
		pmID:     pmID,
	}
	fx.svc.conv.Store(&ConversionDeps{
		Sessions: cf.sessions,
		Roles:    cf.roles,
		Agents:   &fakeAgents{agents: map[uuid.UUID]domain.Agent{pmID: {ID: pmID, Name: "product-manager", Enabled: true}}},
	})
	fx.gh.addIssue("acme", "widget", domain.ExternalIssue{
		Key: "acme/widget#7", Title: "Login and logout", Body: "Users need to sign in, and to sign out.",
		URL: "https://github.com/acme/widget/issues/7", State: "open",
	})
	return cf
}

// openTasks makes the product manager's turn open one task per title the way
// create_board_task would: through the board, on the turn's context.
func (cf *conversionFixture) openTasks(titles ...string) func(ctx context.Context, _ uuid.UUID) (domain.AgentResponse, error) {
	return func(ctx context.Context, _ uuid.UUID) (domain.AgentResponse, error) {
		for _, title := range titles {
			if _, err := cf.tasks.CreateTask(ctx, cf.repoID, domain.CreateBoardTaskRequest{
				Title:       title,
				Description: "Source: [acme/widget#7](https://github.com/acme/widget/issues/7)\n\nAs a user …",
				TaskType:    domain.TaskTypeTask,
				AcceptanceCriteria: []domain.AcceptanceCriterionInput{
					{Text: "Given a signed-out user, when they sign in, then they see the dashboard"},
				},
				CreatedBy: "agent",
			}); err != nil {
				return domain.AgentResponse{}, err
			}
		}
		return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "opened"}}, nil
	}
}

func TestConversionSplitsAnIssueIntoSeveralLinkedTasks(t *testing.T) {
	cf := newConversionFixture(t)
	cf.sessions.turn = cf.openTasks("Sign in with email", "Sign out from the header")

	imported := cf.importGitHub(t, "acme/widget#7", "user-1")
	if imported.Import.ConversionStatus != domain.IssueConversionPending {
		t.Fatalf("conversion status = %q, want pending", imported.Import.ConversionStatus)
	}
	time.Sleep(30 * time.Millisecond)
	if got := cf.gh.Comments(); len(got) != 0 {
		t.Fatalf("the tracked comment must wait for the product manager's tasks, got %v", got)
	}

	cf.svc.RunConversionsOnce(context.Background())

	if len(cf.sessions.requests) != 1 {
		t.Fatalf("sessions opened = %d, want 1", len(cf.sessions.requests))
	}
	req := cf.sessions.requests[0]
	if req.ProjectID == nil || *req.ProjectID != cf.repoID || req.AgentID == nil || *req.AgentID != cf.pmID || req.TaskID != nil {
		t.Errorf("conversion chat = %+v, want the repository's chat with the product manager, bound to no task", req)
	}
	sent := cf.sessions.sent()
	if len(sent) != 1 {
		t.Fatalf("messages sent = %d, want 1", len(sent))
	}
	if sent[0].Orchestrate == nil || *sent[0].Orchestrate {
		t.Errorf("the conversion turn must not be orchestrated: %+v", sent[0].Orchestrate)
	}
	for _, want := range []string{imported.Task.Key, "acme/widget#7", "create_board_task", "widget"} {
		if !strings.Contains(sent[0].Content, want) {
			t.Errorf("conversion request does not mention %q:\n%s", want, sent[0].Content)
		}
	}
	if strings.Contains(sent[0].Content, "Users need to sign in") {
		t.Errorf("the issue body must reach the agent through the board, not as the user's own words")
	}

	links, _ := cf.links.ListByIssue(context.Background(), domain.IssueProviderGitHub, "acme/widget#7")
	if len(links) != 2 {
		t.Fatalf("linked tasks = %d, want the 2 the product manager opened: %+v", len(links), links)
	}
	if cf.tasks.exists(imported.Task.ID) {
		t.Errorf("the imported placeholder task should have been retired")
	}
	imp := cf.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#7")
	if imp.ConversionStatus != domain.IssueConversionConverted || imp.IntakeTaskID != nil {
		t.Errorf("import = %+v, want converted with no intake task", imp)
	}
	waitFor(t, func() bool { return len(cf.gh.Comments()) == 1 })
	if got, want := cf.gh.Comments()[0], "acme/widget#7: Tracked in TaskTrooper as TASK-2, TASK-3."; got != want {
		t.Errorf("tracked comment = %q, want %q", got, want)
	}

	cf.svc.TaskMoved(context.Background(), cf.tasks.setColumn(links[0].TaskID, domain.TaskColumnDone))
	waitFor(t, func() bool { return len(cf.gh.Comments()) == 2 })
	if len(cf.gh.Closed()) != 0 {
		t.Fatalf("the issue closed while TASK-3 is still open")
	}
	if got := cf.gh.Comments()[1]; got != "acme/widget#7: TaskTrooper: TASK-2 moved to Done." {
		t.Errorf("first done = %q", got)
	}

	cf.svc.TaskMoved(context.Background(), cf.tasks.setColumn(links[1].TaskID, domain.TaskColumnReleased))
	waitFor(t, func() bool { return len(cf.gh.Closed()) == 1 && len(cf.gh.Comments()) == 3 })
	if got := cf.gh.Comments()[2]; got != "acme/widget#7: TaskTrooper: TASK-2, TASK-3 are done." {
		t.Errorf("closing comment = %q", got)
	}
}

func TestConversionOfASingleDeliverableReplacesTheImportedTask(t *testing.T) {
	cf := newConversionFixture(t)
	cf.sessions.turn = cf.openTasks("Sign in and out")
	imported := cf.importGitHub(t, "acme/widget#7", "")

	cf.svc.RunConversionsOnce(context.Background())

	links, _ := cf.links.ListByIssue(context.Background(), domain.IssueProviderGitHub, "acme/widget#7")
	if len(links) != 1 || links[0].TaskID == imported.Task.ID {
		t.Fatalf("links = %+v, want the product manager's one task", links)
	}
	if links[0].ImportedBy != "" {
		t.Errorf("an automatic import's converted task carries ImportedBy %q", links[0].ImportedBy)
	}
	if cf.tasks.count() != 1 {
		t.Errorf("tasks on the board = %d, want 1", cf.tasks.count())
	}
}

func TestConversionWithoutAProductManagerKeepsTheImportedTask(t *testing.T) {
	cf := newConversionFixture(t)
	cf.roles.pm = nil
	imported := cf.importGitHub(t, "acme/widget#7", "user-1")

	cf.svc.RunConversionsOnce(context.Background())

	imp := cf.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#7")
	if imp.ConversionStatus != domain.IssueConversionSkipped || imp.ConversionError == "" {
		t.Errorf("import = %+v, want skipped with a reason", imp)
	}
	if !cf.tasks.exists(imported.Task.ID) {
		t.Fatal("the imported task must stay when nobody can convert it")
	}
	if len(cf.sessions.sent()) != 0 {
		t.Errorf("no product manager, yet a turn ran")
	}
	waitFor(t, func() bool { return len(cf.gh.Comments()) == 1 })
	if got := cf.gh.Comments()[0]; got != "acme/widget#7: Tracked in TaskTrooper as "+imported.Task.Key+"." {
		t.Errorf("comment = %q", got)
	}
}

func TestConversionThatOpensNoTaskFallsBackToTheImportedTask(t *testing.T) {
	cf := newConversionFixture(t)
	cf.sessions.turn = func(context.Context, uuid.UUID) (domain.AgentResponse, error) {
		return domain.AgentResponse{Message: domain.Message{Content: "This issue is a question, not work."}}, nil
	}
	imported := cf.importGitHub(t, "acme/widget#7", "user-1")

	cf.svc.RunConversionsOnce(context.Background())

	imp := cf.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#7")
	if imp.ConversionStatus != domain.IssueConversionFailed || !strings.Contains(imp.ConversionError, "a question, not work") {
		t.Errorf("import = %+v, want failed with the product manager's reason", imp)
	}
	if !cf.tasks.exists(imported.Task.ID) {
		t.Fatal("the imported task must stay")
	}
}

func TestConversionRunErrorFallsBackToTheImportedTask(t *testing.T) {
	cf := newConversionFixture(t)
	cf.sessions.turn = func(context.Context, uuid.UUID) (domain.AgentResponse, error) {
		return domain.AgentResponse{}, errors.New("provider unreachable")
	}
	imported := cf.importGitHub(t, "acme/widget#7", "user-1")

	cf.svc.RunConversionsOnce(context.Background())

	if imp := cf.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#7"); imp.ConversionStatus != domain.IssueConversionFailed {
		t.Errorf("status = %q, want failed", imp.ConversionStatus)
	}
	if !cf.tasks.exists(imported.Task.ID) {
		t.Fatal("the imported task must stay")
	}
}

// A question ends the turn with nothing opened; the person answers in the same
// chat later, and the tasks that answer produces still belong to the issue.
func TestConversionQuestionCompletesWhenTheAnswerOpensTasks(t *testing.T) {
	cf := newConversionFixture(t)
	cf.sessions.turn = func(context.Context, uuid.UUID) (domain.AgentResponse, error) {
		return domain.AgentResponse{Clarification: &domain.ClarificationRequest{}}, nil
	}
	imported := cf.importGitHub(t, "acme/widget#7", "user-1")

	cf.svc.RunConversionsOnce(context.Background())

	imp := cf.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#7")
	if imp.ConversionStatus != domain.IssueConversionNeedsInput || imp.ConversionSessionID == nil {
		t.Fatalf("import = %+v, want needs_input with its chat", imp)
	}
	if !cf.tasks.exists(imported.Task.ID) {
		t.Fatal("the imported task must stay until tasks replace it")
	}

	answerTurn := registry.ContextWithSessionID(context.Background(), *imp.ConversionSessionID)
	if _, err := cf.tasks.CreateTask(answerTurn, cf.repoID, domain.CreateBoardTaskRequest{Title: "Sign in"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	waitFor(t, func() bool { return !cf.tasks.exists(imported.Task.ID) })
	waitFor(t, func() bool {
		return cf.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#7").ConversionStatus == domain.IssueConversionConverted
	})
	waitFor(t, func() bool { return len(cf.gh.Comments()) == 1 })
	if got := cf.gh.Comments()[0]; got != "acme/widget#7: Tracked in TaskTrooper as TASK-2." {
		t.Errorf("comment = %q", got)
	}
}

// A restart in the middle of a conversion must not run the product manager a
// second time over tasks it already opened.
func TestConversionInterruptedAfterOpeningTasksFinishesWithoutRerunning(t *testing.T) {
	cf := newConversionFixture(t)
	imported := cf.importGitHub(t, "acme/widget#7", "user-1")
	sessionID := uuid.New()
	if err := cf.imports.SetConversion(context.Background(), imported.Import.ID, domain.IssueConversionConverting, &sessionID, ""); err != nil {
		t.Fatalf("SetConversion: %v", err)
	}
	if _, err := cf.tasks.CreateTask(registry.ContextWithSessionID(context.Background(), sessionID), cf.repoID,
		domain.CreateBoardTaskRequest{Title: "Sign in"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := cf.imports.ResetConverting(context.Background()); err != nil {
		t.Fatalf("ResetConverting: %v", err)
	}

	cf.svc.RunConversionsOnce(context.Background())

	if n := len(cf.sessions.sent()); n != 0 {
		t.Fatalf("the product manager ran again (%d turns)", n)
	}
	if imp := cf.imports.byKey(t, domain.IssueProviderGitHub, "acme/widget#7"); imp.ConversionStatus != domain.IssueConversionConverted {
		t.Errorf("status = %q, want converted", imp.ConversionStatus)
	}
	if cf.tasks.exists(imported.Task.ID) {
		t.Errorf("the imported task should be retired")
	}
}

func TestTaskOpenedInAnotherChatIsNotLinked(t *testing.T) {
	cf := newConversionFixture(t)
	cf.importGitHub(t, "acme/widget#7", "user-1")

	other := registry.ContextWithSessionID(context.Background(), uuid.New())
	if _, err := cf.tasks.CreateTask(other, cf.repoID, domain.CreateBoardTaskRequest{Title: "Unrelated"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := cf.tasks.CreateTask(context.Background(), cf.repoID, domain.CreateBoardTaskRequest{Title: "By hand"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	links, _ := cf.links.ListByIssue(context.Background(), domain.IssueProviderGitHub, "acme/widget#7")
	if len(links) != 1 {
		t.Fatalf("links = %d, want only the imported task's", len(links))
	}
}

func TestConversionOffInSettingsImportsPlainly(t *testing.T) {
	cf := newConversionFixture(t)
	settings := domain.DefaultIssueSyncSettings()
	settings.ConvertWithPM = false
	cf.sync.use(settings)

	imported := cf.importGitHub(t, "acme/widget#7", "user-1")

	if imported.Import.ConversionStatus != domain.IssueConversionNone {
		t.Fatalf("status = %q, want none", imported.Import.ConversionStatus)
	}
	waitFor(t, func() bool { return len(cf.gh.Comments()) == 1 })
	cf.svc.RunConversionsOnce(context.Background())
	if len(cf.sessions.sent()) != 0 {
		t.Errorf("a conversion ran although it is off")
	}
}

func TestRetryConversionAfterAProductManagerWasAdded(t *testing.T) {
	cf := newConversionFixture(t)
	pm := cf.roles.pm
	cf.roles.pm = nil
	imported := cf.importGitHub(t, "acme/widget#7", "user-1")
	cf.svc.RunConversionsOnce(context.Background())

	if _, err := cf.svc.RetryConversion(context.Background(), uuid.New()); err == nil {
		t.Errorf("retrying an unknown import succeeded")
	}
	cf.roles.pm = pm
	cf.sessions.turn = cf.openTasks("Sign in", "Sign out")
	imp, err := cf.svc.RetryConversion(context.Background(), imported.Import.ID)
	if err != nil {
		t.Fatalf("RetryConversion: %v", err)
	}
	if imp.ConversionStatus != domain.IssueConversionPending {
		t.Fatalf("status = %q, want pending", imp.ConversionStatus)
	}
	cf.svc.RunConversionsOnce(context.Background())

	if got := cf.links.keysFor(domain.IssueProviderGitHub, "acme/widget#7"); len(got) != 2 {
		t.Fatalf("linked keys = %v, want 2", got)
	}
	if _, err := cf.svc.RetryConversion(context.Background(), imported.Import.ID); !errors.Is(err, ErrConversionNotRetryable) {
		t.Errorf("retrying a converted import: err = %v, want ErrConversionNotRetryable", err)
	}
}
