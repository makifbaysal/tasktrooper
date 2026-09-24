package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrDeployDependencyNotReleased blocks a release whose task declares a
// deploy_depends_on relation on something that is not live yet. This is the one
// gate that reads OTHER tasks: the rest of the release chain asks "is this task
// ready" and none can see that the API this client calls has not shipped. The
// wrapped detail names the blocking task keys, because "a dependency is not
// released" without saying which one is not actionable.
var ErrDeployDependencyNotReleased = errors.New("release blocked: a task this one must deploy after is not live in production yet")

// ErrDeployPackageCycle refuses to release a package whose members' deploy
// dependencies form a loop. There is no order that satisfies it, and releasing
// in an arbitrary order would deploy something into an environment its own
// dependency gate would have refused.
var ErrDeployPackageCycle = errors.New("deploy package cannot be ordered: its tasks' deploy dependencies form a cycle")

// Deploy package statuses — the train's own lifecycle, independent of any
// member task's board column: a package is released when every member has
// production evidence, not when the last card was dragged.
const (
	// DeployPackageStatusDraft is assembled but never dispatched.
	DeployPackageStatusDraft = "draft"
	// DeployPackageStatusReleasing has at least one member's deploy in flight;
	// later waves are dispatched lazily as earlier members land, so a package
	// sits here across several deploys.
	DeployPackageStatusReleasing = "releasing"
	// DeployPackageStatusReleased means every member is live in production.
	DeployPackageStatusReleased = "released"
	// DeployPackageStatusFailed means a member's release was refused or its
	// deploy failed; Note carries the task key and the reason.
	DeployPackageStatusFailed = "failed"
	// DeployPackageStatusCancelled is a human abandoning the train.
	DeployPackageStatusCancelled = "cancelled"
)

// ValidDeployPackageStatus mirrors the schema's CHECK constraint.
func ValidDeployPackageStatus(s string) bool {
	switch s {
	case DeployPackageStatusDraft, DeployPackageStatusReleasing, DeployPackageStatusReleased,
		DeployPackageStatusFailed, DeployPackageStatusCancelled:
		return true
	default:
		return false
	}
}

// DeployPackage is an explicitly assembled release train for one repository:
// it batches several tasks into one production deploy instead of releasing
// each as it reaches done.
type DeployPackage struct {
	ID           uuid.UUID `json:"id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	Status       string    `json:"status"`
	// Note explains a failure (which member, which error) or a cancellation.
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Tasks is the enriched membership the list endpoint returns; empty on the
	// bare row the store writes.
	Tasks []DeployPackageTask `json:"tasks,omitempty"`
}

// DeployPackageTask is one member of a package, enriched with what the UI needs
// to render it without a second round trip per task.
type DeployPackageTask struct {
	TaskID   uuid.UUID `json:"task_id"`
	Position int       `json:"position"`
	// Key / Title / Column describe the task as the board shows it; joined in
	// by the store rather than fetched per row.
	Key    string     `json:"key,omitempty"`
	Title  string     `json:"title,omitempty"`
	Column TaskColumn `json:"column,omitempty"`
	// Released is the package's own verdict on this member: does it have
	// production evidence yet. It is what AdvancePackage recomputes on, so the
	// UI and the advancement logic never disagree about who is still pending.
	Released bool `json:"released"`
}
