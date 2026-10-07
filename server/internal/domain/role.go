package domain

import "github.com/google/uuid"

// AgentRole is a named responsibility ("developer", "qa") that one or more
// agents may be assigned to, scoped per repo area. It replaces the hardcoded
// agent names the engine used to branch on: it asks "who holds this role for
// this area", never "is your name backend-developer".
type AgentRole struct {
	ID            uuid.UUID
	Key           string
	Name          string
	Description   string
	RequiredTools []string
	Assignments   []RoleAssignment
	Purposes      []RolePurposeKey
}

// RoleKeyProductManager is the lead agent's role. Its holders work the board
// through column subscriptions (pm_uat) and chats, never as a task's assignee.
const RoleKeyProductManager = "product_manager"

// RoleAssignment binds one agent to a role, optionally narrowed to the repo
// areas it covers; areas nil means "any area".
type RoleAssignment struct {
	AgentID uuid.UUID
	// AgentName is read-only, joined in for display; writing an assignment
	// never accepts it.
	AgentName string
	Areas     []string
	Priority  int
}

// AgentRoleMembership is one row of PUT /v1/agents/:agentId/roles — the
// agent-centric write of the same relationship AgentRole.Assignments reads
// role-centrically.
type AgentRoleMembership struct {
	RoleID uuid.UUID
	Areas  []string
}

// RolePurposeKey names a system hook that resolves to a role rather than to one
// hardcoded agent — "who does the control plane itself hand a task to", as
// opposed to a workflow stage's assignee, which a human configures per task
// type.
type RolePurposeKey string

const (
	// PurposeSystemTaskAssignee is who CreateWorkflowSetupTask, deploy,
	// repodocs and prodops hand their own system-opened tasks to.
	PurposeSystemTaskAssignee RolePurposeKey = "system_task_assignee"
	// PurposeRepoProfiler is who repoprofile hands a repository-profile refresh
	// to.
	PurposeRepoProfiler RolePurposeKey = "repo_profiler"
)

// ValidRolePurposeKey reports whether a purpose is one the schema's CHECK
// constraint accepts.
func ValidRolePurposeKey(p RolePurposeKey) bool {
	switch p {
	case PurposeSystemTaskAssignee, PurposeRepoProfiler:
		return true
	default:
		return false
	}
}

// RolePurpose is one row of the purpose → role mapping. RoleID nil means the
// purpose has no role assigned yet, which is a valid (if unhelpful) state:
// AgentForPurpose then resolves nobody rather than guessing.
type RolePurpose struct {
	Purpose RolePurposeKey
	RoleID  *uuid.UUID
}

// ValidRoleKey mirrors the roles.key CHECK constraint: lowercase, starting with
// a letter, the rest letters/digits/underscore.
func ValidRoleKey(key string) bool {
	if key == "" {
		return false
	}
	if key[0] < 'a' || key[0] > 'z' {
		return false
	}
	for _, r := range key {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}
