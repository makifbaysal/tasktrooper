package workflow

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ---- port.RoleResolver ----

// AgentForRole resolves roleID's assignment for area: an assignment whose
// Areas contains area wins over one with no Areas (any area — a catalog
// `areas: []` is stored as '{}', not NULL), which wins over one covering a
// domain.AreaFallbacks area. Within a tier, the first match wins —
// RoleStore.List/Get order assignments by priority then created_at, so
// "first" already encodes both tie-breaks. nil, nil when the role is unknown
// to this snapshot or has no assignment covering area.
func (s *Service) AgentForRole(ctx context.Context, roleID uuid.UUID, area string) (*uuid.UUID, error) {
	snap, err := s.getSnapshot()
	if err != nil {
		return nil, err
	}
	role, ok := snap.roles[roleID]
	if !ok {
		return nil, nil
	}
	var anyAreaMatch *uuid.UUID
	for _, a := range role.Assignments {
		if len(a.Areas) == 0 {
			if anyAreaMatch == nil {
				id := a.AgentID
				anyAreaMatch = &id
			}
			continue
		}
		if containsArea(a.Areas, area) {
			id := a.AgentID
			return &id, nil
		}
	}
	if anyAreaMatch != nil {
		return anyAreaMatch, nil
	}
	for _, fallback := range domain.AreaFallbacks(area) {
		for _, a := range role.Assignments {
			if containsArea(a.Areas, fallback) {
				id := a.AgentID
				return &id, nil
			}
		}
	}
	return nil, nil
}

func (s *Service) AgentForPurpose(ctx context.Context, purpose domain.RolePurposeKey, area string) (*uuid.UUID, error) {
	snap, err := s.getSnapshot()
	if err != nil {
		return nil, err
	}
	roleID, ok := snap.purposes[purpose]
	if !ok || roleID == nil {
		return nil, nil
	}
	return s.AgentForRole(ctx, *roleID, area)
}

// AgentArea is the single area of agentID's own area-scoped assignment(s),
// across every role it holds — the union of every Areas entry from every
// assignment that names a specific area. "" when the agent
// holds no area-scoped assignment, or holds ones naming more than one
// distinct area: profileKindForAgent used to substring-match an agent's NAME
// for exactly one area, so an agent this cannot answer for unambiguously gets
// the same "" a name match would have failed to produce a guess for.
func (s *Service) AgentArea(ctx context.Context, agentID uuid.UUID) string {
	if areas := s.AgentAreas(ctx, agentID); len(areas) == 1 {
		return areas[0]
	}
	return ""
}

func (s *Service) AgentAreas(ctx context.Context, agentID uuid.UUID) []string {
	snap, err := s.getSnapshot()
	if err != nil {
		return nil
	}
	var areas []string
	for _, role := range snap.roles {
		for _, a := range role.Assignments {
			if a.AgentID != agentID {
				continue
			}
			for _, ar := range a.Areas {
				if !slices.Contains(areas, ar) {
					areas = append(areas, ar)
				}
			}
		}
	}
	slices.Sort(areas)
	return areas
}

// AssigneeForNewTask resolves CreateTask's assignee for a new task of
// taskType in area, given what the caller requested (nil/empty means
// "nothing requested"). Reads the type's assignee_mode:
//
//   - none: requested stands, unchanged — today's behaviour for task/bug.
//   - default: requested stands if set; otherwise the role's agent for area.
//   - override: the role's agent for area always wins when the role has one
//     there; otherwise requested stands — today's resolveAnalizAssignee
//     behaviour for analiz.
func (s *Service) AssigneeForNewTask(ctx context.Context, taskType domain.TaskType, area string, requested *uuid.UUID) (*uuid.UUID, error) {
	wf, err := s.Workflow(ctx, taskType)
	if err != nil {
		return nil, err
	}
	switch wf.Type.AssigneeMode {
	case domain.AssigneeModeOverride:
		if wf.Type.AssigneeRoleID != nil {
			if agent, err := s.AgentForRole(ctx, *wf.Type.AssigneeRoleID, area); err != nil {
				return nil, err
			} else if agent != nil {
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
		return s.AgentForRole(ctx, *wf.Type.AssigneeRoleID, area)
	default: // AssigneeModeNone
		return requested, nil
	}
}

// AgentHoldsRole reports whether agentID has any assignment, in any area, on
// the role keyed roleKey. Read from the snapshot, so the board can ask it on
// every assignee write without a query.
func (s *Service) AgentHoldsRole(ctx context.Context, agentID uuid.UUID, roleKey string) (bool, error) {
	snap, err := s.getSnapshot()
	if err != nil {
		return false, err
	}
	for _, r := range snap.roles {
		if r.Key != roleKey {
			continue
		}
		for _, a := range r.Assignments {
			if a.AgentID == agentID {
				return true, nil
			}
		}
	}
	return false, nil
}

// RoleByKey resolves a role from its key against the snapshot — the extra
// surface the create_board_task tool's optional assignee_role argument needs
// beyond port.RoleResolver's id-keyed methods, so a task-creation call never
// pays a query for it either.
func (s *Service) RoleByKey(ctx context.Context, key string) (domain.AgentRole, error) {
	snap, err := s.getSnapshot()
	if err != nil {
		return domain.AgentRole{}, err
	}
	for _, r := range snap.roles {
		if r.Key == key {
			return r, nil
		}
	}
	return domain.AgentRole{}, fmt.Errorf("unknown role %q", key)
}

func containsArea(areas []string, area string) bool {
	for _, a := range areas {
		if a == area {
			return true
		}
	}
	return false
}
