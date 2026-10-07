package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ValidationError carries the per-field problems behind a rejected
// role/task-type/workflow write, so a transport can answer 422 with the list
// instead of one flattened string.
type ValidationError struct {
	Problems []StageProblem
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "validation failed"
	}
	return e.Problems[0].Error()
}

// ErrPrefixChangeWithTasks is UpdateTaskType's refusal to change key_prefix
// once the type has tasks: every existing task's key was rendered with the
// old prefix, and changing it would make a stored key lie about its own type.
var ErrPrefixChangeWithTasks = errors.New("task type key_prefix may not change while the type has tasks")

// ErrTaskTypeInUse is DeleteTaskType's refusal to delete a type that still
// has tasks, or is the board's default type.
var ErrTaskTypeInUse = errors.New("task type may not be deleted")

func (s *Service) columnSet(ctx context.Context) (map[string]bool, error) {
	if s.board == nil {
		return nil, nil
	}
	cols, err := s.board.ListColumns(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(cols))
	for _, c := range cols {
		out[c.Slug] = true
	}
	return out, nil
}

// ---- Roles ----

func (s *Service) ListRoles(ctx context.Context) ([]domain.AgentRole, error) {
	return s.roles.List(ctx)
}

func (s *Service) GetRole(ctx context.Context, id uuid.UUID) (domain.AgentRole, error) {
	return s.roles.Get(ctx, id)
}

func (s *Service) CreateRole(ctx context.Context, role domain.AgentRole) (domain.AgentRole, error) {
	if !domain.ValidRoleKey(role.Key) {
		return domain.AgentRole{}, fmt.Errorf("invalid role key %q", role.Key)
	}
	created, err := s.roles.Create(ctx, role)
	if err != nil {
		return domain.AgentRole{}, err
	}
	_ = s.Reload(ctx)
	return created, nil
}

func (s *Service) UpdateRole(ctx context.Context, role domain.AgentRole) (domain.AgentRole, error) {
	updated, err := s.roles.Update(ctx, role)
	if err != nil {
		return domain.AgentRole{}, err
	}
	_ = s.Reload(ctx)
	return updated, nil
}

func (s *Service) DeleteRole(ctx context.Context, id uuid.UUID) error {
	if err := s.roles.Delete(ctx, id); err != nil {
		return err
	}
	return s.Reload(ctx)
}

func (s *Service) SetRoleAssignments(ctx context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment) error {
	if err := s.roles.SetAssignments(ctx, roleID, assignments); err != nil {
		return err
	}
	return s.Reload(ctx)
}

// AgentCatalog is the narrow slice of catalog.Service a role-assignment write
// needs: look an agent up by id to check its tool policy, and widen that
// policy once the user has confirmed the grant. Mirrors
// application/settings.AgentCatalog — both packages check the same kind of
// thing against two different required-tool lists (RequiredAnalizTools vs. a
// role's own RequiredTools).
type AgentCatalog interface {
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
	UpdateAgent(ctx context.Context, id uuid.UUID, req domain.UpdateAgentRequest) (domain.Agent, error)
}

// SetAgentCatalog wires the agent lookup SetRoleAssignmentsChecked /
// SetAgentRolesChecked need. Nil (the pre-wiring default) makes an assignment
// to a role with RequiredTools always refuse for a missing-tools reason it
// cannot actually check.
func (s *Service) SetAgentCatalog(agents AgentCatalog) { s.agentCatalog = agents }

// MissingToolsError carries the per-agent missing-tool lists behind a
// SetRoleAssignmentsChecked/SetAgentRolesChecked refusal, mirroring
// domain.MissingAnalizToolsError's shape for the same 422 pattern.
type MissingToolsError struct {
	Missing map[string][]string // agent id (string) -> missing tool names
}

func (e *MissingToolsError) Error() string { return "agent is missing required role tools" }

// SetRoleAssignmentsChecked is SetRoleAssignments with the tool-grant gate:
// every assignment naming an agent that does not already carry roleID's
// RequiredTools is refused (returning *MissingToolsError) unless
// confirmGrantTools is set, in which case the missing tools are appended to
// that agent's policy before the assignments are saved. Returns which tools
// were granted to which agent (by id), if any.
func (s *Service) SetRoleAssignmentsChecked(ctx context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment, confirmGrantTools bool) (map[string][]string, error) {
	role, err := s.roles.Get(ctx, roleID)
	if err != nil {
		return nil, err
	}
	granted, err := s.checkAndGrant(ctx, role.RequiredTools, assignments, confirmGrantTools)
	if err != nil {
		return nil, err
	}
	if err := s.SetRoleAssignments(ctx, roleID, assignments); err != nil {
		return nil, err
	}
	return granted, nil
}

