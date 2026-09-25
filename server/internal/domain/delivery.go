package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// DeliveryMode is WHAT the merge of a signed-off task sets in motion for a
// component. It is stated per component rather than inferred from which
// workflows or targets happen to exist: inference is what let a green deploy
// watch leave cards in done forever and let tasks slide into released with
// nothing deployed.
type DeliveryMode string

const (
	// DeliveryOnMerge: the merge itself deploys (an Actions workflow on push to
	// the default branch, Vercel's git integration). The release engineer only
	// watches, verifies and, if needed, rolls back.
	DeliveryOnMerge DeliveryMode = "on_merge"
	// DeliveryDispatch: a deploy workflow is dispatched at the release tag
	// after the merge.
	DeliveryDispatch DeliveryMode = "dispatch"
	// DeliveryBatch: merges accumulate in a draft release; a human cuts it.
	DeliveryBatch DeliveryMode = "batch"
	// DeliveryNone: nothing deploys (a library, docs); the merge is the release.
	DeliveryNone DeliveryMode = "none"
)

func (m DeliveryMode) Valid() bool {
	switch m {
	case DeliveryOnMerge, DeliveryDispatch, DeliveryBatch, DeliveryNone:
		return true
	}
	return false
}

// DeliveryExecutor is WHO carries the deploy out.
type DeliveryExecutor string

const (
	ExecutorGitHubActions DeliveryExecutor = "github_actions"
	ExecutorVercel        DeliveryExecutor = "vercel"
	ExecutorLocal         DeliveryExecutor = "local"
	ExecutorStore         DeliveryExecutor = "store"
)

func (e DeliveryExecutor) Valid() bool {
	switch e {
	case ExecutorGitHubActions, ExecutorVercel, ExecutorLocal, ExecutorStore:
		return true
	}
	return false
}

const (
	DefaultSoakMinutes = 10
	MaxSoakMinutes     = 120
	MaxSmokeChecks     = 20
)

// SmokeCheck is one read-only request sent to production after a deploy. Only
// GET and HEAD exist: a smoke check that could write would be a test against
// production, which no agent may run.
type SmokeCheck struct {
	Method string `json:"method,omitempty"`
	// Path is relative to the environment's URL ("/api/health"), or an
	// absolute http(s) URL.
	Path string `json:"path"`
	// ExpectStatus 0 means any 2xx/3xx.
	ExpectStatus int `json:"expect_status,omitempty"`
	// Contains, when set, must appear in the response body.
	Contains string `json:"contains,omitempty"`
}

func (c SmokeCheck) Normalized() SmokeCheck {
	c.Method = strings.ToUpper(strings.TrimSpace(c.Method))
	if c.Method == "" {
		c.Method = "GET"
	}
	c.Path = strings.TrimSpace(c.Path)
	return c
}

// Passes reports whether a response status satisfies the check's expectation.
func (c SmokeCheck) Passes(status int) bool {
	if c.ExpectStatus != 0 {
		return status == c.ExpectStatus
	}
	return status >= 200 && status < 400
}

// DeliveryVerify is what "healthy after the deploy" means for a component.
type DeliveryVerify struct {
	// SoakMinutes is how long production is watched after the deploy settles
	// before a verdict is asked for.
	SoakMinutes int `json:"soak_minutes,omitempty"`
	// MaxNewErrors is how many runtime error groups that first appeared after
	// the deploy are tolerated before the soak stops early.
	MaxNewErrors int          `json:"max_new_errors,omitempty"`
	Smoke        []SmokeCheck `json:"smoke,omitempty"`
}

// ComponentDelivery is a component's delivery profile.
type ComponentDelivery struct {
	Mode     DeliveryMode     `json:"mode"`
	Executor DeliveryExecutor `json:"executor,omitempty"`
	// Workflow is the deploy workflow's file basename ("deploy.yml"): the one
	// dispatched in dispatch mode, and the one whose run is watched on merge.
	Workflow string `json:"workflow,omitempty"`
	// TagPattern names a batch release's tag; "{version}" is substituted.
	TagPattern string `json:"tag_pattern,omitempty"`
	// LocalCommand runs a batch release on this machine; "{version}" is
	// substituted.
	LocalCommand string         `json:"local_command,omitempty"`
	Verify       DeliveryVerify `json:"verify"`
	// AutoRollback lets the release engineer roll a bad release back on its
	// own; off, it writes the proposal and a human confirms.
	AutoRollback bool `json:"auto_rollback"`
}

var ErrInvalidDelivery = errors.New("invalid delivery profile")

