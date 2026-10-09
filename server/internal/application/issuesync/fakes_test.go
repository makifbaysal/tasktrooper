package issuesync

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeTasks struct {
	mu        sync.Mutex
	tasks     map[uuid.UUID]domain.BoardTask
	deleted   []uuid.UUID
	nextKeyN  int
	createErr error
	// observer stands in for repository.Service calling its
	// TaskCreatedObserver at the end of CreateTask.
	observer interface {
		TaskCreated(ctx context.Context, task domain.BoardTask)
	}
}

func newFakeTasks() *fakeTasks { return &fakeTasks{tasks: map[uuid.UUID]domain.BoardTask{}} }

func (f *fakeTasks) CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.mu.Lock()
	if f.createErr != nil {
		f.mu.Unlock()
		return domain.BoardTask{}, f.createErr
	}
	f.nextKeyN++
	column := req.Column
	if column == "" {
		column = domain.TaskColumnBacklog
	}
	t := domain.BoardTask{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		Key:          fmt.Sprintf("TASK-%d", f.nextKeyN),
		Title:        req.Title,
		Description:  req.Description,
		TaskType:     req.TaskType,
		Priority:     req.Priority,
		Column:       column,
		CreatedBy:    req.CreatedBy,
	}
	if t.Priority == "" {
		t.Priority = domain.TaskPriorityMedium
	}
	f.tasks[t.ID] = t
	observer := f.observer
	f.mu.Unlock()
	if observer != nil {
		observer.TaskCreated(ctx, t)
	}
	return t, nil
}

func (f *fakeTasks) GetTask(_ context.Context, _, taskID uuid.UUID) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return t, nil
}

func (f *fakeTasks) DeleteTask(_ context.Context, _, taskID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tasks[taskID]; !ok {
		return domain.ErrBoardTaskNotFound
	}
	delete(f.tasks, taskID)
	f.deleted = append(f.deleted, taskID)
	return nil
}

func (f *fakeTasks) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tasks)
}

func (f *fakeTasks) exists(id uuid.UUID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.tasks[id]
	return ok
}

func (f *fakeTasks) setColumn(id uuid.UUID, col domain.TaskColumn) domain.BoardTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[id]
	t.Column = col
	f.tasks[id] = t
	return t
}

type fakeRepos struct {
	repos map[uuid.UUID]domain.Repository
}

func newFakeRepos(repos ...domain.Repository) *fakeRepos {
	m := map[uuid.UUID]domain.Repository{}
	for _, r := range repos {
		m[r.ID] = r
	}
	return &fakeRepos{repos: m}
}

func (f *fakeRepos) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	r, ok := f.repos[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("repository %s: %w", id, port.ErrNotFound)
	}
	return r, nil
}

func (f *fakeRepos) List(context.Context) ([]domain.Repository, error) {
	out := make([]domain.Repository, 0, len(f.repos))
	for _, r := range f.repos {
		out = append(out, r)
	}
	return out, nil
}

type fakeTaskTypes struct {
	valid map[domain.TaskType]bool
}

func (f *fakeTaskTypes) TaskTypeExists(_ context.Context, t domain.TaskType) (bool, error) {
	return f.valid[t], nil
}

type fakeColumns struct {
	labels map[string]string
}

func (f *fakeColumns) ListColumns(context.Context) ([]domain.BoardColumn, error) {
	out := make([]domain.BoardColumn, 0, len(f.labels))
	for slug, label := range f.labels {
		out = append(out, domain.BoardColumn{Slug: slug, Label: label})
	}
	return out, nil
}

// fakeLinks drops a task's link when fakeTasks deletes the task only through
// cascade(), the way issue_links.task_id's ON DELETE CASCADE does.
type fakeLinks struct {
	mu     sync.Mutex
	byTask map[uuid.UUID]domain.IssueLink
	seq    int
	order  map[uuid.UUID]int
}

func newFakeLinks() *fakeLinks {
	return &fakeLinks{byTask: map[uuid.UUID]domain.IssueLink{}, order: map[uuid.UUID]int{}}
}

