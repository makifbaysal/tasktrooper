package domain

import "github.com/google/uuid"

// MapTier is the column a node sits in on an architecture map: callers on
// the left, what they call to the right.
type MapTier string

const (
	TierClient   MapTier = "client"
	TierService  MapTier = "service"
	TierLibrary  MapTier = "library"
	TierData     MapTier = "data"
	TierExternal MapTier = "external"
)

func TierForRole(r ComponentRole) MapTier {
	switch r {
	case ComponentRoleFrontend, ComponentRoleMobile, ComponentRoleDesktop, ComponentRoleCLI:
		return TierClient
	case ComponentRoleLibrary:
		return TierLibrary
	default:
		return TierService
	}
}

func TierForResource(k ResourceKind) MapTier {
	switch k {
	case ResourceDatabase, ResourceCache, ResourceQueue, ResourceStorage, ResourceSearch:
		return TierData
	default:
		return TierExternal
	}
}

type MapNodeKind string

const (
	MapNodeComponent MapNodeKind = "component"
	MapNodeResource  MapNodeKind = "resource"
)

// MapNode is a component or a resource. Foreign marks a component of another
// project, drawn only because one of this project's edges reaches it.
type MapNode struct {
	ID             string              `json:"id"`
	Kind           MapNodeKind         `json:"kind"`
	Tier           MapTier             `json:"tier"`
	Label          string              `json:"label"`
	Role           ComponentRole       `json:"role,omitempty"`
	ResourceKind   ResourceKind        `json:"resource_kind,omitempty"`
	Vendor         string              `json:"vendor,omitempty"`
	ComponentID    *uuid.UUID          `json:"component_id,omitempty"`
	ResourceID     *uuid.UUID          `json:"resource_id,omitempty"`
	RepositoryID   *uuid.UUID          `json:"repository_id,omitempty"`
	RepositoryName string              `json:"repository_name,omitempty"`
	Path           string              `json:"path,omitempty"`
	ProjectIDs     []uuid.UUID         `json:"project_ids,omitempty"`
	Foreign        bool                `json:"foreign"`
	StackSummary   string              `json:"stack_summary,omitempty"`
	Provider       CloudProviderKind   `json:"provider,omitempty"`
	Health         CloudResourceStatus `json:"health,omitempty"`
	ErrorCount24h  int                 `json:"error_count_24h,omitempty"`
	// SharedWith lists the other projects linking the same resource.
	SharedWith []ProjectRef `json:"shared_with,omitempty"`
}

type MapEdge struct {
	ID       string       `json:"id"`
	LinkID   uuid.UUID    `json:"link_id"`
	From     string       `json:"from"`
	To       string       `json:"to"`
	Protocol LinkProtocol `json:"protocol"`
	Detail   string       `json:"detail,omitempty"`
	Status   LinkStatus   `json:"status"`
	Source   LinkSource   `json:"source"`
	EnvVars  []string     `json:"env_vars,omitempty"`
	// CrossProject is set when the edge leaves the project the map is for.
	CrossProject bool `json:"cross_project"`
}

// ProjectMap is GET /v1/projects/:projectId/map.
type ProjectMap struct {
	Project ProjectRef `json:"project"`
	Nodes   []MapNode  `json:"nodes"`
	Edges   []MapEdge  `json:"edges"`
}

type WorkspaceMapRepository struct {
	ID         uuid.UUID          `json:"id"`
	Name       string             `json:"name"`
	Shape      RepoShape          `json:"shape"`
	Components []ComponentSummary `json:"components"`
}

type WorkspaceMapProject struct {
	ID           uuid.UUID                `json:"id"`
	Name         string                   `json:"name"`
	Type         ProjectType              `json:"type"`
	Repositories []WorkspaceMapRepository `json:"repositories"`
}

// WorkspaceMapEdge aggregates every link between two projects; independent
// projects simply have none.
type WorkspaceMapEdge struct {
	FromProjectID uuid.UUID      `json:"from_project_id"`
	ToProjectID   uuid.UUID      `json:"to_project_id"`
	Links         int            `json:"links"`
	Suggested     int            `json:"suggested"`
	Protocols     []LinkProtocol `json:"protocols"`
	Examples      []string       `json:"examples"`
}

type WorkspaceSharedResource struct {
	Resource   ResourceRefView `json:"resource"`
	ProjectIDs []uuid.UUID     `json:"project_ids"`
}

// WorkspaceMap is GET /v1/projects/map.
type WorkspaceMap struct {
	Projects        []WorkspaceMapProject     `json:"projects"`
	Unassigned      []WorkspaceMapRepository  `json:"unassigned"`
	Edges           []WorkspaceMapEdge        `json:"edges"`
	SharedResources []WorkspaceSharedResource `json:"shared_resources"`
}
