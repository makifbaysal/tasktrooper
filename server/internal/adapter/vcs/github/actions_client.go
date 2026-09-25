package github

import (
	"context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ActionsAPI struct {
	token   TokenSource
	baseURL string
}

type TokenSource func(ctx context.Context) (string, error)

func NewActionsAPI(token string) *ActionsAPI {
	return &ActionsAPI{token: func(context.Context) (string, error) { return token, nil }}
}

func NewActionsAPIFor(source TokenSource) *ActionsAPI {
	return &ActionsAPI{token: source}
}

func (a *ActionsAPI) SetBaseURL(u string) { a.baseURL = u }

func (a *ActionsAPI) resolve(ctx context.Context) (string, error) {
	if a == nil || a.token == nil {
		return "", nil
	}
	return a.token(ctx)
}

func (a *ActionsAPI) ListWorkflowRuns(ctx context.Context, owner, repo, workflowFile, branch string) ([]port.ActionsRun, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return nil, err
	}
	runs, err := listWorkflowRunsByFileBranchAt(ctx, a.baseURL, token, owner, repo, workflowFile, branch)
	if err != nil {
		return nil, err
	}
	return mapActionsRuns(runs), nil
}

func (a *ActionsAPI) DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string) error {
	token, err := a.resolve(ctx)
	if err != nil {
		return err
	}
	return dispatchWorkflowAt(ctx, a.baseURL, token, owner, repo, workflowFile, ref)
}

func (a *ActionsAPI) CreateTag(ctx context.Context, owner, repo, tag, sha string) error {
	token, err := a.resolve(ctx)
	if err != nil {
		return err
	}
	return createTagAt(ctx, a.baseURL, token, owner, repo, tag, sha)
}

func (a *ActionsAPI) ListRunsForCommit(ctx context.Context, owner, repo, sha string) ([]port.ActionsRun, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return nil, err
	}
	runs, err := listRunsByHeadSHAAt(ctx, a.baseURL, token, owner, repo, sha)
	if err != nil {
		return nil, err
	}
	return mapActionsRuns(runs), nil
}

func (a *ActionsAPI) ListRunJobs(ctx context.Context, owner, repo string, runID int64) ([]port.ActionsJob, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return nil, err
	}
	jobs, err := listRunJobsAt(ctx, a.baseURL, token, owner, repo, runID)
	if err != nil {
		return nil, err
	}
	out := make([]port.ActionsJob, len(jobs))
	for i, j := range jobs {
		out[i] = port.ActionsJob{
			ID:          j.ID,
			Name:        j.Name,
			Status:      j.Status,
			Conclusion:  j.Conclusion,
			HTMLURL:     j.HTMLURL,
			StartedAt:   j.StartedAt,
			CompletedAt: j.CompletedAt,
		}
	}
	return out, nil
}

func (a *ActionsAPI) JobLogs(ctx context.Context, owner, repo string, jobID int64) (string, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return "", err
	}
	return getJobLogsAt(ctx, a.baseURL, token, owner, repo, jobID)
}

func (a *ActionsAPI) CommitDeployStatus(ctx context.Context, owner, repo, sha string) (port.CommitDeploySignal, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return port.CommitDeploySignal{}, err
	}
	combined, err := getCombinedStatusAt(ctx, a.baseURL, token, owner, repo, sha)
	if err != nil {
		return port.CommitDeploySignal{}, err
	}
	if combined.HasSignal() {
		if deploy := deployProviderStatuses(combined.Statuses); len(deploy) > 0 {
			signal := port.CommitDeploySignal{
				Kind:     domain.DeploySignalCommitStatus,
				State:    aggregateStatusState(deploy),
				Contexts: statusContexts(deploy),
			}
			signal.Description = deploy[0].Description
			signal.URL = deploy[0].TargetURL
			return signal, nil
		}
		// Every status was a CI context (codecov, ci/*, build, test, lint, …),
		// none of it a deploy provider — fall through to deployments rather
		// than reading a red test run as a failed deploy.
	}

	deployments, err := listDeploymentsForSHAAt(ctx, a.baseURL, token, owner, repo, sha)
	if err != nil {
		return port.CommitDeploySignal{}, err
	}
	for _, d := range deployments {
		statuses, serr := listDeploymentStatusesAt(ctx, a.baseURL, token, owner, repo, d.ID)
		if serr != nil {
			return port.CommitDeploySignal{}, serr
		}
		if len(statuses) == 0 {
			continue
		}
		latest := statuses[0]
		return port.CommitDeploySignal{
			Kind:        domain.DeploySignalDeploymentState,
			State:       normalizeStatusState(latest.State),
			Environment: d.Environment,
			Description: latest.Description,
			URL:         firstNonEmptyString(latest.EnvironmentURL, latest.LogURL, latest.TargetURL),
		}, nil
	}
	return port.CommitDeploySignal{}, nil
}

func mapActionsRuns(runs []WorkflowRun) []port.ActionsRun {
	out := make([]port.ActionsRun, len(runs))
	for i, r := range runs {
		out[i] = port.ActionsRun{
			ID:           r.ID,
			RunNumber:    r.RunNumber,
			HeadSHA:      r.HeadSHA,
			HeadBranch:   r.HeadBranch,
			Status:       r.Status,
			Conclusion:   r.Conclusion,
			HTMLURL:      r.HTMLURL,
			Event:        r.Event,
			RunStartedAt: r.RunStartedAt,
			UpdatedAt:    r.UpdatedAt,
		}
	}
	return out
}

// deployProviderTokens are substrings that mark a commit-status context as a
// deploy provider rather than a CI check. "deploy" alone also matches
// "deployment", so it is not listed separately.
var deployProviderTokens = []string{"vercel", "netlify", "render", "cloudflare", "heroku", "fly", "deploy"}

// deployProviderStatuses keeps only the statuses that look like a deploy
// provider wrote them — a red codecov, ci/*, build, test or lint context must
// never read as a deploy failure.
func deployProviderStatuses(statuses []CommitStatus) []CommitStatus {
	out := make([]CommitStatus, 0, len(statuses))
	for _, s := range statuses {
		if looksLikeDeployContext(s.Context) {
			out = append(out, s)
		}
	}
	return out
}

func looksLikeDeployContext(context string) bool {
	lower := strings.ToLower(strings.TrimSpace(context))
	if lower == "" {
		return false
	}
	for _, token := range deployProviderTokens {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

// aggregateStatusState mirrors GitHub's own combined-status rule (any
// failure wins, then any pending, else success) but computed over the
// deploy-provider subset only, since combined.State was computed over every
// status including the CI ones this filter just dropped.
func aggregateStatusState(statuses []CommitStatus) string {
	state := "success"
	for _, s := range statuses {
		switch normalizeStatusState(s.State) {
		case "failure":
			return "failure"
		case "pending":
			state = "pending"
		}
	}
	return state
}

func statusContexts(statuses []CommitStatus) []string {
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		if s.Context != "" {
			out = append(out, s.Context)
		}
	}
	return out
}

func normalizeStatusState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "success":
		return "success"
	case "failure", "error":
		return "failure"
	default:
		return "pending"
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
