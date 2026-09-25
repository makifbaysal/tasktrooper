// Package workflowtest builds, in Go, the exact same roles/task-types/
// workflow-stages that migration 143 writes for a fresh install with default
// settings. It exists for two reasons: unit tests across the codebase need a
// fake port.WorkflowReader/port.RoleResolver without a database, and the
// migration's own parity test asserts the migrated rows equal Default()
// column for column — so this file and the migration must be changed
// together, never one without the other.
package workflowtest

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// AgentID is a stable, deterministic id for a built-in agent name — the same
// value every call and every process, so a test can assert
// workflowtest.AgentID("qa-agent") without threading an id through a fixture.
func AgentID(name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("tasktrooper-agent:"+name))
}

// RoleID is the deterministic id for a built-in role key, mirrored the same
// way as AgentID.
func RoleID(key string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("tasktrooper-role:"+key))
}

func ref(key domain.BehaviourKey, kv ...string) domain.BehaviourRef {
	var params map[string]string
	if len(kv) > 0 {
		params = make(map[string]string, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			params[kv[i]] = kv[i+1]
		}
	}
	return domain.BehaviourRef{Key: key, Params: params}
}

func assignment(agentName string, areas ...string) domain.RoleAssignment {
	return domain.RoleAssignment{AgentID: AgentID(agentName), AgentName: agentName, Areas: areas}
}

// Fixture is everything Default builds.
type Fixture struct {
	Roles     []domain.AgentRole
	Purposes  []domain.RolePurpose
	TaskTypes []domain.TaskTypeDef
	Workflows map[domain.TaskType]domain.Workflow
}

const (
	taskTypeTask      = domain.TaskType("task")
	taskTypeAnaliz    = domain.TaskType("analiz")
	taskTypeBug       = domain.TaskType("bug")
	taskTypeTechnical = domain.TaskType("technical")
)

// Default builds the roles, task types and per-type workflow stages the
// migration seeds for a fresh install with every analiz-assignee area left on
// its default (system-architect) — i.e. the "all default" migration test
// scenario. RequiredAnalizTools is not imported here (workflow/domain would
// then have to depend on the tool_policy list at exactly the snapshot the
// migration was written against); it is copied verbatim and the migration
// test asserts the two still agree.
func Default() Fixture {
	developerID := RoleID("developer")
	analystID := RoleID("analyst")
	architectID := RoleID("architect")
	qaID := RoleID("qa")
	pmID := RoleID("product_manager")
	releaseID := RoleID("release")

	roles := []domain.AgentRole{
		{
			ID: developerID, Key: "developer", Name: "Developer",
			Description: "Writes the implementation for task/bug/technical work.",
			Assignments: []domain.RoleAssignment{
				assignment("backend-developer", "backend"),
				assignment("frontend-developer", "frontend"),
				assignment("mobile-developer", "mobile"),
			},
		},
		{
			ID: analystID, Key: "analyst", Name: "Analyst",
			Description:   "Produces the spec and implementation plan for an analiz task.",
			RequiredTools: append([]string{}, requiredAnalizToolsSnapshot...),
			// "All default" scenario: every area's analiz_assignee setting was
			// unset, so all three resolve to system-architect, which is what
			// groups them into one areas=NULL assignment (see §3).
			Assignments: []domain.RoleAssignment{assignment("system-architect")},
		},
		{
			ID: architectID, Key: "architect", Name: "Architect",
			Description: "Reviews diffs and designs rather than writing code.",
			Assignments: []domain.RoleAssignment{assignment("system-architect")},
		},
		{
			ID: qaID, Key: "qa", Name: "QA",
			Description: "Tests the product as a black box before UAT.",
			Assignments: []domain.RoleAssignment{assignment("qa-agent")},
		},
		{
			ID: pmID, Key: "product_manager", Name: "Product Manager",
			Description: "Verifies acceptance criteria against QA's evidence.",
			Assignments: []domain.RoleAssignment{assignment("product-manager")},
		},
		{
			// No assignment: the release-engineer agent is created by the
			// catalog sync at boot, after migrations have run.
			ID: releaseID, Key: "release", Name: "Release Engineer",
			Description: "Merges signed-off work, ships it, verifies production after the deploy and rolls back what breaks.",
		},
	}

	purposes := []domain.RolePurpose{
		{Purpose: domain.PurposeSystemTaskAssignee, RoleID: &developerID},
		{Purpose: domain.PurposeRepoProfiler, RoleID: &architectID},
	}

	taskTypes := []domain.TaskTypeDef{
		{Key: taskTypeTask, Label: "Task", KeyPrefix: "T", Position: 0, IsDefault: true, BuiltIn: true, AssigneeMode: domain.AssigneeModeNone},
		{
			Key: taskTypeAnaliz, Label: "Analysis", KeyPrefix: "A", Position: 1, BuiltIn: true,
			AssigneeRoleID: &analystID, AssigneeMode: domain.AssigneeModeOverride,
			Behaviours: []domain.BehaviourRef{
				ref(domain.BehaviourNoWorkspaceWrites),
				ref(domain.BehaviourRequireRepoGrounding),
			},
		},
		{Key: taskTypeBug, Label: "Bug", KeyPrefix: "B", Position: 2, IsDefect: true, BuiltIn: true, AssigneeMode: domain.AssigneeModeNone},
		{Key: taskTypeTechnical, Label: "Technical", KeyPrefix: "TC", Position: 3, BuiltIn: true, AssigneeMode: domain.AssigneeModeNone},
	}

	workflows := map[domain.TaskType]domain.Workflow{}
	for i, t := range taskTypes {
		workflows[t.Key] = domain.Workflow{Type: taskTypes[i], Stages: stagesFor(t.Key)}
	}

	return Fixture{Roles: roles, Purposes: purposes, TaskTypes: taskTypes, Workflows: workflows}
}