// Normalized fills the defaults a stored profile may omit.
func (d ComponentDelivery) Normalized() ComponentDelivery {
	d.Workflow = strings.TrimSpace(d.Workflow)
	d.TagPattern = strings.TrimSpace(d.TagPattern)
	d.LocalCommand = strings.TrimSpace(d.LocalCommand)
	if d.Verify.SoakMinutes <= 0 {
		d.Verify.SoakMinutes = DefaultSoakMinutes
	}
	if d.Verify.MaxNewErrors < 0 {
		d.Verify.MaxNewErrors = 0
	}
	smoke := make([]SmokeCheck, 0, len(d.Verify.Smoke))
	for _, c := range d.Verify.Smoke {
		smoke = append(smoke, c.Normalized())
	}
	d.Verify.Smoke = smoke
	if d.Mode == DeliveryBatch && d.TagPattern == "" && d.Executor == ExecutorGitHubActions {
		d.TagPattern = "v{version}"
	}
	return d
}

func (d ComponentDelivery) Validate() error {
	if !d.Mode.Valid() {
		return fmt.Errorf("%w: mode %q is not one of on_merge, dispatch, batch, none", ErrInvalidDelivery, d.Mode)
	}
	if d.Mode == DeliveryNone {
		return nil
	}
	if !d.Executor.Valid() {
		return fmt.Errorf("%w: executor %q is not one of github_actions, vercel, local, store", ErrInvalidDelivery, d.Executor)
	}
	if d.Mode == DeliveryDispatch {
		if d.Executor != ExecutorGitHubActions {
			return fmt.Errorf("%w: dispatch mode dispatches a GitHub Actions workflow; executor must be github_actions", ErrInvalidDelivery)
		}
		if strings.TrimSpace(d.Workflow) == "" {
			return fmt.Errorf("%w: dispatch mode needs the deploy workflow's file name", ErrInvalidDelivery)
		}
	}
	if d.Mode == DeliveryOnMerge && d.Executor != ExecutorGitHubActions && d.Executor != ExecutorVercel {
		return fmt.Errorf("%w: on_merge deploys through GitHub Actions or Vercel", ErrInvalidDelivery)
	}
	if d.Mode == DeliveryBatch && d.Executor == ExecutorLocal && strings.TrimSpace(d.LocalCommand) == "" {
		return fmt.Errorf("%w: a local batch release needs the command that builds and publishes it", ErrInvalidDelivery)
	}
	if strings.Contains(d.Workflow, "/") {
		return fmt.Errorf("%w: workflow is the file's base name (deploy.yml), not a path", ErrInvalidDelivery)
	}
	if d.Verify.SoakMinutes > MaxSoakMinutes {
		return fmt.Errorf("%w: soak_minutes is at most %d", ErrInvalidDelivery, MaxSoakMinutes)
	}
	if len(d.Verify.Smoke) > MaxSmokeChecks {
		return fmt.Errorf("%w: at most %d smoke checks", ErrInvalidDelivery, MaxSmokeChecks)
	}
	for i, c := range d.Verify.Smoke {
		c = c.Normalized()
		if c.Method != "GET" && c.Method != "HEAD" {
			return fmt.Errorf("%w: smoke check %d uses %s — only GET and HEAD may be sent to production", ErrInvalidDelivery, i+1, c.Method)
		}
		if c.Path == "" {
			return fmt.Errorf("%w: smoke check %d has no path", ErrInvalidDelivery, i+1)
		}
		if strings.Contains(c.Path, "://") {
			u, err := url.Parse(c.Path)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("%w: smoke check %d is not an http(s) URL", ErrInvalidDelivery, i+1)
			}
		} else if !strings.HasPrefix(c.Path, "/") {
			return fmt.Errorf("%w: smoke check %d path must start with / (or be an absolute URL)", ErrInvalidDelivery, i+1)
		}
		if c.ExpectStatus != 0 && (c.ExpectStatus < 100 || c.ExpectStatus > 599) {
			return fmt.Errorf("%w: smoke check %d expects an impossible status %d", ErrInvalidDelivery, i+1, c.ExpectStatus)
		}
	}
	return nil
}

// DeliveryConfirmed reports whether a component's profile may drive a release
// without asking: a human override, or a detection confident enough to be
// auto-confirmed. A medium-confidence guess would otherwise start dispatching
// production deploys nobody asked for.
func DeliveryConfirmed(f Fact[ComponentDelivery]) (ComponentDelivery, bool) {
	if f.Override != nil {
		return f.Override.Normalized(), true
	}
	if f.Detected != nil && (f.Confidence == ConfidenceExact || f.Confidence == ConfidenceHigh) {
		return f.Detected.Normalized(), true
	}
	return ComponentDelivery{}, false
}
