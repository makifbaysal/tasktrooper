package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// A Release is one shipment of one component: the merge commits it carries,
// how it was deployed, what production looked like afterwards and what was
// decided. A task reaches `released` only through its release's verdict — never
// through a deploy job's colour alone.
type ReleaseStatus string

const (
	// ReleaseDraft collects merged tasks of a batch component until a human cuts it.
	ReleaseDraft ReleaseStatus = "draft"
	// ReleasePending is opened but not deploying yet (dispatch mode waits for
	// deploy_release; a cut batch waits for its executor).
	ReleasePending ReleaseStatus = "pending"
	// ReleaseDeploying: the deploy was started or the merge started it; the
	// sweeper watches it settle.
	ReleaseDeploying ReleaseStatus = "deploying"
	// ReleaseVerifying: the deploy settled green; the soak window is running
	// (health samples, smoke checks, new runtime errors).
	ReleaseVerifying ReleaseStatus = "verifying"
	// ReleaseAwaitingVerdict: evidence is in; the release engineer reads the
	// logs and either finishes or rolls back.
	ReleaseAwaitingVerdict ReleaseStatus = "awaiting_verdict"
	// ReleaseRollingBack: a rollback was started; the sweeper watches the
	// rollback deploy.
	ReleaseRollingBack ReleaseStatus = "rolling_back"
	ReleaseReleased    ReleaseStatus = "released"
	ReleaseRolledBack  ReleaseStatus = "rolled_back"
	// ReleaseFailed: the deploy failed, never showed up, or the rollback
	// itself failed — a human or the release engineer decides what follows.
	ReleaseFailed ReleaseStatus = "failed"
	// ReleaseSuperseded: a newer merge of the same component opened a release
	// while this one was unfinished; the newer one deploys this one's commit
	// too, so it took over these tasks and is judged for both.
	ReleaseSuperseded ReleaseStatus = "superseded"
)

// ReleaseActor is who performed a release action; an agent is refused what
// only a human may confirm (a rollback with auto_rollback off).
type ReleaseActor string

const (
	ReleaseActorAgent  ReleaseActor = "agent"
	ReleaseActorHuman  ReleaseActor = "human"
	ReleaseActorSystem ReleaseActor = "system"
)

func (s ReleaseStatus) Valid() bool {
	switch s {
	case ReleaseDraft, ReleasePending, ReleaseDeploying, ReleaseVerifying, ReleaseAwaitingVerdict,
		ReleaseRollingBack, ReleaseReleased, ReleaseRolledBack, ReleaseFailed, ReleaseSuperseded:
		return true
	}
	return false
}

// Terminal reports a release nothing will act on again by itself.
func (s ReleaseStatus) Terminal() bool {
	return s == ReleaseReleased || s == ReleaseRolledBack || s == ReleaseSuperseded
}

// Open reports a release a newer merge of the same component must take over
// rather than run beside.
func (s ReleaseStatus) Open() bool {
	return s == ReleasePending || s == ReleaseDeploying || s == ReleaseVerifying || s == ReleaseAwaitingVerdict
}

// Watched reports a release the sweeper advances without an agent.
func (s ReleaseStatus) Watched() bool {
	return s == ReleaseDeploying || s == ReleaseVerifying || s == ReleaseRollingBack
}

// ReleaseParks reports a release the release engineer waits on (its card is
// parked on ResourceReleaseWatch until the sweeper hands it back).
func (s ReleaseStatus) Parks() bool { return s.Watched() }

type ReleaseTaskRef struct {
	ID             uuid.UUID  `json:"id"`
	Key            string     `json:"key,omitempty"`
	Title          string     `json:"title,omitempty"`
	TaskType       TaskType   `json:"task_type,omitempty"`
	Column         TaskColumn `json:"column,omitempty"`
	MergeCommitSHA string     `json:"merge_commit_sha,omitempty"`
}

