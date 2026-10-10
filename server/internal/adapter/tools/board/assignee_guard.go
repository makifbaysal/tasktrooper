package board

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (kit *ToolKit) agentByID(ctx context.Context, id uuid.UUID) (domain.Agent, bool) {
	if kit.Team == nil {
		return domain.Agent{}, false
	}
	agents, err := kit.Team.ListAgents(ctx)
	if err != nil {
		return domain.Agent{}, false
	}
	for _, a := range agents {
		if a.ID == id {
			return a, true
		}
	}
	return domain.Agent{}, false
}

// disabledAssigneeRefusal is the tool error for assigning to a switched-off
// agent, or "" when the agent is enabled or unknown to the roster (the task
// manager rejects unknown ids itself).
func (kit *ToolKit) disabledAssigneeRefusal(ctx context.Context, id uuid.UUID) string {
	agent, ok := kit.agentByID(ctx, id)
	if !ok || agent.Enabled {
		return ""
	}
	return assigneeDisabledKey.Render(assigneeDisabledInput{Agents: []string{agent.Name}})
}

// disabledRoleRefusal is the tool error for an assignee_role whose resolved
// agent is switched off, naming every disabled agent that holds the role.
func (kit *ToolKit) disabledRoleRefusal(ctx context.Context, roleKey string, resolved uuid.UUID) string {
	agent, ok := kit.agentByID(ctx, resolved)
	if !ok || agent.Enabled {
		return ""
	}
	names := []string{agent.Name}
	if lookup, ok := kit.Roles.(roleByKeyLookup); ok {
		if role, err := lookup.RoleByKey(ctx, roleKey); err == nil {
			names = names[:0]
			seen := map[uuid.UUID]bool{}
			for _, as := range role.Assignments {
				if seen[as.AgentID] {
					continue
				}
				seen[as.AgentID] = true
				if holder, found := kit.agentByID(ctx, as.AgentID); found && !holder.Enabled {
					names = append(names, holder.Name)
				}
			}
			if len(names) == 0 {
				names = append(names, agent.Name)
			}
		}
	}
	return assigneeDisabledKey.Render(assigneeDisabledInput{Agents: names, Role: roleKey})
}
