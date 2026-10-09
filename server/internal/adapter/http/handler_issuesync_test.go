package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/issuesync"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type isTasks struct {
	mu    sync.Mutex
	tasks map[uuid.UUID]domain.BoardTask
	n     int
}

func newIsTasks() *isTasks { return &isTasks{tasks: map[uuid.UUID]domain.BoardTask{}} }

func (f *isTasks) CreateTask(_ context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	t := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: fmt.Sprintf("TASK-%d", f.n), Title: req.Title, Column: domain.TaskColumnBacklog}
	f.tasks[t.ID] = t
	return t, nil
}

func (f *isTasks) GetTask(_ context.Context, _, taskID uuid.UUID) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return t, nil
}

func (f *isTasks) DeleteTask(_ context.Context, _, taskID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tasks, taskID)
	return nil
}

func (f *isTasks) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tasks)
}

type isLinks struct {
	mu    sync.Mutex
	links []domain.IssueLink
}

func (f *isLinks) Create(_ context.Context, link domain.IssueLink) (domain.IssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	link.ID = uuid.New()
	f.links = append(f.links, link)
	return link, nil
}

func (f *isLinks) GetByTask(_ context.Context, taskID uuid.UUID) (domain.IssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.TaskID == taskID {
			return l, nil
		}
	}
	return domain.IssueLink{}, port.ErrNotFound
}

func (f *isLinks) ListByIssue(_ context.Context, provider domain.IssueProvider, key string) ([]domain.IssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.IssueLink{}
	for _, l := range f.links {
		if l.Provider == provider && l.ExternalKey == key {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *isLinks) ExistingKeys(context.Context, domain.IssueProvider, []string) (map[string]domain.IssueLink, error) {
	return map[string]domain.IssueLink{}, nil
}
func (f *isLinks) UpdateColumn(context.Context, uuid.UUID, string) error { return nil }
func (f *isLinks) MarkIssueClosed(context.Context, domain.IssueProvider, string, time.Time) error {
	return nil
}

type isImports struct {
	mu   sync.Mutex
	rows map[uuid.UUID]domain.IssueImport
}

func newIsImports() *isImports { return &isImports{rows: map[uuid.UUID]domain.IssueImport{}} }

func (f *isImports) Create(_ context.Context, imp domain.IssueImport) (domain.IssueImport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.Provider == imp.Provider && r.ExternalKey == imp.ExternalKey {
			return domain.IssueImport{}, port.ErrIssueImportExists
		}
	}
	imp.ID = uuid.New()
	f.rows[imp.ID] = imp
	return imp, nil
}

func (f *isImports) Get(_ context.Context, id uuid.UUID) (domain.IssueImport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.IssueImport{}, port.ErrNotFound
	}
	return r, nil
}

func (f *isImports) GetByProviderKey(_ context.Context, provider domain.IssueProvider, key string) (domain.IssueImport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.Provider == provider && r.ExternalKey == key {
			return r, nil
		}
	}
	return domain.IssueImport{}, port.ErrNotFound
}

func (f *isImports) GetBySession(context.Context, uuid.UUID) (domain.IssueImport, error) {
	return domain.IssueImport{}, port.ErrNotFound
}
func (f *isImports) ListByStatus(context.Context, domain.IssueConversionStatus) ([]domain.IssueImport, error) {
	return nil, nil
}
func (f *isImports) ClaimConversion(context.Context, uuid.UUID) (bool, error) { return false, nil }
func (f *isImports) ResetConverting(context.Context) error                    { return nil }
func (f *isImports) SetConversion(context.Context, uuid.UUID, domain.IssueConversionStatus, *uuid.UUID, string) error {
	return nil
}
func (f *isImports) ClearIntakeTask(context.Context, uuid.UUID) error       { return nil }
func (f *isImports) MarkClosed(context.Context, uuid.UUID, time.Time) error { return nil }
func (f *isImports) Delete(_ context.Context, id uuid.UUID) error           { delete(f.rows, id); return nil }

type isGitHubIssues struct {
	byKey map[string]domain.ExternalIssue
}

