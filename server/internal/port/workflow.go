package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// RoleStore is the admin CRUD surface for roles; the engine's hot path reads
// go through RoleResolver, an in-memory snapshot.
type RoleStore interface {
	List(ctx context.Context) ([]domain.AgentRole, error)
	Get(ctx context.Context, id uuid.UUID) (domain.AgentRole, error)
	Create(ctx context.Context, role domain.AgentRole) (domain.AgentRole, error)
	Update(ctx context.Context, role domain.AgentRole) (domain.AgentRole, error)
	Delete(ctx context.Context, id uuid.UUID) error
	SetAssignments(ctx context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment) error
	ListAssignmentsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.AgentRole, error)
	// The agent-centric write of the same relationship SetAssignments writes
	// role-centrically; touches no other agent's assignments.
	SetAgentRoles(ctx context.Context, agentID uuid.UUID, roles []domain.AgentRoleMembership) error
	ListPurposes(ctx context.Context) ([]domain.RolePurpose, error)
	SetPurpose(ctx context.Context, purpose domain.RolePurposeKey, roleID *uuid.UUID) error
}

// WorkflowStore is the admin CRUD surface for task types and their per-column
// workflow stages.
type WorkflowStore interface {
	ListTaskTypes(ctx context.Context) ([]domain.TaskTypeDef, error)
	GetTaskType(ctx context.Context, key domain.TaskType) (domain.TaskTypeDef, error)
	// cloneFrom, when non-empty, copies the named type's stages onto the new
	// type before returning.
	CreateTaskType(ctx context.Context, def domain.TaskTypeDef, cloneFrom domain.TaskType) (domain.TaskTypeDef, error)
	UpdateTaskType(ctx context.Context, def domain.TaskTypeDef) (domain.TaskTypeDef, error)
	DeleteTaskType(ctx context.Context, key domain.TaskType) error
	ListStages(ctx context.Context, taskType domain.TaskType) ([]domain.WorkflowStage, error)
	ReplaceStages(ctx context.Context, taskType domain.TaskType, stages []domain.WorkflowStage) error
	LoadAll(ctx context.Context) ([]domain.Workflow, error)
	// A stage with zero behaviours is the same as no stage at all, so it does
	// not block a column removal.
	ColumnHasBehaviourStages(ctx context.Context, slug string) (bool, error)
}

// WorkflowReader is the only workflow surface the engine's dispatch/gate path
// sees — a read against an in-memory snapshot, never the database. An unknown
// task type returns the DEFAULT type's workflow rather than an error, and an
// empty cache makes every method error rather than silently answer nothing
// (gates fail CLOSED).
type WorkflowReader interface {
	Workflow(ctx context.Context, taskType domain.TaskType) (domain.Workflow, error)
	DefaultTaskType(ctx context.Context) (domain.TaskType, error)
	DefectTaskType(ctx context.Context) (domain.TaskType, error)
	TaskTypeExists(ctx context.Context, taskType domain.TaskType) (bool, error)
	KeyPrefix(ctx context.Context, taskType domain.TaskType) (string, error)
}

// RoleResolver answers "which agent holds this role/purpose for this area",
// backed by the same in-memory snapshot WorkflowReader reads.
type RoleResolver interface {
	// An area-scoped assignment wins over one with no Areas; ties break on
	// Priority then on assignment order. nil when the role covers no area.
	AgentForRole(ctx context.Context, roleID uuid.UUID, area string) (*uuid.UUID, error)
	AgentForPurpose(ctx context.Context, purpose domain.RolePurposeKey, area string) (*uuid.UUID, error)
	// The single area of the agent's own area-scoped assignment across any
	// role; "" when the agent has none or covers more than one.
	AgentArea(ctx context.Context, agentID uuid.UUID) string
	// Every area the agent's area-scoped assignments name, across any role;
	// nil when it holds none, i.e. it is not tied to one area.
	AgentAreas(ctx context.Context, agentID uuid.UUID) []string
	// Reads the type's assignee_mode: none returns requested unchanged;
	// default fills only when requested is nil; override prefers the role's
	// agent for area, falling back to requested.
	AssigneeForNewTask(ctx context.Context, taskType domain.TaskType, area string, requested *uuid.UUID) (*uuid.UUID, error)
}
