// Package issuesync imports GitHub and Jira issues onto the board, has the
// product manager turn each one into board tasks, and writes back to the issue
// as those tasks move.
package issuesync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// BoardTasks is narrowed from *repository.Service so this package need not
// import it.
type BoardTasks interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
	DeleteTask(ctx context.Context, repositoryID, taskID uuid.UUID) error
}

type Repositories interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	List(ctx context.Context) ([]domain.Repository, error)
}

// TaskTypeChecker keeps an import from asking CreateTask for a "bug" type the
// workflow would reject.
type TaskTypeChecker interface {
	TaskTypeExists(ctx context.Context, taskType domain.TaskType) (bool, error)
}

type BoardColumns interface {
	ListColumns(ctx context.Context) ([]domain.BoardColumn, error)
}

// Deps may leave any field zero: each capability then answers "not
// configured" instead of panicking.
type Deps struct {
	Tasks        BoardTasks
	Repositories Repositories
	TaskTypes    TaskTypeChecker
	Columns      BoardColumns
	Links        port.IssueLinkStore
	Imports      port.IssueImportStore
	GitHubTokens port.GitHubTokenStore
	GitHubIssues port.GitHubIssuesClient
	JiraSettings port.JiraSettingsStore
	SyncSettings port.IssueSyncSettingsStore
	JiraFactory  port.JiraClientFactory
}

type Service struct {
	tasks        BoardTasks
	repos        Repositories
	taskTypes    TaskTypeChecker
	columns      BoardColumns
	links        port.IssueLinkStore
	imports      port.IssueImportStore
	githubTokens port.GitHubTokenStore
	githubIssues port.GitHubIssuesClient
	jiraSettings port.JiraSettingsStore
	syncSettings port.IssueSyncSettingsStore
	jiraFactory  port.JiraClientFactory
	now          func() time.Time
	// writeBackTimeout bounds every call to the issue tracker, so a wedged API
	// never leaks a goroutine past shutdown.
	writeBackTimeout time.Duration

	conv     atomic.Pointer[ConversionDeps]
	convWake chan struct{}
	// convMu serialises finishing a conversion: the worker at the end of the
	// product manager's turn and a task the same session opens later must not
	// both retire the intake task and announce the result.
	convMu sync.Mutex
}

func NewService(d Deps) *Service {
	return &Service{
		tasks:            d.Tasks,
		repos:            d.Repositories,
		taskTypes:        d.TaskTypes,
		columns:          d.Columns,
		links:            d.Links,
		imports:          d.Imports,
		githubTokens:     d.GitHubTokens,
		githubIssues:     d.GitHubIssues,
		jiraSettings:     d.JiraSettings,
		syncSettings:     d.SyncSettings,
		jiraFactory:      d.JiraFactory,
		now:              time.Now,
		writeBackTimeout: 30 * time.Second,
		convWake:         make(chan struct{}, 1),
	}
}

// HandleGitHubIssuesEvent answers the "issues" webhook: an issue that arrives
// carrying the configured label ("opened") or gets it ("labeled") is imported
// the same way the poller would on its next pass.
func (s *Service) HandleGitHubIssuesEvent(ctx context.Context, repositoryID uuid.UUID, action, key string, labels []string) (bool, string) {
	if action != "labeled" && action != "opened" {
		return false, "action not tracked"
	}
	if s.syncSettings == nil {
		return false, "issue sync not configured"
	}
	settings, err := s.syncSettings.IssueSyncSettings(ctx)
	if err != nil {
		return false, "reading issue sync settings failed"
	}
	if !settings.GitHubAutoImport {
		return false, "github auto-import is off"
	}
	if !hasLabelNamed(labels, settings.Label) {
		return false, "label not present"
	}
	if _, err := s.Import(ctx, domain.IssueProviderGitHub, key, repositoryID, string(domain.IssueProviderGitHub), ""); err != nil {
		var already *AlreadyImportedError
		if errors.As(err, &already) {
			return false, "already imported"
		}
		// The reason lands in GitHub's delivery log, which the repository's
		// admins can read; the detail stays in ours.
		log.Warn().Err(err).Str("issue", key).Msg("issue sync: webhook import failed")
		return false, "import failed"
	}
	return true, "imported"
}

func hasLabelNamed(labels []string, name string) bool {
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// IssueForTask returns ErrNoLink for a task that did not come from an issue.
// The import is zero when its row is gone.
func (s *Service) IssueForTask(ctx context.Context, taskID uuid.UUID) (domain.IssueLink, domain.IssueImport, error) {
	if s.links == nil {
		return domain.IssueLink{}, domain.IssueImport{}, ErrNoLink
	}
	link, err := s.links.GetByTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return domain.IssueLink{}, domain.IssueImport{}, ErrNoLink
		}
		return domain.IssueLink{}, domain.IssueImport{}, fmt.Errorf("get issue link: %w", err)
	}
	if s.imports == nil {
		return link, domain.IssueImport{}, nil
	}
	imp, err := s.imports.GetByProviderKey(ctx, link.Provider, link.ExternalKey)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return domain.IssueLink{}, domain.IssueImport{}, fmt.Errorf("get issue import: %w", err)
	}
	return link, imp, nil
}