func (f *isGitHubIssues) ListOpen(context.Context, string, string, string, int) ([]domain.ExternalIssue, error) {
	return nil, nil
}
func (f *isGitHubIssues) ListLabelled(context.Context, string, string, string, string, int) ([]domain.ExternalIssue, error) {
	return nil, nil
}
func (f *isGitHubIssues) Search(context.Context, string, string, string, string) ([]domain.ExternalIssue, error) {
	return nil, nil
}
func (f *isGitHubIssues) Get(_ context.Context, _, owner, repo string, number int) (domain.ExternalIssue, error) {
	iss, ok := f.byKey[owner+"/"+repo+"#"+strconv.Itoa(number)]
	if !ok {
		return domain.ExternalIssue{}, errors.New("not found")
	}
	return iss, nil
}
func (f *isGitHubIssues) AddComment(context.Context, string, string, string, int, string) error {
	return nil
}
func (f *isGitHubIssues) Close(context.Context, string, string, string, int) error { return nil }

type isGitHubTokens struct{}

func (isGitHubTokens) GitHubToken(context.Context) (string, error)  { return "tok", nil }
func (isGitHubTokens) SetGitHubToken(context.Context, string) error { return nil }
func (isGitHubTokens) DeleteGitHubToken(context.Context) error      { return nil }

type isSyncSettings struct {
	settings domain.IssueSyncSettings
}

func (f *isSyncSettings) IssueSyncSettings(context.Context) (domain.IssueSyncSettings, error) {
	return f.settings, nil
}
func (f *isSyncSettings) SetIssueSyncSettings(_ context.Context, s domain.IssueSyncSettings) error {
	f.settings = s
	return nil
}

func newIssueSyncService(repos issuesync.Repositories, tasks *isTasks, links *isLinks, settings domain.IssueSyncSettings) *issuesync.Service {
	return issuesync.NewService(issuesync.Deps{
		Tasks:        tasks,
		Repositories: repos,
		Links:        links,
		Imports:      newIsImports(),
		GitHubTokens: isGitHubTokens{},
		GitHubIssues: &isGitHubIssues{byKey: map[string]domain.ExternalIssue{
			"acme/widget#12": {Key: "acme/widget#12", Title: "Crash", URL: "https://github.com/acme/widget/issues/12", Labels: []string{"tasktrooper"}},
		}},
		SyncSettings: &isSyncSettings{settings: settings},
	})
}

type isFixture struct {
	app    *fiber.App
	repoID uuid.UUID
	tasks  *isTasks
	links  *isLinks
}

func newIssueSyncTestApp(t *testing.T) *isFixture {
	t.Helper()
	repoID := uuid.New()
	repos := newFakeDeployOpsRepositoryStore([]domain.Repository{
		{ID: repoID, Name: "widget", RemoteURL: "https://github.com/acme/widget.git"},
	})
	tasks := newIsTasks()
	links := &isLinks{}
	h := &Handler{issueSyncSvc: newIssueSyncService(repos, tasks, links, domain.DefaultIssueSyncSettings())}
	app := fiber.New()
	h.registerIssueSyncRoutes(app)
	return &isFixture{app: app, repoID: repoID, tasks: tasks, links: links}
}

func (f *isFixture) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.app.Test(req, 5000)
	require.NoError(t, err)
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out
}

func TestImportIssueHandlerHappyPath(t *testing.T) {
	f := newIssueSyncTestApp(t)
	status, raw := f.do(t, "POST", "/v1/issues/import", map[string]any{
		"provider": "github", "key": "acme/widget#12", "repository_id": f.repoID.String(),
	})
	require.Equal(t, fiber.StatusCreated, status, string(raw))
	var out struct {
		Task   domain.BoardTask   `json:"task"`
		Link   domain.IssueLink   `json:"link"`
		Import domain.IssueImport `json:"import"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, "Crash", out.Task.Title)
	require.Equal(t, "acme/widget#12", out.Link.ExternalKey)
	require.Equal(t, "acme/widget#12", out.Import.ExternalKey)
	require.Equal(t, domain.IssueConversionNone, out.Import.ConversionStatus)
}

func TestImportIssueHandlerDuplicateIs409(t *testing.T) {
	f := newIssueSyncTestApp(t)
	body := map[string]any{"provider": "github", "key": "acme/widget#12", "repository_id": f.repoID.String()}
	status, raw := f.do(t, "POST", "/v1/issues/import", body)
	require.Equal(t, fiber.StatusCreated, status, string(raw))

	status, raw = f.do(t, "POST", "/v1/issues/import", body)
	require.Equal(t, fiber.StatusConflict, status, string(raw))
	var out struct {
		Error        errorDetail `json:"error"`
		TaskID       string      `json:"task_id"`
		TaskKey      string      `json:"task_key"`
		RepositoryID string      `json:"repository_id"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, "issue_already_imported", out.Error.Type)
	require.NotEmpty(t, out.TaskID)
	require.Equal(t, "TASK-1", out.TaskKey)
	require.Equal(t, f.repoID.String(), out.RepositoryID)
}

