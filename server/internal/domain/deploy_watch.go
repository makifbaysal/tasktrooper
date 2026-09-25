package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The deploy watch is keyed on ONE thing — board_tasks.merge_commit_sha, the
// squash commit merge_task_pull_request recorded — never the branch (a default
// branch carries everyone's merges) and never "the latest deploy". It is one
// task-scoped abstraction over three signals, resolved in order: an Actions run
// on the merge commit, a GitHub commit status (Vercel push-to-deploy), or a
// GitHub Deployment state.

// DeployWatchState is the answer to "did this task's commit reach production,
// and did it work".
type DeployWatchState string

const (
	// DeployWatchPending is a deploy that has started and not finished; it must
	// never be waited on inside a tool call — the agent parks
	// (ResourceDeployWatch) and is re-dispatched when it settles.
	DeployWatchPending DeployWatchState = "pending"
	DeployWatchSuccess DeployWatchState = "success"
	DeployWatchFailure DeployWatchState = "failure"
	// DeployWatchNoSignal means nothing reports anything about deploying the
	// commit. NOT a failure: a repo that deploys on a manual schedule (or not
	// at all) lands here and there is nothing to roll back.
	DeployWatchNoSignal DeployWatchState = "no_signal"
	// DeployWatchUnknown is the watch itself failing (no merge commit, GitHub
	// unreachable); every other state authorises an action, this one none.
	DeployWatchUnknown DeployWatchState = "unknown"
)

// Settled reports whether the state is one the agent can act on; only pending
// is unsettled — no_signal and unknown are answers, not waits.
func (s DeployWatchState) Settled() bool { return s != DeployWatchPending }

// Deploy watch signal kinds, so an agent can tell "the deploy job went green"
// from "Vercel said success".
const (
	DeploySignalActionsRun      = "actions_run"
	DeploySignalCommitStatus    = "commit_status"
	DeploySignalDeploymentState = "deployment_status"
	DeploySignalNone            = "none"
)

// DeployWatchJob is one job inside the Actions run that carried the deploy;
// get_deploy_logs is pointed at it when the deploy failed.
type DeployWatchJob struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url,omitempty"`
}

// DeployWatchStatus is the whole answer for one task.
type DeployWatchStatus struct {
	TaskID       uuid.UUID        `json:"task_id"`
	TaskKey      string           `json:"task_key,omitempty"`
	RepositoryID uuid.UUID        `json:"repository_id"`
	Env          string           `json:"env"`
	MergeSHA     string           `json:"merge_commit_sha"`
	State        DeployWatchState `json:"state"`
	// Signal names which of the three sources produced State.
	Signal string `json:"signal"`
	// Detail is one sentence a human can read on the card.
	Detail string `json:"detail,omitempty"`
	// RunID / RunURL identify the Actions run when Signal is actions_run.
	RunID  int64  `json:"run_id,omitempty"`
	RunURL string `json:"run_url,omitempty"`
	// FailedJob is the job to fetch logs for, nil unless the deploy failed
	// through an Actions run.
	FailedJob *DeployWatchJob `json:"failed_job,omitempty"`
	// Contexts are the commit-status contexts behind a commit_status verdict
	// ("Vercel", "vercel[bot]/deploy", …).
	Contexts []string `json:"contexts,omitempty"`
	// HealthURL / LogsURL are the environment's own endpoints, carried so the
	// agent needs no second call to find them.
	HealthURL string `json:"health_url,omitempty"`
	LogsURL   string `json:"logs_url,omitempty"`
	// AutoRollback is the target's policy: is a failure rolled back by the
	// agent or written up for a human.
	AutoRollback bool `json:"auto_rollback"`
	// HealthWindowUntil is set on a successful deploy: until then an incident
	// on this environment is attributed to THIS task.
	HealthWindowUntil *time.Time `json:"health_window_until,omitempty"`
	CheckedAt         time.Time  `json:"checked_at"`
}

// Failed reports a deploy that finished red — the trigger for the rollback
// half of the loop.
func (s DeployWatchStatus) Failed() bool { return s.State == DeployWatchFailure }

// ReleaseAttribution names the task a production incident belongs to, keyed on
// the commit that was deployed — a specific card where the old correlation
// ("some deploy finished in the last 45 minutes") yielded only a generic
// "roll back the last release".
type ReleaseAttribution struct {
	TaskID     uuid.UUID `json:"task_id"`
	TaskKey    string    `json:"task_key,omitempty"`
	Title      string    `json:"title,omitempty"`
	MergeSHA   string    `json:"merge_commit_sha"`
	Env        string    `json:"env"`
	DeployedAt time.Time `json:"deployed_at"`
}

// RollbackMechanism is HOW a release is undone, chosen by what the repository
// actually has rather than by configuration.
type RollbackMechanism string

const (
	// RollbackMechanismWorkflow is the existing one: tag the last known-good
	// commit and workflow_dispatch the deploy workflow at that tag. It needs a
	// deploy workflow to dispatch.
	RollbackMechanismWorkflow RollbackMechanism = "workflow_dispatch"
	// RollbackMechanismRevert is for the push-to-deploy case, where the host
	// redeploys whatever the default branch points at: the rollback is `git
	// revert` of the merge commit followed by a push. A squash merge is a
	// single-parent commit, so this is plain `git revert <sha>` — no -m, which
	// would fail with "mainline was specified but commit is not a merge".
	RollbackMechanismRevert RollbackMechanism = "revert_push"
	// RollbackMechanismProvider: the hosting provider put the previous
	// deployment back into production (Vercel instant rollback, Cloud Run
	// traffic); the pushed revert still follows so the default branch cannot
	// ship the bad change again.
	RollbackMechanismProvider RollbackMechanism = "provider_rollback"
)

