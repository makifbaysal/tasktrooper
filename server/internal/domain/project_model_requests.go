package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Patch distinguishes an absent JSON field (leave alone) from null (revert
// to the detected value) from a value (set the human override).
type Patch[T any] struct {
	Set   bool
	Value *T
}

func (p *Patch[T]) UnmarshalJSON(b []byte) error {
	p.Set = true
	if string(b) == "null" {
		p.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	p.Value = &v
	return nil
}

func (p Patch[T]) MarshalJSON() ([]byte, error) {
	if p.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*p.Value)
}

func (p Patch[T]) ApplyTo(f Fact[T]) Fact[T] {
	if p.Set {
		f.Override = p.Value
	}
	return f
}

type ComponentPatch struct {
	Name     Patch[string]                    `json:"name"`
	Role     Patch[ComponentRole]             `json:"role"`
	Commands map[CommandPurpose]Patch[string] `json:"commands,omitempty"`
	Gates    *ComponentGates                  `json:"gates,omitempty"`
	Docs     *RepositoryDocs                  `json:"docs,omitempty"`
	Status   *ComponentStatus                 `json:"status,omitempty"`
	// Reviewed true acknowledges a component a later scan added.
	Reviewed *bool `json:"reviewed,omitempty"`
}

type NewComponentRequest struct {
	Path string        `json:"path"`
	Name string        `json:"name,omitempty"`
	Role ComponentRole `json:"role"`
}

type CheckPatch struct {
	Purpose       Patch[CheckPurpose]   `json:"purpose"`
	Gate          Patch[CheckGate]      `json:"gate"`
	LocalCommands Patch[[]LocalCommand] `json:"local_commands"`
	Status        *ModelStatus          `json:"status,omitempty"`
	Reviewed      *bool                 `json:"reviewed,omitempty"`
}

type NewCheckRequest struct {
	ComponentID   uuid.UUID      `json:"component_id"`
	Name          string         `json:"name"`
	Purpose       CheckPurpose   `json:"purpose"`
	LocalCommands []LocalCommand `json:"local_commands"`
	Gate          CheckGate      `json:"gate"`
}

// ResourceRef names a resource the human picked or typed; the service turns
// it into a SystemResource with a user-scoped identity key.
type ResourceRef struct {
	Kind   ResourceKind `json:"kind"`
	Vendor string       `json:"vendor,omitempty"`
	Name   string       `json:"name"`
}

type LinkPatch struct {
	Status        *LinkStatus   `json:"status,omitempty"`
	ToComponentID *uuid.UUID    `json:"to_component_id,omitempty"`
	ToResource    *ResourceRef  `json:"to_resource,omitempty"`
	Protocol      *LinkProtocol `json:"protocol,omitempty"`
}

type NewLinkRequest struct {
	FromComponentID uuid.UUID    `json:"from_component_id"`
	ToComponentID   *uuid.UUID   `json:"to_component_id,omitempty"`
	ToResource      *ResourceRef `json:"to_resource,omitempty"`
	Protocol        LinkProtocol `json:"protocol"`
	Detail          string       `json:"detail,omitempty"`
}

type SaveNoteRequest struct {
	ComponentID *uuid.UUID `json:"component_id,omitempty"`
	Topic       NoteTopic  `json:"topic"`
	BodyMD      string     `json:"body_md"`
	Locked      bool       `json:"locked"`
}

type NotePatch struct {
	BodyMD *string `json:"body_md,omitempty"`
	Locked *bool   `json:"locked,omitempty"`
}

type ReviewKind string

const (
	ReviewRole        ReviewKind = "role"
	ReviewLink        ReviewKind = "link"
	ReviewEnvironment ReviewKind = "environment"
	ReviewComponent   ReviewKind = "component"
	ReviewCheck       ReviewKind = "check"
)

// ReviewItem points at one medium-confidence value waiting for the human; the
// UI renders it from the entity in the same payload.
type ReviewItem struct {
	Kind         ReviewKind `json:"kind"`
	EntityID     uuid.UUID  `json:"entity_id"`
	RepositoryID uuid.UUID  `json:"repository_id"`
	ComponentID  uuid.UUID  `json:"component_id"`
	Confidence   Confidence `json:"confidence"`
}

// LinkedComponent is the display identity of a component that lives in
// another repository but is the other end of one of this payload's links.
type LinkedComponent struct {
	ID             uuid.UUID     `json:"id"`
	RepositoryID   uuid.UUID     `json:"repository_id"`
	RepositoryName string        `json:"repository_name"`
	ProjectIDs     []uuid.UUID   `json:"project_ids,omitempty"`
	Path           string        `json:"path"`
	Name           string        `json:"name"`
	Role           ComponentRole `json:"role"`
}

// RepositoryModel is GET /v1/repositories/:id/model.
type RepositoryModel struct {
	Repository       Repository             `json:"repository"`
	Shape            RepoShape              `json:"shape"`
	Components       []Component            `json:"components"`
	Checks           []ComponentCheck       `json:"checks"`
	Links            []ComponentLink        `json:"links"`
	IncomingLinks    []ComponentLink        `json:"incoming_links"`
	Resources        []SystemResource       `json:"resources"`
	LinkedComponents []LinkedComponent      `json:"linked_components"`
	Notes            []ProjectNote          `json:"notes"`
	Environments     []ComponentEnvironment `json:"environments"`
	Review           []ReviewItem           `json:"review"`
	LatestScan       *ProjectScan           `json:"latest_scan,omitempty"`
}

type ComponentSummary struct {
	ID             uuid.UUID     `json:"id"`
	Path           string        `json:"path"`
	Name           string        `json:"name"`
	Role           ComponentRole `json:"role"`
	RoleConfidence Confidence    `json:"role_confidence,omitempty"`
	StackSummary   string        `json:"stack_summary"`
	Checks         int           `json:"checks"`
	RequiredChecks int           `json:"required_checks"`
}

type ScanSummary struct {
	ID         uuid.UUID   `json:"id"`
	Status     ScanStatus  `json:"status"`
	Trigger    ScanTrigger `json:"trigger"`
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
}

type RepositorySummary struct {
	ID          uuid.UUID          `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	RemoteURL   string             `json:"remote_url,omitempty"`
	ProjectIDs  []uuid.UUID        `json:"project_ids,omitempty"`
	Shape       RepoShape          `json:"shape"`
	Components  []ComponentSummary `json:"components"`
	ReviewCount int                `json:"review_count"`
	LastScan    *ScanSummary       `json:"last_scan,omitempty"`
	// Environments is filled from stored health only, so the projects page
	// never waits on a cloud provider.
	Environments []EnvironmentSummary `json:"environments"`
	GitWarning   string               `json:"git_warning,omitempty"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

type ProjectRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type ResourceRefView struct {
	ID   uuid.UUID    `json:"id"`
	Kind ResourceKind `json:"kind"`
	Name string       `json:"name"`
}

// ProjectOverview is one card on the projects page and the header of the
// project page. CrossProjects lists only projects this one actually links to
// or is linked from; independent projects have none.
type ProjectOverview struct {
	ID              uuid.UUID           `json:"id"`
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	Type            ProjectType         `json:"type"`
	Repositories    []RepositorySummary `json:"repositories"`
	ReviewCount     int                 `json:"review_count"`
	CrossProjects   []ProjectRef        `json:"cross_projects"`
	CrossLinks      int                 `json:"cross_links"`
	SharedResources []ResourceRefView   `json:"shared_resources"`
}

// ProjectsOverview is GET /v1/projects/overview.
type ProjectsOverview struct {
	Projects   []ProjectOverview   `json:"projects"`
	Unassigned []RepositorySummary `json:"unassigned"`
}

// ProjectDetail is GET /v1/projects/:projectId/overview.
type ProjectDetail struct {
	ProjectOverview
	Review []ReviewItem `json:"review"`
}

type EnvironmentSummary struct {
	ID            uuid.UUID           `json:"id"`
	ComponentID   uuid.UUID           `json:"component_id"`
	Environment   DeployEnvironment   `json:"environment"`
	Provider      CloudProviderKind   `json:"provider,omitempty"`
	ResourceName  string              `json:"resource_name,omitempty"`
	URL           string              `json:"url,omitempty"`
	Status        LinkStatus          `json:"status"`
	Health        CloudResourceStatus `json:"health,omitempty"`
	ErrorCount24h int                 `json:"error_count_24h"`
}
