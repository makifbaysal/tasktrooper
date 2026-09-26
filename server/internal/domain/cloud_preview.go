package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type PreviewAccessMode string

const (
	PreviewAccessNone                  PreviewAccessMode = "none"
	PreviewAccessVercelAuth            PreviewAccessMode = "vercel_authentication"
	PreviewAccessPassword              PreviewAccessMode = "password"
	PreviewAccessVercelAuthAndPassword PreviewAccessMode = "vercel_authentication_and_password"
)

// Vercel's Protection Bypass for Automation: the secret goes in this header
// (or the same-named query parameter), and the set-cookie flag makes the
// browser's follow-up requests carry it without the parameter.
const (
	VercelProtectionBypassHeader    = "x-vercel-protection-bypass"
	VercelProtectionBypassSetCookie = "x-vercel-set-bypass-cookie"
)

// PreviewAccess is whether a per-branch preview answers an automated request
// at all (Vercel Deployment Protection), and whether the project holds a
// "Protection Bypass for Automation" secret that lets one through.
type PreviewAccess struct {
	Protected        bool              `json:"protected"`
	Mode             PreviewAccessMode `json:"mode"`
	BypassConfigured bool              `json:"bypass_configured"`
	// BypassSecret opens the preview for whoever holds it, so it is never
	// serialized: only the agent tool that drives the preview reads it.
	BypassSecret string `json:"-"`
}

type TaskPreviewStatus string

const (
	TaskPreviewBuilding TaskPreviewStatus = "building"
	TaskPreviewReady    TaskPreviewStatus = "ready"
	TaskPreviewError    TaskPreviewStatus = "error"
	TaskPreviewCanceled TaskPreviewStatus = "canceled"
	TaskPreviewNone     TaskPreviewStatus = "none"
)

// TaskPreviewStatusOf maps a deployment's state onto the preview's. An
// unrecognised provider state (blocked, deleted) is reported as error so the
// reader opens inspect_url rather than waiting on a build that is not coming.
func TaskPreviewStatusOf(s CloudDeploymentStatus) TaskPreviewStatus {
	switch s {
	case CloudDeployReady:
		return TaskPreviewReady
	case CloudDeployBuilding:
		return TaskPreviewBuilding
	case CloudDeployCanceled:
		return TaskPreviewCanceled
	}
	return TaskPreviewError
}

// TaskPreview is one component's preview deployment for a task's branch.
// Timestamps are strings so a "none" entry carries them empty rather than as
// the zero time.
type TaskPreview struct {
	ComponentID      uuid.UUID         `json:"component_id"`
	ComponentName    string            `json:"component_name"`
	EnvironmentID    uuid.UUID         `json:"environment_id"`
	Provider         CloudProviderKind `json:"provider"`
	Status           TaskPreviewStatus `json:"status"`
	URL              string            `json:"url"`
	BranchURL        string            `json:"branch_url"`
	PRNumber         int               `json:"pr_number"`
	CommitSHA        string            `json:"commit_sha"`
	CreatedAt        string            `json:"created_at"`
	ReadyAt          string            `json:"ready_at,omitempty"`
	InspectURL       string            `json:"inspect_url"`
	Protected        bool              `json:"protected"`
	BypassConfigured bool              `json:"bypass_configured"`
	// BypassSecret: see PreviewAccess.BypassSecret.
	BypassSecret string `json:"-"`
}

// TaskPreviews is GET …/tasks/:taskId/previews before it is narrowed to the
// list: Branch and HeadSHA are what the lookup keyed on, which the agent tool
// reports so a preview built from an older commit is recognisable as stale.
type TaskPreviews struct {
	Branch   string        `json:"branch"`
	HeadSHA  string        `json:"head_sha,omitempty"`
	Previews []TaskPreview `json:"previews"`
}

func TaskPreviewFromDeployment(d CloudDeployment) TaskPreview {
	p := TaskPreview{
		Status:     TaskPreviewStatusOf(d.Status),
		URL:        d.URL,
		BranchURL:  d.BranchURL,
		PRNumber:   d.PRNumber,
		CommitSHA:  d.CommitSHA,
		InspectURL: d.InspectURL,
	}
	if !d.CreatedAt.IsZero() {
		p.CreatedAt = d.CreatedAt.UTC().Format(time.RFC3339)
	}
	if d.ReadyAt != nil && !d.ReadyAt.IsZero() {
		p.ReadyAt = d.ReadyAt.UTC().Format(time.RFC3339)
	}
	return p
}

// SameCommit matches a short sha against a full one in either direction, the
// way a provider's build metadata and GitHub's PR head each record one.
func SameCommit(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if len(a) < 7 || len(b) < 7 {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}