func (s *Service) checkAndGrant(ctx context.Context, required []string, assignments []domain.RoleAssignment, confirmGrantTools bool) (map[string][]string, error) {
	if len(required) == 0 || s.agentCatalog == nil {
		return nil, nil
	}
	missingByAgent := map[string][]string{}
	grantedByAgent := map[string][]string{}
	for _, a := range assignments {
		agent, err := s.agentCatalog.GetAgent(ctx, a.AgentID)
		if err != nil {
			return nil, fmt.Errorf("get agent %s: %w", a.AgentID, err)
		}
		missing := domain.MissingTools(agent.ToolPolicy, required)
		if len(missing) == 0 {
			continue
		}
		if !confirmGrantTools {
			missingByAgent[agent.ID.String()] = missing
			continue
		}
		policy := agent.ToolPolicy
		policy.AllowTools = append(append([]string(nil), policy.AllowTools...), missing...)
		if _, err := s.agentCatalog.UpdateAgent(ctx, agent.ID, domain.UpdateAgentRequest{
			Name: agent.Name, Description: agent.Description, SubagentType: agent.SubagentType,
			SystemPrompt: agent.SystemPrompt, ProviderType: agent.ProviderType, Model: agent.Model,
			ModelHeavy: agent.ModelHeavy, MaxTurns: agent.MaxTurns, Effort: agent.Effort,
			ToolPolicy: policy, Enabled: agent.Enabled, SelfEvolutionEnabled: agent.SelfEvolutionEnabled,
		}); err != nil {
			return nil, fmt.Errorf("grant tools to agent %s: %w", agent.ID, err)
		}
		grantedByAgent[agent.ID.String()] = missing
	}
	if len(missingByAgent) > 0 {
		return nil, &MissingToolsError{Missing: missingByAgent}
	}
	if len(grantedByAgent) == 0 {
		return nil, nil
	}
	return grantedByAgent, nil
}

func (s *Service) ListAssignmentsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.AgentRole, error) {
	return s.roles.ListAssignmentsByAgent(ctx, agentID)
}

// SetAgentRoles replaces every role membership agentID holds, unchecked (see
// SetAgentRolesChecked for the tool-grant gate PUT /v1/agents/:agentId/roles
// actually uses).
func (s *Service) SetAgentRoles(ctx context.Context, agentID uuid.UUID, roles []domain.AgentRoleMembership) error {
	if err := s.roles.SetAgentRoles(ctx, agentID, roles); err != nil {
		return err
	}
	return s.Reload(ctx)
}

// SetAgentRolesChecked is SetAgentRoles with the same tool-grant gate
// SetRoleAssignmentsChecked applies, checked against the UNION of every named
// role's RequiredTools (an agent taking on two roles at once needs both
// roles' tools, checked and granted together rather than in two separate
// round trips that could each half-succeed).
func (s *Service) SetAgentRolesChecked(ctx context.Context, agentID uuid.UUID, roles []domain.AgentRoleMembership, confirmGrantTools bool) (map[string][]string, error) {
	if s.agentCatalog == nil || len(roles) == 0 {
		if err := s.SetAgentRoles(ctx, agentID, roles); err != nil {
			return nil, err
		}
		return nil, nil
	}
	agent, err := s.agentCatalog.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent %s: %w", agentID, err)
	}
	seen := map[string]bool{}
	var required []string
	for _, m := range roles {
		role, err := s.roles.Get(ctx, m.RoleID)
		if err != nil {
			return nil, err
		}
		for _, t := range role.RequiredTools {
			if !seen[t] {
				seen[t] = true
				required = append(required, t)
			}
		}
	}
	var granted map[string][]string
	if missing := domain.MissingTools(agent.ToolPolicy, required); len(missing) > 0 {
		if !confirmGrantTools {
			return nil, &MissingToolsError{Missing: map[string][]string{agent.ID.String(): missing}}
		}
		policy := agent.ToolPolicy
		policy.AllowTools = append(append([]string(nil), policy.AllowTools...), missing...)
		if _, err := s.agentCatalog.UpdateAgent(ctx, agent.ID, domain.UpdateAgentRequest{
			Name: agent.Name, Description: agent.Description, SubagentType: agent.SubagentType,
			SystemPrompt: agent.SystemPrompt, ProviderType: agent.ProviderType, Model: agent.Model,
			ModelHeavy: agent.ModelHeavy, MaxTurns: agent.MaxTurns, Effort: agent.Effort,
			ToolPolicy: policy, Enabled: agent.Enabled, SelfEvolutionEnabled: agent.SelfEvolutionEnabled,
		}); err != nil {
			return nil, fmt.Errorf("grant tools to agent %s: %w", agent.ID, err)
		}
		granted = map[string][]string{agent.ID.String(): missing}
	}
	if err := s.SetAgentRoles(ctx, agentID, roles); err != nil {
		return nil, err
	}
	return granted, nil
}

func (s *Service) ListPurposes(ctx context.Context) ([]domain.RolePurpose, error) {
	return s.roles.ListPurposes(ctx)
}

func (s *Service) SetPurpose(ctx context.Context, purpose domain.RolePurposeKey, roleID *uuid.UUID) error {
	if !domain.ValidRolePurposeKey(purpose) {
		return fmt.Errorf("unknown purpose %q", purpose)
	}
	if err := s.roles.SetPurpose(ctx, purpose, roleID); err != nil {
		return err
	}
	return s.Reload(ctx)
}