func (f *fakeLinks) Create(_ context.Context, link domain.IssueLink) (domain.IssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byTask[link.TaskID]; exists {
		return domain.IssueLink{}, fmt.Errorf("task already linked")
	}
	link.ID = uuid.New()
	link.CreatedAt = time.Now()
	f.seq++
	f.order[link.TaskID] = f.seq
	f.byTask[link.TaskID] = link
	return link, nil
}

func (f *fakeLinks) GetByTask(_ context.Context, taskID uuid.UUID) (domain.IssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.byTask[taskID]
	if !ok {
		return domain.IssueLink{}, port.ErrNotFound
	}
	return l, nil
}

func (f *fakeLinks) ListByIssue(_ context.Context, provider domain.IssueProvider, key string) ([]domain.IssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listLocked(provider, key), nil
}

func (f *fakeLinks) listLocked(provider domain.IssueProvider, key string) []domain.IssueLink {
	out := []domain.IssueLink{}
	for _, l := range f.byTask {
		if l.Provider == provider && l.ExternalKey == key {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return f.order[out[i].TaskID] < f.order[out[j].TaskID] })
	return out
}

func (f *fakeLinks) ExistingKeys(_ context.Context, provider domain.IssueProvider, keys []string) (map[string]domain.IssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]domain.IssueLink{}
	for _, k := range keys {
		if links := f.listLocked(provider, k); len(links) > 0 {
			out[k] = links[0]
		}
	}
	return out, nil
}

func (f *fakeLinks) UpdateColumn(_ context.Context, taskID uuid.UUID, column string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.byTask[taskID]
	if !ok {
		return port.ErrNotFound
	}
	l.LastColumn = column
	f.byTask[taskID] = l
	return nil
}

func (f *fakeLinks) MarkIssueClosed(_ context.Context, provider domain.IssueProvider, key string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, l := range f.byTask {
		if l.Provider == provider && l.ExternalKey == key && l.ClosedAt == nil {
			atCopy := at
			l.ClosedAt = &atCopy
			f.byTask[id] = l
		}
	}
	return nil
}

func (f *fakeLinks) cascade(taskID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byTask, taskID)
}

func (f *fakeLinks) keysFor(provider domain.IssueProvider, key string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, l := range f.listLocked(provider, key) {
		out = append(out, l.TaskKey)
	}
	return out
}

// cascadingTasks deletes a task's issue link with it.
type cascadingTasks struct {
	*fakeTasks
	links *fakeLinks
}

func (c cascadingTasks) DeleteTask(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	if err := c.fakeTasks.DeleteTask(ctx, repositoryID, taskID); err != nil {
		return err
	}
	c.links.cascade(taskID)
	return nil
}

type fakeImports struct {
	mu   sync.Mutex
	rows map[uuid.UUID]domain.IssueImport
}

func newFakeImports() *fakeImports { return &fakeImports{rows: map[uuid.UUID]domain.IssueImport{}} }

func (f *fakeImports) Create(_ context.Context, imp domain.IssueImport) (domain.IssueImport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.Provider == imp.Provider && r.ExternalKey == imp.ExternalKey {
			return domain.IssueImport{}, fmt.Errorf("dup: %w", port.ErrIssueImportExists)
		}
	}
	if imp.ID == uuid.Nil {
		imp.ID = uuid.New()
	}
	imp.CreatedAt = time.Now()
	imp.UpdatedAt = imp.CreatedAt
	f.rows[imp.ID] = imp
	return imp, nil
}

func (f *fakeImports) Get(_ context.Context, id uuid.UUID) (domain.IssueImport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.IssueImport{}, port.ErrNotFound
	}
	return r, nil
}

func (f *fakeImports) GetByProviderKey(_ context.Context, provider domain.IssueProvider, key string) (domain.IssueImport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.Provider == provider && r.ExternalKey == key {
			return r, nil
		}
	}
	return domain.IssueImport{}, port.ErrNotFound
}

func (f *fakeImports) GetBySession(_ context.Context, sessionID uuid.UUID) (domain.IssueImport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ConversionSessionID != nil && *r.ConversionSessionID == sessionID {
			return r, nil
		}
	}
	return domain.IssueImport{}, port.ErrNotFound
}