func (s *Service) githubToken(ctx context.Context) (string, error) {
	if s.githubTokens == nil {
		return "", nil
	}
	return s.githubTokens.GitHubToken(ctx)
}

// jiraClient answers (nil, false, nil) for "not connected".
func (s *Service) jiraClient(ctx context.Context) (port.JiraClient, bool, error) {
	if s.jiraSettings == nil || s.jiraFactory == nil {
		return nil, false, nil
	}
	site, email, token, err := s.jiraSettings.JiraSettings(ctx)
	if err != nil {
		return nil, false, err
	}
	if site == "" || token == "" {
		return nil, false, nil
	}
	client, err := s.jiraFactory(site, email, token)
	if err != nil {
		return nil, false, fmt.Errorf("build jira client: %w", err)
	}
	return client, true, nil
}

// Search needs repositoryID for GitHub and project for Jira.
func (s *Service) Search(ctx context.Context, provider domain.IssueProvider, repositoryID *uuid.UUID, project, q string) ([]domain.ExternalIssue, error) {
	var issues []domain.ExternalIssue
	switch provider {
	case domain.IssueProviderGitHub:
		if repositoryID == nil {
			return nil, fmt.Errorf("%w: repository_id required", ErrInvalidIssue)
		}
		if s.repos == nil || s.githubIssues == nil {
			return nil, ErrSourceNotConfigured
		}
		repo, err := s.repos.Get(ctx, *repositoryID)
		if err != nil {
			return nil, fmt.Errorf("get repository: %w", err)
		}
		owner, name, ok := githubOwnerRepo(repo.RemoteURL)
		if !ok {
			return nil, fmt.Errorf("%w: repository has no GitHub remote", ErrSourceNotConfigured)
		}
		token, err := s.githubToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("github token: %w", err)
		}
		if token == "" {
			return nil, fmt.Errorf("%w: github is not connected", ErrSourceNotConfigured)
		}
		if strings.TrimSpace(q) == "" {
			issues, err = s.githubIssues.ListOpen(ctx, token, owner, name, 50)
		} else {
			issues, err = s.githubIssues.Search(ctx, token, owner, name, q)
		}
		if err != nil {
			return nil, fmt.Errorf("github search: %w: %w", ErrUpstream, err)
		}
	case domain.IssueProviderJira:
		if strings.TrimSpace(project) == "" {
			return nil, fmt.Errorf("%w: project required", ErrInvalidIssue)
		}
		client, connected, err := s.jiraClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("jira settings: %w", err)
		}
		if !connected {
			return nil, fmt.Errorf("%w: jira is not connected", ErrSourceNotConfigured)
		}
		jql := "project = " + quoteJQL(project)
		if strings.TrimSpace(q) != "" {
			jql += " AND text ~ " + quoteJQL(q)
		}
		jql += " ORDER BY updated DESC"
		issues, err = client.Search(ctx, jql, 50)
		if err != nil {
			return nil, fmt.Errorf("jira search: %w: %w", ErrUpstream, err)
		}
	default:
		return nil, fmt.Errorf("%w: unknown provider %q", ErrInvalidIssue, provider)
	}
	if err := s.markImported(ctx, provider, issues); err != nil {
		log.Warn().Err(err).Msg("issuesync: marking imported search results failed")
	}
	return issues, nil
}

func (s *Service) markImported(ctx context.Context, provider domain.IssueProvider, issues []domain.ExternalIssue) error {
	if s.links == nil || len(issues) == 0 {
		return nil
	}
	keys := make([]string, len(issues))
	for i, iss := range issues {
		keys[i] = iss.Key
	}
	existing, err := s.links.ExistingKeys(ctx, provider, keys)
	if err != nil {
		return err
	}
	for i, iss := range issues {
		if link, ok := existing[iss.Key]; ok {
			issues[i].ImportedTask = &domain.ImportedTaskRef{
				ID: link.TaskID, Key: link.TaskKey, RepositoryID: link.RepositoryID,
			}
		}
	}
	return nil
}

// githubOwnerRepo duplicates the github adapter's ParseOwnerRepo rather than
// crossing the layer boundary: RemoteURL is always a plain https or ssh
// github.com remote this process wrote itself.
func githubOwnerRepo(remoteURL string) (owner, repo string, ok bool) {
	s := strings.TrimSpace(remoteURL)
	switch {
	case strings.HasPrefix(s, "git@github.com:"):
		s = strings.TrimPrefix(s, "git@github.com:")
	case strings.Contains(s, "github.com/"):
		s = s[strings.Index(s, "github.com/")+len("github.com/"):]
	default:
		return "", "", false
	}
	s = strings.TrimSuffix(s, ".git")
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