// requiredAnalizToolsSnapshot mirrors domain.RequiredAnalizTools at the time
// migration 143 was written (CodeExplorationTools ++ AnalizDocumentTools ++
// BoardProgressTools ++ the four extras) — see tool_policy.go.
var requiredAnalizToolsSnapshot = []string{
	"codebase_search", "grep_code", "get_repo_tree", "get_symbol_skeleton", "expand_symbol_context", "read_file",
	"add_task_document", "update_task_document",
	"claim_board_task", "move_board_task",
	"add_task_comment", "list_task_comments", "list_task_documents", "create_board_task",
}

// ---- workflow.Reader / RoleResolver fakes ----

// reader implements port.WorkflowReader over a Fixture, for tests that need a
// fake without a database.
type reader struct{ f Fixture }

// Reader returns a port.WorkflowReader-shaped fake backed by f. It is typed
// as an unexported struct returned as an interface{} consumer packages assert
// against their own copy of port.WorkflowReader, avoiding a dependency from
// this test-only package back onto port for callers that only need the
// domain data (Fixture) directly.
func (f Fixture) Reader() interface {
	Workflow(ctx context.Context, taskType domain.TaskType) (domain.Workflow, error)
	DefaultTaskType(ctx context.Context) (domain.TaskType, error)
	DefectTaskType(ctx context.Context) (domain.TaskType, error)
	TaskTypeExists(ctx context.Context, taskType domain.TaskType) (bool, error)
	KeyPrefix(ctx context.Context, taskType domain.TaskType) (string, error)
} {
	return reader{f: f}
}

func (r reader) Workflow(_ context.Context, taskType domain.TaskType) (domain.Workflow, error) {
	if wf, ok := r.f.Workflows[taskType]; ok {
		return wf, nil
	}
	for _, t := range r.f.TaskTypes {
		if t.IsDefault {
			return r.f.Workflows[t.Key], nil
		}
	}
	return domain.Workflow{}, errNotFound
}

func (r reader) DefaultTaskType(context.Context) (domain.TaskType, error) {
	for _, t := range r.f.TaskTypes {
		if t.IsDefault {
			return t.Key, nil
		}
	}
	return "", errNotFound
}