func (f *fakeImports) ListByStatus(_ context.Context, status domain.IssueConversionStatus) ([]domain.IssueImport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.IssueImport{}
	for _, r := range f.rows {
		if r.ConversionStatus == status {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeImports) ClaimConversion(_ context.Context, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.ConversionStatus != domain.IssueConversionPending {
		return false, nil
	}
	r.ConversionStatus = domain.IssueConversionConverting
	f.rows[id] = r
	return true, nil
}

func (f *fakeImports) ResetConverting(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, r := range f.rows {
		if r.ConversionStatus == domain.IssueConversionConverting {
			r.ConversionStatus = domain.IssueConversionPending
			f.rows[id] = r
		}
	}
	return nil
}

func (f *fakeImports) SetConversion(_ context.Context, id uuid.UUID, status domain.IssueConversionStatus, sessionID *uuid.UUID, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return port.ErrNotFound
	}
	r.ConversionStatus = status
	if sessionID != nil {
		sid := *sessionID
		r.ConversionSessionID = &sid
	}
	r.ConversionError = errMsg
	f.rows[id] = r
	return nil
}

func (f *fakeImports) ClearIntakeTask(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rows[id]
	r.IntakeTaskID = nil
	f.rows[id] = r
	return nil
}

func (f *fakeImports) MarkClosed(_ context.Context, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rows[id]
	if r.ClosedAt == nil {
		atCopy := at
		r.ClosedAt = &atCopy
	}
	f.rows[id] = r
	return nil
}

func (f *fakeImports) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}

func (f *fakeImports) byKey(t *testing.T, provider domain.IssueProvider, key string) domain.IssueImport {
	t.Helper()
	imp, err := f.GetByProviderKey(context.Background(), provider, key)
	if err != nil {
		t.Fatalf("import %s %s: %v", provider, key, err)
	}
	return imp
}

type fakeGitHubTokens struct{ token string }

func (f *fakeGitHubTokens) GitHubToken(context.Context) (string, error)  { return f.token, nil }
func (f *fakeGitHubTokens) SetGitHubToken(context.Context, string) error { return nil }
func (f *fakeGitHubTokens) DeleteGitHubToken(context.Context) error      { return nil }

type fakeGitHubIssues struct {
	mu       sync.Mutex
	open     map[string][]domain.ExternalIssue
	byKey    map[string]domain.ExternalIssue
	comments []string
	closed   []string
	getErr   error
}

func newFakeGitHubIssues() *fakeGitHubIssues {
	return &fakeGitHubIssues{open: map[string][]domain.ExternalIssue{}, byKey: map[string]domain.ExternalIssue{}}
}

func (f *fakeGitHubIssues) addIssue(owner, repo string, issue domain.ExternalIssue) {
	f.open[owner+"/"+repo] = append(f.open[owner+"/"+repo], issue)
	f.byKey[issue.Key] = issue
}

func (f *fakeGitHubIssues) ListOpen(_ context.Context, _, owner, repo string, _ int) ([]domain.ExternalIssue, error) {
	return f.open[owner+"/"+repo], nil
}

func (f *fakeGitHubIssues) ListLabelled(_ context.Context, _, owner, repo, label string, _ int) ([]domain.ExternalIssue, error) {
	var out []domain.ExternalIssue
	for _, iss := range f.open[owner+"/"+repo] {
		for _, l := range iss.Labels {
			if l == label {
				out = append(out, iss)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeGitHubIssues) Search(_ context.Context, _, owner, repo, _ string) ([]domain.ExternalIssue, error) {
	return f.open[owner+"/"+repo], nil
}

func (f *fakeGitHubIssues) Get(_ context.Context, _, owner, repo string, number int) (domain.ExternalIssue, error) {
	if f.getErr != nil {
		return domain.ExternalIssue{}, f.getErr
	}
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	iss, ok := f.byKey[key]
	if !ok {
		return domain.ExternalIssue{}, fmt.Errorf("issue %s not found", key)
	}
	return iss, nil
}

func (f *fakeGitHubIssues) AddComment(_ context.Context, _, owner, repo string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, fmt.Sprintf("%s/%s#%d: %s", owner, repo, number, body))
	return nil
}

func (f *fakeGitHubIssues) Close(_ context.Context, _, owner, repo string, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, fmt.Sprintf("%s/%s#%d", owner, repo, number))
	return nil
}

// Comments and Closed snapshot under the lock: write-back runs on its own
// goroutine.
func (f *fakeGitHubIssues) Comments() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.comments...)
}

