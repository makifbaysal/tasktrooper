package issuesync

import (
	"errors"

	"github.com/google/uuid"
)

// ErrSourceNotConfigured: GitHub or Jira is not connected, or the repository
// has no GitHub remote.
var ErrSourceNotConfigured = errors.New("issue source not configured")

// ErrInvalidIssue: the key does not parse, or a GitHub key names a different
// repository than the one it is imported into.
var ErrInvalidIssue = errors.New("invalid issue")

var ErrInvalidJiraSite = errors.New("invalid jira site")

var ErrJiraAuthFailed = errors.New("jira authentication failed")

// ErrUpstream wraps a failure GitHub or Jira returned, so a handler can tell
// "we refused this" (400) from "they refused this" (502).
var ErrUpstream = errors.New("issue tracker request failed")

var ErrInvalidSettings = errors.New("invalid issue sync settings")

var ErrNoLink = errors.New("task has no issue link")

var ErrNoProductManager = errors.New("no enabled agent holds the product manager role")

var ErrConversionUnavailable = errors.New("issue conversion is not available")

// ErrConversionNotRetryable: only a failed or skipped conversion whose
// imported task still exists can run again.
var ErrConversionNotRetryable = errors.New("this issue's conversion cannot be retried")

// AlreadyImportedError carries what the 409 body needs without a second
// lookup. TaskID is uuid.Nil when every task the issue became is being
// replaced right now.
type AlreadyImportedError struct {
	TaskID       uuid.UUID
	TaskKey      string
	RepositoryID uuid.UUID
}

func (e *AlreadyImportedError) Error() string {
	if e.TaskKey == "" {
		return "issue already imported"
	}
	return "issue already imported as " + e.TaskKey
}