// TaskRollbackResult is what a rollback attempt did, successful or not.
type TaskRollbackResult struct {
	RolledBack bool              `json:"rolled_back"`
	Mechanism  RollbackMechanism `json:"mechanism,omitempty"`
	Env        string            `json:"env"`
	// Ref is the tag dispatched (workflow mechanism) or the branch pushed
	// (revert mechanism).
	Ref string `json:"ref,omitempty"`
	// RevertSHA is the revert commit for the revert mechanism.
	RevertSHA string `json:"revert_sha,omitempty"`
	// RolledBackFrom is the commit that was live and is now being undone.
	RolledBackFrom string `json:"rolled_back_from,omitempty"`
	// RolledBackTo is the commit production is being returned to (workflow
	// mechanism only; a revert has no single prior commit to name).
	RolledBackTo string    `json:"rolled_back_to,omitempty"`
	IncidentID   uuid.UUID `json:"incident_id,omitempty"`
	// Proposed is true when auto_rollback is OFF: nothing was executed, the
	// incident carries the proposal for a human to confirm.
	Proposed bool `json:"proposed"`
	// ManualSteps are the parts of the task's rollback plan this system cannot
	// perform (migration reversal, external feature flag, cache purge),
	// reported loudly rather than skipped.
	ManualSteps []string `json:"manual_steps,omitempty"`
	Message     string   `json:"message"`
}

// Rollback refusals. Each is a state only a board or a human action can
// change, so the tool tells the model not to retry.
var (
	// ErrRollbackNotMerged: the task's change never landed, so there is
	// nothing in production to undo.
	ErrRollbackNotMerged = errors.New("this task has no merge commit recorded — nothing of it is in production, so there is nothing to roll back")
	// ErrRollbackNotOwner is the authorization replacing the human's typed
	// confirmation for an agent actor: the task asking must own the commit
	// production is currently running.
	ErrRollbackNotOwner = errors.New("this task's merge commit is not what the environment is currently running — another release has shipped since, and rolling back now would undo somebody else's change")
	// auto_rollback off is deliberately a SUCCESSFUL call that executed nothing
	// and returned a proposal (Proposed), not an error — an error reads to a
	// model as a broken system to work around.

	// ErrRollbackNoTrigger: the release did not fail, so there is nothing to
	// roll back.
	ErrRollbackNoTrigger = errors.New("this task's deploy did not fail and its environment is healthy — a rollback needs a failed deploy or an open incident attributed to this release")
	// ErrRollbackNoMechanism: neither a deploy workflow nor a resolvable
	// working copy to revert on.
	ErrRollbackNoMechanism = errors.New("this repository has no deploy workflow to dispatch and no resolvable git working copy to revert on — the rollback cannot be performed from here")
	// ErrRollbackColumn: the task left a column that never released anything.
	ErrRollbackColumn = errors.New("a release can only be rolled back from `done` or `released` — this task has not been released")
)

// DeployLogsToolName is named in domain because the policy layer decides
// things about it by name in more than one place.
const DeployLogsToolName = "get_deploy_logs"

// ReleaseControlTools are the tools that CHANGE production or a release's
// outcome. A list so the stage narrowing can iterate it.
var ReleaseControlTools = []string{
	DeployReleaseToolName,
	FinishReleaseToolName,
	ReleaseRollbackToolName,
}

// IsReleaseControlTool reports whether name can change what is running in
// production.
func IsReleaseControlTool(name string) bool {
	for _, t := range ReleaseControlTools {
		if t == name {
			return true
		}
	}
	return false
}

// ReleaseTagForCommit is the tag a release is dispatched at. workflow_dispatch
// accepts only a branch or tag name, never a bare SHA. The name derives from
// the COMMIT, not the clock: re-releasing the same commit reuses the same tag,
// so "already exists" is a success rather than an accumulation.
func ReleaseTagForCommit(sha string) string {
	return "release/" + ShortSHA(strings.TrimSpace(sha))
}

// TaskRollbackRunbook renders the task's own rollback instructions — the
// rollback_plan, before_deploy and after_deploy fields a developer wrote — as
// the text a rollback run must follow. This is the half of a rollback no
// mechanism can perform: `git revert` undoes code, not migrations, feature
// flags, CDN purges or manual switches, and the developer was the only one who
// knew which applied. Empty when the task recorded none, so a rollback of a
// task with no plan says so rather than pretending one was followed.
func TaskRollbackRunbook(task BoardTask) string {
	var sections []string
	if plan := trimmedTaskField(task.RollbackPlan); plan != "" {
		sections = append(sections, "Rollback plan recorded on this task (FOLLOW IT — it is the developer's own instruction):\n"+plan)
	}
	if before := trimmedTaskField(task.BeforeDeploy); before != "" {
		sections = append(sections, "What had to happen BEFORE this was deployed (each of these may need undoing, in reverse order):\n"+before)
	}
	if after := trimmedTaskField(task.AfterDeploy); after != "" {
		sections = append(sections, "What was done AFTER the deploy (undo anything here that is now pointing at code that no longer exists):\n"+after)
	}
	return strings.Join(sections, "\n\n")
}

// HasRollbackRunbook reports whether the task carries any rollback instructions
// at all.
func HasRollbackRunbook(task BoardTask) bool { return TaskRollbackRunbook(task) != "" }

func trimmedTaskField(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