func (f *fakeGitHubIssues) Closed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.closed...)
}

type fakeJiraSettings struct {
	site, email, token string
}

func (f *fakeJiraSettings) JiraSettings(context.Context) (string, string, string, error) {
	return f.site, f.email, f.token, nil
}
func (f *fakeJiraSettings) SetJiraSettings(_ context.Context, site, email, token string) error {
	f.site, f.email, f.token = site, email, token
	return nil
}
func (f *fakeJiraSettings) DeleteJiraSettings(context.Context) error {
	f.site, f.email, f.token = "", "", ""
	return nil
}

type fakeSyncSettings struct {
	mu       sync.Mutex
	settings domain.IssueSyncSettings
	set      bool
}

func (f *fakeSyncSettings) IssueSyncSettings(context.Context) (domain.IssueSyncSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.set {
		return domain.DefaultIssueSyncSettings(), nil
	}
	return f.settings, nil
}

func (f *fakeSyncSettings) SetIssueSyncSettings(_ context.Context, s domain.IssueSyncSettings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings = s
	f.set = true
	return nil
}

func (f *fakeSyncSettings) use(s domain.IssueSyncSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings = s
	f.set = true
}

type fakeJiraIssueStore struct {
	mu        sync.Mutex
	byKey     map[string]domain.ExternalIssue
	search    []domain.ExternalIssue
	comments  []string
	done      map[string]bool
	myself    port.JiraMyself
	myselfErr error
	projects  []domain.JiraProjectRef
}

func newFakeJiraIssueStore() *fakeJiraIssueStore {
	return &fakeJiraIssueStore{byKey: map[string]domain.ExternalIssue{}, done: map[string]bool{}}
}

type fakeJiraClient struct {
	store *fakeJiraIssueStore
}

// fakeJiraFactory rejects a site the rough shape ValidateSiteURL would, so
// SetJira's ErrInvalidJiraSite wrapping is exercised as against the real
// client.
func fakeJiraFactory(store *fakeJiraIssueStore) port.JiraClientFactory {
	return func(site, _, _ string) (port.JiraClient, error) {
		if !strings.HasPrefix(site, "https://") || !strings.HasSuffix(site, ".atlassian.net") {
			return nil, fmt.Errorf("site must be https://<name>.atlassian.net, got %q", site)
		}
		return &fakeJiraClient{store: store}, nil
	}
}

func (c *fakeJiraClient) Myself(context.Context) (port.JiraMyself, error) {
	if c.store.myselfErr != nil {
		return port.JiraMyself{}, c.store.myselfErr
	}
	return c.store.myself, nil
}

func (c *fakeJiraClient) Projects(context.Context) ([]domain.JiraProjectRef, error) {
	return c.store.projects, nil
}

func (c *fakeJiraClient) Search(_ context.Context, _ string, _ int) ([]domain.ExternalIssue, error) {
	return c.store.search, nil
}

func (c *fakeJiraClient) GetIssue(_ context.Context, key string) (domain.ExternalIssue, error) {
	iss, ok := c.store.byKey[key]
	if !ok {
		return domain.ExternalIssue{}, fmt.Errorf("jira issue %s not found", key)
	}
	return iss, nil
}

func (c *fakeJiraClient) AddComment(_ context.Context, key, text string) error {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	c.store.comments = append(c.store.comments, key+": "+text)
	return nil
}

func (c *fakeJiraClient) TransitionToDone(_ context.Context, key string) (bool, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	c.store.done[key] = true
	return true, nil
}

func (s *fakeJiraIssueStore) Comments() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.comments...)
}

func (s *fakeJiraIssueStore) isDone(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done[key]
}