func TestImportIssueHandlerInvalidKeyIs400(t *testing.T) {
	f := newIssueSyncTestApp(t)
	status, raw := f.do(t, "POST", "/v1/issues/import", map[string]any{
		"provider": "github", "key": "not-a-key", "repository_id": f.repoID.String(),
	})
	require.Equal(t, fiber.StatusBadRequest, status, string(raw))
	var out struct {
		Error errorDetail `json:"error"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, "invalid_issue", out.Error.Type)
}

func TestGetTaskIssueLinkHandler(t *testing.T) {
	f := newIssueSyncTestApp(t)
	path := func(taskID uuid.UUID) string {
		return "/v1/repositories/" + f.repoID.String() + "/tasks/" + taskID.String() + "/issue-link"
	}

	status, raw := f.do(t, "GET", path(uuid.New()), nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var out struct {
		Link   *domain.IssueLink   `json:"link"`
		Import *domain.IssueImport `json:"import"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Nil(t, out.Link)
	require.Nil(t, out.Import)

	status, raw = f.do(t, "POST", "/v1/issues/import", map[string]any{
		"provider": "github", "key": "acme/widget#12", "repository_id": f.repoID.String(),
	})
	require.Equal(t, fiber.StatusCreated, status, string(raw))
	var imported struct {
		Task domain.BoardTask `json:"task"`
	}
	require.NoError(t, json.Unmarshal(raw, &imported))

	status, raw = f.do(t, "GET", path(imported.Task.ID), nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotNil(t, out.Link)
	require.Equal(t, "acme/widget#12", out.Link.ExternalKey)
	require.NotNil(t, out.Import)
	require.Equal(t, domain.IssueConversionNone, out.Import.ConversionStatus)
}

func TestConvertIssueImportWithoutAProductManagerIs409(t *testing.T) {
	f := newIssueSyncTestApp(t)
	status, raw := f.do(t, "POST", "/v1/issues/imports/"+uuid.NewString()+"/convert", nil)
	require.Equal(t, fiber.StatusConflict, status, string(raw))
	var out struct {
		Error errorDetail `json:"error"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, "issue_conversion_unavailable", out.Error.Type)
}

func TestSearchHandlerRequiresRepositoryForGitHub(t *testing.T) {
	f := newIssueSyncTestApp(t)
	status, raw := f.do(t, "GET", "/v1/issues/search?provider=github", nil)
	require.Equal(t, fiber.StatusBadRequest, status, string(raw))
}

func TestIssueSyncSettingsHandlerRoundTrip(t *testing.T) {
	f := newIssueSyncTestApp(t)
	status, raw := f.do(t, "GET", "/v1/settings/issue-sync", nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var got domain.IssueSyncSettings
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, "tasktrooper", got.Label)
	require.True(t, got.ConvertWithPM)

	status, raw = f.do(t, "PUT", "/v1/settings/issue-sync", map[string]any{"label": ""})
	require.Equal(t, fiber.StatusBadRequest, status, string(raw))

	status, raw = f.do(t, "PUT", "/v1/settings/issue-sync", map[string]any{"label": "triage", "convert_with_pm": false})
	require.Equal(t, fiber.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, "triage", got.Label)
	require.False(t, got.ConvertWithPM)
}

func TestJiraStatusHandlerNotConnected(t *testing.T) {
	f := newIssueSyncTestApp(t)
	status, raw := f.do(t, "GET", "/v1/settings/jira", nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var got domain.JiraStatus
	require.NoError(t, json.Unmarshal(raw, &got))
	require.False(t, got.Connected)
}

// webhookIssuesTestRepoStore carries a real secret: the "issues" event's tests
// hinge on the HMAC actually verifying.
type webhookIssuesTestRepoStore struct {
	port.RepositoryStore
	repo   domain.Repository
	secret string
}

func (s *webhookIssuesTestRepoStore) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	if id != s.repo.ID {
		return domain.Repository{}, errors.New("not found")
	}
	return s.repo, nil
}

func (s *webhookIssuesTestRepoStore) List(context.Context) ([]domain.Repository, error) {
	return []domain.Repository{s.repo}, nil
}

func (s *webhookIssuesTestRepoStore) WebhookSecret(_ context.Context, id uuid.UUID) (string, error) {
	if id != s.repo.ID {
		return "", errors.New("not found")
	}
	return s.secret, nil
}

type issuesEventFixture struct {
	app    *fiber.App
	secret string
	tasks  *isTasks
}

func newIssuesEventTestApp(t *testing.T, autoImport bool) *issuesEventFixture {
	t.Helper()
	repo := domain.Repository{ID: uuid.New(), Name: "widget", RemoteURL: "https://github.com/acme/widget.git"}
	secret := "s3cret"
	repoStore := &webhookIssuesTestRepoStore{repo: repo, secret: secret}
	repoSvc := repository.NewService(repoStore, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	tasks := newIsTasks()
	issueSyncSvc := newIssueSyncService(repoStore, tasks, &isLinks{}, domain.IssueSyncSettings{
		Label: "tasktrooper", GitHubAutoImport: autoImport,
	})
	h := &Handler{repositorySvc: repoSvc, issueSyncSvc: issueSyncSvc}
	app := fiber.New()
	h.registerRepositoryRoutes(app)
	return &issuesEventFixture{app: app, secret: secret, tasks: tasks}
}

func (f *issuesEventFixture) post(t *testing.T, body []byte, sign bool) *http.Response {
	t.Helper()
	req := httptest.NewRequest("POST", githubWebhookPath, bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", uuid.NewString())
	if sign {
		req.Header.Set("X-Hub-Signature-256", signBody(f.secret, body))
	}
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	return resp
}

func issuesEventBody(action string, number int, labels ...string) []byte {
	labelObjs := make([]map[string]string, len(labels))
	for i, l := range labels {
		labelObjs[i] = map[string]string{"name": l}
	}
	raw, _ := json.Marshal(map[string]any{
		"action":     action,
		"repository": map[string]any{"full_name": "acme/widget"},
		"issue":      map[string]any{"number": number, "labels": labelObjs},
	})
	return raw
}

func decodeAccepted(t *testing.T, resp *http.Response) (bool, string) {
	t.Helper()
	require.Equal(t, fiber.StatusAccepted, resp.StatusCode)
	var out struct {
		Accepted bool   `json:"accepted"`
		Reason   string `json:"reason"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out.Accepted, out.Reason
}

func TestGitHubIssuesWebhookImportsALabelledIssue(t *testing.T) {
	f := newIssuesEventTestApp(t, true)

	accepted, reason := decodeAccepted(t, f.post(t, issuesEventBody("labeled", 12, "tasktrooper"), true))

	require.True(t, accepted, reason)
	require.Equal(t, 1, f.tasks.count())
}

func TestGitHubIssuesWebhookIgnoresOtherLabelsAndActions(t *testing.T) {
	f := newIssuesEventTestApp(t, true)

	accepted, _ := decodeAccepted(t, f.post(t, issuesEventBody("labeled", 12, "not-tasktrooper"), true))
	require.False(t, accepted, "an unrelated label must not import")

	accepted, _ = decodeAccepted(t, f.post(t, issuesEventBody("closed", 12, "tasktrooper"), true))
	require.False(t, accepted, "an untracked action must not import")

	require.Equal(t, 0, f.tasks.count())
}

func TestGitHubIssuesWebhookRespectsAutoImportOff(t *testing.T) {
	f := newIssuesEventTestApp(t, false)

	accepted, reason := decodeAccepted(t, f.post(t, issuesEventBody("labeled", 12, "tasktrooper"), true))

	require.False(t, accepted)
	require.Equal(t, "github auto-import is off", reason)
	require.Equal(t, 0, f.tasks.count())
}

func TestGitHubIssuesWebhookStillEnforcesTheSignature(t *testing.T) {
	f := newIssuesEventTestApp(t, true)
	resp := f.post(t, issuesEventBody("labeled", 12, "tasktrooper"), false)
	require.Equal(t, fiber.StatusUnauthorized, resp.StatusCode, "unsigned deliveries must be rejected")
	require.Equal(t, 0, f.tasks.count())
}