func (r reader) DefectTaskType(context.Context) (domain.TaskType, error) {
	for _, t := range r.f.TaskTypes {
		if t.IsDefect {
			return t.Key, nil
		}
	}
	return "", errNotFound
}

func (r reader) TaskTypeExists(_ context.Context, taskType domain.TaskType) (bool, error) {
	_, ok := r.f.Workflows[taskType]
	return ok, nil
}

func (r reader) KeyPrefix(ctx context.Context, taskType domain.TaskType) (string, error) {
	wf, err := r.Workflow(ctx, taskType)
	if err != nil {
		return "", err
	}
	return wf.Type.KeyPrefix, nil
}

// resolver implements port.RoleResolver over a Fixture.
type resolver struct{ f Fixture }

func (f Fixture) Resolver() interface {
	AgentForRole(ctx context.Context, roleID uuid.UUID, area string) (*uuid.UUID, error)
	AgentForPurpose(ctx context.Context, purpose domain.RolePurposeKey, area string) (*uuid.UUID, error)
	AgentArea(ctx context.Context, agentID uuid.UUID) string
	AssigneeForNewTask(ctx context.Context, taskType domain.TaskType, area string, requested *uuid.UUID) (*uuid.UUID, error)
} {
	return resolver{f: f}
}

func (r resolver) roleByID(id uuid.UUID) (domain.AgentRole, bool) {
	for _, role := range r.f.Roles {
		if role.ID == id {
			return role, true
		}
	}
	return domain.AgentRole{}, false
}

func (r resolver) AgentForRole(_ context.Context, roleID uuid.UUID, area string) (*uuid.UUID, error) {
	role, ok := r.roleByID(roleID)
	if !ok {
		return nil, nil
	}
	var anyMatch *uuid.UUID
	for _, a := range role.Assignments {
		if a.Areas == nil {
			if anyMatch == nil {
				id := a.AgentID
				anyMatch = &id
			}
			continue
		}
		for _, ar := range a.Areas {
			if ar == area {
				id := a.AgentID
				return &id, nil
			}
		}
	}
	return anyMatch, nil
}

func (r resolver) AgentForPurpose(ctx context.Context, purpose domain.RolePurposeKey, area string) (*uuid.UUID, error) {
	for _, p := range r.f.Purposes {
		if p.Purpose == purpose && p.RoleID != nil {
			return r.AgentForRole(ctx, *p.RoleID, area)
		}
	}
	return nil, nil
}

func (r resolver) AgentArea(_ context.Context, agentID uuid.UUID) string {
	areas := map[string]bool{}
	for _, role := range r.f.Roles {
		for _, a := range role.Assignments {
			if a.AgentID != agentID || a.Areas == nil {
				continue
			}
			for _, ar := range a.Areas {
				areas[ar] = true
			}
		}
	}
	if len(areas) != 1 {
		return ""
	}
	for ar := range areas {
		return ar
	}
	return ""
}

func (r resolver) AssigneeForNewTask(ctx context.Context, taskType domain.TaskType, area string, requested *uuid.UUID) (*uuid.UUID, error) {
	wf, ok := r.f.Workflows[taskType]
	if !ok {
		return requested, nil
	}
	switch wf.Type.AssigneeMode {
	case domain.AssigneeModeOverride:
		if wf.Type.AssigneeRoleID != nil {
			if agent, _ := r.AgentForRole(ctx, *wf.Type.AssigneeRoleID, area); agent != nil {
				return agent, nil
			}
		}
		return requested, nil
	case domain.AssigneeModeDefault:
		if requested != nil {
			return requested, nil
		}
		if wf.Type.AssigneeRoleID == nil {
			return nil, nil
		}
		return r.AgentForRole(ctx, *wf.Type.AssigneeRoleID, area)
	default:
		return requested, nil
	}
}

type notFoundError struct{}

func (notFoundError) Error() string { return "workflowtest: not found" }

var errNotFound error = notFoundError{}