type HealthSample struct {
	At        time.Time `json:"at"`
	Status    int       `json:"status,omitempty"`
	OK        bool      `json:"ok"`
	LatencyMS int64     `json:"latency_ms,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type SmokeResult struct {
	Check     SmokeCheck `json:"check"`
	URL       string     `json:"url"`
	At        time.Time  `json:"at"`
	Status    int        `json:"status,omitempty"`
	OK        bool       `json:"ok"`
	LatencyMS int64      `json:"latency_ms,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// ReleaseChecks is the evidence gathered while the release was verified.
type ReleaseChecks struct {
	// EnvironmentID is the bound production environment the runtime reads
	// came from; nil when the component has none (health/smoke only).
	EnvironmentID *uuid.UUID     `json:"environment_id,omitempty"`
	BaseURL       string         `json:"base_url,omitempty"`
	HealthURL     string         `json:"health_url,omitempty"`
	Health        []HealthSample `json:"health,omitempty"`
	Smoke         []SmokeResult  `json:"smoke,omitempty"`
	// NewErrors are runtime error groups whose first occurrence is after the
	// deploy settled.
	NewErrors []RuntimeErrorGroup `json:"new_errors,omitempty"`
	// Notes are gaps the verifier hit ("no health URL", "logs not readable")
	// so a clean result is never mistaken for a checked one.
	Notes []string `json:"notes,omitempty"`
	// EarlyStop names why the soak ended before its window did.
	EarlyStop string `json:"early_stop,omitempty"`
}

// Failing reports whether the evidence alone says the release is unhealthy.
func (c ReleaseChecks) Failing(maxNewErrors int) bool {
	if c.EarlyStop != "" {
		return true
	}
	for _, s := range c.Smoke {
		if !s.OK {
			return true
		}
	}
	return len(c.NewErrors) > maxNewErrors
}

type RollbackReason string

const (
	RollbackDeployFailed   RollbackReason = "deploy_failed"
	RollbackVerifyFailed   RollbackReason = "verify_failed"
	RollbackHealthIncident RollbackReason = "health_incident"
	RollbackManual         RollbackReason = "manual"
)

func (r RollbackReason) Valid() bool {
	switch r {
	case RollbackDeployFailed, RollbackVerifyFailed, RollbackHealthIncident, RollbackManual:
		return true
	}
	return false
}

// ReleaseRollback records how a release was undone.
type ReleaseRollback struct {
	Reason RollbackReason `json:"reason"`
	Note   string         `json:"note,omitempty"`
	// Mechanism is what restored production: redeploying the previous good
	// release (dispatch) or a pushed revert that redeploys on merge.
	Mechanism RollbackMechanism `json:"mechanism,omitempty"`
	// RevertSHA is the commit on the default branch that undoes the release's
	// merges — pushed in every mode, so the next release cannot ship the bad
	// change again.
	RevertSHA string `json:"revert_sha,omitempty"`
	// RestoredRef is what was redeployed (the previous release's tag or the
	// revert commit).
	RestoredRef string `json:"restored_ref,omitempty"`
	RunURL      string `json:"run_url,omitempty"`
	// ManualSteps is what no mechanism can undo (migrations, flags, CDN) —
	// from the tasks' rollback plans; the agent performs or reports each.
	ManualSteps []string  `json:"manual_steps,omitempty"`
	Actor       string    `json:"actor,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	Detail      string    `json:"detail,omitempty"`
}

type Release struct {
	ID           uuid.UUID        `json:"id"`
	RepositoryID uuid.UUID        `json:"repository_id"`
	ComponentID  *uuid.UUID       `json:"component_id,omitempty"`
	Version      string           `json:"version"`
	Mode         DeliveryMode     `json:"mode"`
	Executor     DeliveryExecutor `json:"executor,omitempty"`
	Status       ReleaseStatus    `json:"status"`
	// CommitSHA is the commit this release puts in production: the task's
	// merge commit, or the default branch head a batch was cut at.
	CommitSHA string `json:"commit_sha,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Notes     string `json:"notes,omitempty"`
	// Profile is the delivery profile the release was opened under, frozen so
	// a later edit cannot change what an in-flight release is judged by.
	Profile ComponentDelivery  `json:"profile"`
	Deploy  *DeployWatchStatus `json:"deploy,omitempty"`
	Checks  ReleaseChecks      `json:"checks"`
	// Verdict is the release engineer's (or a human's) closing note.
	Verdict  string           `json:"verdict,omitempty"`
	Rollback *ReleaseRollback `json:"rollback,omitempty"`
	// FailureReason is set with ReleaseFailed.
	FailureReason string `json:"failure_reason,omitempty"`
	// CardTaskID is the release card a cut batch runs on.
	CardTaskID *uuid.UUID `json:"card_task_id,omitempty"`
	// LocalRun is set for a batch release built on this machine.
	LocalRun *ReleaseLocalRun `json:"local_run,omitempty"`
	// StoreBuilds is set for a batch release built through the store pipeline.
	StoreBuilds     []ReleaseStoreBuild `json:"store_builds,omitempty"`
	CutAt           *time.Time          `json:"cut_at,omitempty"`
	Tasks           []ReleaseTaskRef    `json:"tasks"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	DeployStartedAt *time.Time          `json:"deploy_started_at,omitempty"`
	DeployedAt      *time.Time          `json:"deployed_at,omitempty"`
	VerifyUntil     *time.Time          `json:"verify_until,omitempty"`
	FinishedAt      *time.Time          `json:"finished_at,omitempty"`
}

func (r Release) TaskIDs() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(r.Tasks))
	for _, t := range r.Tasks {
		out = append(out, t.ID)
	}
	return out
}

// ReleaseOpening is what merging a task set in motion; carried on the merge
// result so the release engineer knows its next step without another call.
type ReleaseOpening struct {
	Mode      DeliveryMode  `json:"mode"`
	ReleaseID *uuid.UUID    `json:"release_id,omitempty"`
	Status    ReleaseStatus `json:"status,omitempty"`
	// Released is true when the merge was the whole release (mode none).
	Released bool `json:"released,omitempty"`
	// Unconfirmed is true when the component's delivery profile has not been
	// confirmed: nothing deploys and the task waits in done.
	Unconfirmed bool   `json:"unconfirmed,omitempty"`
	Next        string `json:"next"`
}

// ReleaseListFilter narrows a release listing.
type ReleaseListFilter struct {
	RepositoryID *uuid.UUID
	ComponentID  *uuid.UUID
	TaskID       *uuid.UUID
	Statuses     []ReleaseStatus
	Limit        int
}

var (
	ErrReleaseNotFound = errors.New("release not found")
	// ErrReleaseWrongStatus: the action does not apply in the release's
	// current status; the message names both.
	ErrReleaseWrongStatus = errors.New("the release is not in a status this action applies to")
	// ErrDeliveryUnconfirmed: the component's delivery profile was never
	// confirmed, so nothing may deploy it.
	ErrDeliveryUnconfirmed = errors.New("the component's delivery profile is not confirmed")
	// ErrReleaseNoDeploy: the action needs a deploy mechanism the profile does
	// not have (deploy_release on an on_merge component).
	ErrReleaseNoDeploy = errors.New("this release's delivery mode has no deploy step to trigger")
	// ErrRollbackNeedsHuman: auto_rollback is off; the proposal is written and
	// a human confirms it.
	ErrRollbackNeedsHuman = errors.New("auto rollback is off for this component — a human must confirm the rollback")
)

// Release tools, named in domain because the stage policy decides things
// about them by name.
const (
	GetReleaseToolName      = "get_release"
	DeployReleaseToolName   = "deploy_release"
	WatchReleaseToolName    = "watch_release"
	RunSmokeChecksToolName  = "run_smoke_checks"
	FinishReleaseToolName   = "finish_release"
	ReleaseRollbackToolName = "rollback_release"
)