// ---- Task types ----

func (s *Service) ListTaskTypes(ctx context.Context) ([]domain.TaskTypeDef, error) {
	return s.workflows.ListTaskTypes(ctx)
}

func (s *Service) GetTaskType(ctx context.Context, key domain.TaskType) (domain.TaskTypeDef, error) {
	return s.workflows.GetTaskType(ctx, key)
}

func (s *Service) CreateTaskType(ctx context.Context, def domain.TaskTypeDef, cloneFrom domain.TaskType) (domain.TaskTypeDef, error) {
	if !domain.ValidRoleKey(string(def.Key)) {
		return domain.TaskTypeDef{}, fmt.Errorf("invalid task type key %q", def.Key)
	}
	if !domain.ValidAssigneeMode(def.AssigneeMode) {
		def.AssigneeMode = domain.AssigneeModeNone
	}
	if err := s.refuseUnassignableRole(def.AssigneeRoleID); err != nil {
		return domain.TaskTypeDef{}, err
	}
	cols, err := s.columnSet(ctx)
	if err != nil {
		return domain.TaskTypeDef{}, err
	}
	if problems := ValidateTypeBehaviours(def.Behaviours, cols); len(problems) > 0 {
		return domain.TaskTypeDef{}, &ValidationError{Problems: problems}
	}
	created, err := s.workflows.CreateTaskType(ctx, def, cloneFrom)
	if err != nil {
		return domain.TaskTypeDef{}, err
	}
	_ = s.Reload(ctx)
	return created, nil
}

func (s *Service) UpdateTaskType(ctx context.Context, def domain.TaskTypeDef) (domain.TaskTypeDef, error) {
	if !domain.ValidAssigneeMode(def.AssigneeMode) {
		def.AssigneeMode = domain.AssigneeModeNone
	}
	if err := s.refuseUnassignableRole(def.AssigneeRoleID); err != nil {
		return domain.TaskTypeDef{}, err
	}
	cols, err := s.columnSet(ctx)
	if err != nil {
		return domain.TaskTypeDef{}, err
	}
	if problems := ValidateTypeBehaviours(def.Behaviours, cols); len(problems) > 0 {
		return domain.TaskTypeDef{}, &ValidationError{Problems: problems}
	}
	current, err := s.workflows.GetTaskType(ctx, def.Key)
	if err != nil {
		return domain.TaskTypeDef{}, err
	}
	if current.KeyPrefix != def.KeyPrefix && current.TaskCount > 0 {
		return domain.TaskTypeDef{}, fmt.Errorf("%w: %s", ErrPrefixChangeWithTasks, def.Key)
	}
	updated, err := s.workflows.UpdateTaskType(ctx, def)
	if err != nil {
		return domain.TaskTypeDef{}, err
	}
	_ = s.Reload(ctx)
	return updated, nil
}

func (s *Service) refuseUnassignableRole(roleID *uuid.UUID) error {
	if roleID == nil {
		return nil
	}
	snap, err := s.getSnapshot()
	if err != nil {
		return err
	}
	if role, ok := snap.roles[*roleID]; ok && role.Key == domain.RoleKeyProductManager {
		return domain.ErrAssigneeNotAssignable
	}
	return nil
}

func (s *Service) DeleteTaskType(ctx context.Context, key domain.TaskType) error {
	current, err := s.workflows.GetTaskType(ctx, key)
	if err != nil {
		return err
	}
	if current.IsDefault || current.TaskCount > 0 {
		return fmt.Errorf("%w: %s", ErrTaskTypeInUse, key)
	}
	if err := s.workflows.DeleteTaskType(ctx, key); err != nil {
		return err
	}
	return s.Reload(ctx)
}

// ---- Workflow stages ----

func (s *Service) ListStages(ctx context.Context, taskType domain.TaskType) ([]domain.WorkflowStage, error) {
	return s.workflows.ListStages(ctx, taskType)
}

func (s *Service) ReplaceStages(ctx context.Context, taskType domain.TaskType, stages []domain.WorkflowStage) error {
	cols, err := s.columnSet(ctx)
	if err != nil {
		return err
	}
	if problems := ValidateStages(stages, cols); len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	if err := s.workflows.ReplaceStages(ctx, taskType, stages); err != nil {
		return err
	}
	return s.Reload(ctx)
}

// ListWorkflows returns every task type's full workflow — GET /v1/workflows.
func (s *Service) ListWorkflows(ctx context.Context) ([]domain.Workflow, error) {
	return s.workflows.LoadAll(ctx)
}

// ColumnHasBehaviourStages satisfies workspace.WorkflowStageChecker.
func (s *Service) ColumnHasBehaviourStages(ctx context.Context, slug string) (bool, error) {
	return s.workflows.ColumnHasBehaviourStages(ctx, slug)
}
