package domain

import (
	"time"

	"github.com/google/uuid"
)

type IssueProvider string

const (
	IssueProviderGitHub IssueProvider = "github"
	IssueProviderJira   IssueProvider = "jira"
)

// IssueLink ties one board task to the GitHub or Jira issue it came from. An
// issue the product manager split into several tasks has one link per task;
// the link is also the write-back cursor (LastColumn, ClosedAt).
type IssueLink struct {
	ID           uuid.UUID     `json:"id"`
	TaskID       uuid.UUID     `json:"task_id"`
	RepositoryID uuid.UUID     `json:"repository_id"`
	Provider     IssueProvider `json:"provider"`
	ExternalKey  string        `json:"key"`
	URL          string        `json:"url"`
	Title        string        `json:"title"`
	// TaskKey is denormalized at link time (task keys never change) so search
	// can mark a page of already-imported issues in one query.
	TaskKey string `json:"-"`
	// ImportedBy is "" for an automatic import (poller or webhook).
	ImportedBy string     `json:"imported_by"`
	LastColumn string     `json:"-"`
	ClosedAt   *time.Time `json:"closed_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// IssueConversionStatus tracks the product manager turning an imported issue
// into board tasks. "" means no conversion was asked for: the imported task is
// the issue's only task.
type IssueConversionStatus string

const (
	IssueConversionNone       IssueConversionStatus = ""
	IssueConversionPending    IssueConversionStatus = "pending"
	IssueConversionConverting IssueConversionStatus = "converting"
	IssueConversionConverted  IssueConversionStatus = "converted"
	IssueConversionNeedsInput IssueConversionStatus = "needs_input"
	IssueConversionFailed     IssueConversionStatus = "failed"
	IssueConversionSkipped    IssueConversionStatus = "skipped"
)

// IssueImport is the issue-level record of an import, one per (provider,
// key). It outlives the intake task, which the conversion replaces with the
// product manager's tasks, so it is what keeps an issue from being imported
// twice.
type IssueImport struct {
	ID           uuid.UUID     `json:"id"`
	Provider     IssueProvider `json:"provider"`
	ExternalKey  string        `json:"key"`
	RepositoryID uuid.UUID     `json:"repository_id"`
	URL          string        `json:"url"`
	Title        string        `json:"title"`
	ImportedBy   string        `json:"imported_by"`
	// IntakeTaskID is the task the import opened straight from the issue; nil
	// once the conversion replaced it.
	IntakeTaskID        *uuid.UUID            `json:"intake_task_id"`
	ConversionStatus    IssueConversionStatus `json:"conversion_status"`
	ConversionSessionID *uuid.UUID            `json:"conversion_session_id"`
	ConversionError     string                `json:"conversion_error,omitempty"`
	ClosedAt            *time.Time            `json:"closed_at"`
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
}

type ImportedTaskRef struct {
	ID           uuid.UUID `json:"id"`
	Key          string    `json:"key"`
	RepositoryID uuid.UUID `json:"repository_id"`
}

// ExternalIssue is an issue before it becomes a task. Body is untrusted text
// from outside this process: it only ever lands in a task description.
type ExternalIssue struct {
	Provider IssueProvider `json:"provider"`
	Key      string        `json:"key"`
	Title    string        `json:"title"`
	Body     string        `json:"-"`
	URL      string        `json:"url"`
	State    string        `json:"state"`
	Labels   []string      `json:"labels"`
	// IssueType and Priority are Jira's raw names; GitHub has neither and
	// reads "bug" off Labels instead.
	IssueType    string           `json:"-"`
	Priority     string           `json:"-"`
	UpdatedAt    time.Time        `json:"updated_at"`
	ImportedTask *ImportedTaskRef `json:"imported_task"`
}

type JiraStatus struct {
	Connected   bool   `json:"connected"`
	SiteURL     string `json:"site_url"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
}

type JiraProjectRef struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type JiraProjectMapping struct {
	ProjectKey   string    `json:"project_key"`
	RepositoryID uuid.UUID `json:"repository_id"`
}

type IssueSyncSettings struct {
	Label            string               `json:"label"`
	GitHubAutoImport bool                 `json:"github_auto_import"`
	JiraAutoImport   bool                 `json:"jira_auto_import"`
	WriteBack        bool                 `json:"write_back"`
	ConvertWithPM    bool                 `json:"convert_with_pm"`
	JiraProjects     []JiraProjectMapping `json:"jira_projects"`
}

func DefaultIssueSyncSettings() IssueSyncSettings {
	return IssueSyncSettings{
		Label:         "tasktrooper",
		WriteBack:     true,
		ConvertWithPM: true,
		JiraProjects:  []JiraProjectMapping{},
	}
}
