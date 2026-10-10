package board

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const listTeamToolName = "list_team"

type listTeamTool struct {
	kit *ToolKit
}

func newListTeamTool(kit *ToolKit) port.ToolExecutor {
	return &listTeamTool{kit: kit}
}

func (t *listTeamTool) Name() string {
	return listTeamToolName
}

func (t *listTeamTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listTeamToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties":           map[string]interface{}{},
			},
		},
	}
}

// agentRoleLister is the narrow extra surface application/workflow.Service
// offers beyond port.RoleResolver — every (role, areas) an agent holds, for
// the roles field below. Asserted against t.kit.Roles rather than added to
// port.RoleResolver, whose every other caller is the hot dispatch path.
type agentRoleLister interface {
	ListAssignmentsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.AgentRole, error)
}

func (t *listTeamTool) Execute(ctx context.Context, _ string) domain.ToolResult {
	agents, err := t.kit.Team.ListAgents(ctx)
	if err != nil {
		return toolError(listTeamToolName, err.Error())
	}
	roleLister, _ := t.kit.Roles.(agentRoleLister)
	team := make([]map[string]any, 0, len(agents))
	available := make([]map[string]any, 0)
	for _, a := range agents {
		if !a.Enabled {
			entry := map[string]any{
				"name":         a.Name,
				"catalog_slug": a.CatalogSlug,
				"role":         a.Description,
			}
			if roleLister != nil {
				if roles, rerr := roleLister.ListAssignmentsByAgent(ctx, a.ID); rerr == nil {
					entry["roles"] = roleSummaries(roles)
				}
			}
			available = append(available, entry)
			continue
		}
		entry := map[string]any{
			"name":          a.Name,
			"role":          a.Description,
			"subagent_type": a.SubagentType,
			"enabled":       a.Enabled,
		}
		if roleLister != nil {
			if roles, rerr := roleLister.ListAssignmentsByAgent(ctx, a.ID); rerr == nil {
				entry["roles"] = roleSummaries(roles)
			}
		}
		if t.kit.Subscriptions != nil {
			if cols, serr := t.kit.Subscriptions.ListAgentSubscriptions(ctx, a.ID); serr == nil {
				entry["subscribed_columns"] = cols
			}
		}
		team = append(team, entry)
	}
	return toolJSON(listTeamToolName, map[string]any{
		"count":            len(team),
		"team":             team,
		"available_to_add": available,
	})
}

func roleSummaries(roles []domain.AgentRole) []map[string]any {
	out := make([]map[string]any, 0, len(roles))
	for _, r := range roles {
		var areas []string
		if len(r.Assignments) > 0 {
			areas = r.Assignments[0].Areas
		}
		out = append(out, map[string]any{"key": r.Key, "name": r.Name, "areas": areas})
	}
	return out
}
