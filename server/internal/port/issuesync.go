package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrJiraUnauthorized is what a JiraClient wraps when Jira refused the
// credentials (401/403), as opposed to being unreachable.
var ErrJiraUnauthorized = errors.New("jira refused the credentials")

// ErrIssueImportExists is IssueImportStore.Create losing the unique-index race
// on (provider, external_key).
var ErrIssueImportExists = errors.New("issue already imported")

type IssueLinkStore interface {
	Create(ctx context.Context, link domain.IssueLink) (domain.IssueLink, error)
	// GetByTask returns ErrNotFound when the task has no link.
	GetByTask(ctx context.Context, taskID uuid.UUID) (domain.IssueLink, error)
	// ListByIssue returns every task linked to one issue, oldest first.
	ListByIssue(ctx context.Context, provider domain.IssueProvider, key string) ([]domain.IssueLink, error)
	// ExistingKeys maps each already-linked key to its oldest link, in one
	// query for a whole page of search results.
	ExistingKeys(ctx context.Context, provider domain.IssueProvider, keys []string) (map[string]domain.IssueLink, error)
	UpdateColumn(ctx context.Context, taskID uuid.UUID, column string) error
	// MarkIssueClosed stamps every link of the issue, once.
	MarkIssueClosed(ctx context.Context, provider domain.IssueProvider, key string, at time.Time) error
}

type IssueImportStore interface {
	// Create returns ErrIssueImportExists when (provider, key) is taken.
	Create(ctx context.Context, imp domain.IssueImport) (domain.IssueImport, error)
	Get(ctx context.Context, id uuid.UUID) (domain.IssueImport, error)
	// GetByProviderKey and GetBySession return ErrNotFound when absent.
	GetByProviderKey(ctx context.Context, provider domain.IssueProvider, key string) (domain.IssueImport, error)
	GetBySession(ctx context.Context, sessionID uuid.UUID) (domain.IssueImport, error)
	ListByStatus(ctx context.Context, status domain.IssueConversionStatus) ([]domain.IssueImport, error)
	// ClaimConversion moves a pending import to converting; false when another
	// claim got there first or the import is no longer pending.
	ClaimConversion(ctx context.Context, id uuid.UUID) (bool, error)
	// ResetConverting puts every converting import back to pending. Only
	// called at boot, when no conversion can be running.
	ResetConverting(ctx context.Context) error
	SetConversion(ctx context.Context, id uuid.UUID, status domain.IssueConversionStatus, sessionID *uuid.UUID, errMsg string) error
	ClearIntakeTask(ctx context.Context, id uuid.UUID) error
	MarkClosed(ctx context.Context, id uuid.UUID, at time.Time) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// JiraSettingsStore keeps the API token encrypted at rest, like github_token.
type JiraSettingsStore interface {
	// JiraSettings returns ("", "", "", nil) when nothing is stored.
	JiraSettings(ctx context.Context) (siteURL, email, apiToken string, err error)
	SetJiraSettings(ctx context.Context, siteURL, email, apiToken string) error
	DeleteJiraSettings(ctx context.Context) error
}

type IssueSyncSettingsStore interface {
	// IssueSyncSettings returns domain.DefaultIssueSyncSettings() when nothing
	// has been saved yet.
	IssueSyncSettings(ctx context.Context) (domain.IssueSyncSettings, error)
	SetIssueSyncSettings(ctx context.Context, settings domain.IssueSyncSettings) error
}

// GitHubIssuesClient is kept apart from the PR/webhook clients because issue
// import is its only caller. Every list excludes pull requests.
type GitHubIssuesClient interface {
	ListOpen(ctx context.Context, token, owner, repo string, perPage int) ([]domain.ExternalIssue, error)
	ListLabelled(ctx context.Context, token, owner, repo, label string, perPage int) ([]domain.ExternalIssue, error)
	Search(ctx context.Context, token, owner, repo, q string) ([]domain.ExternalIssue, error)
	Get(ctx context.Context, token, owner, repo string, number int) (domain.ExternalIssue, error)
	AddComment(ctx context.Context, token, owner, repo string, number int, body string) error
	// Close sets state=closed, state_reason=completed.
	Close(ctx context.Context, token, owner, repo string, number int) error
}

type JiraMyself struct {
	AccountID   string
	DisplayName string
	Email       string
}

type JiraClient interface {
	Myself(ctx context.Context) (JiraMyself, error)
	Projects(ctx context.Context) ([]domain.JiraProjectRef, error)
	Search(ctx context.Context, jql string, max int) ([]domain.ExternalIssue, error)
	GetIssue(ctx context.Context, key string) (domain.ExternalIssue, error)
	AddComment(ctx context.Context, key, text string) error
	// TransitionToDone returns (false, nil) when the issue has no done-category
	// transition to make.
	TransitionToDone(ctx context.Context, key string) (bool, error)
}

// JiraClientFactory rather than one client: the credentials can change from
// Settings at any time and every call needs the current ones.
type JiraClientFactory func(site, email, apiToken string) (JiraClient, error)
