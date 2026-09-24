package projectmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const listChecksToolName = "list_component_checks"

type checksTool struct {
	kit *ToolKit
}

func (t *checksTool) Name() string { return listChecksToolName }

func (t *checksTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listChecksToolName,
			Description: "List the CI checks mapped onto each component: what job runs it, what it verifies, whether CI gates on it, " +
				"and the local command that reproduces it. Use this to find the exact command CI runs before you claim something builds or passes.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"component": map[string]interface{}{
						"type":        "string",
						"description": "A component's own path (e.g. \"apps/web\", or \".\" for a single-purpose repo) to list just that component's checks.",
					},
					repositoryIDProperty: repositoryIDSchema(),
				},
			},
		},
	}
}

type checksArgs struct {
	Component    string `json:"component"`
	RepositoryID string `json:"repository_id"`
}

type checkView struct {
	Workflow      string   `json:"workflow,omitempty"`
	Job           string   `json:"job"`
	Purpose       string   `json:"purpose,omitempty"`
	Gate          string   `json:"gate,omitempty"`
	LocalCommands []string `json:"local_commands,omitempty"`
	Required      bool     `json:"required"`
	Missing       bool     `json:"missing"`
}

type componentChecksView struct {
	Path   string      `json:"path"`
	Checks []checkView `json:"checks"`
}

func (t *checksTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args checksArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}

	repositoryID, err := resolveRepositoryID(ctx, args.RepositoryID, name)
	if err != nil {
		return toolError(name, err.Error())
	}

	var filter *uuid.UUID
	if component := strings.TrimSpace(args.Component); component != "" {
		componentID, err := resolveComponentID(ctx, t.kit.Model, repositoryID, component)
		if err != nil {
			return toolError(name, err.Error())
		}
		filter = &componentID
	}

	model, err := t.kit.Model.RepositoryModel(ctx, repositoryID)
	if err != nil {
		return toolError(name, err.Error())
	}

	checksByComponent := map[uuid.UUID][]domain.ComponentCheck{}
	for _, chk := range model.Checks {
		checksByComponent[chk.ComponentID] = append(checksByComponent[chk.ComponentID], chk)
	}

	out := make([]componentChecksView, 0, len(model.Components))
	for _, c := range model.Components {
		if c.Status != domain.ComponentStatusActive {
			continue
		}
		if filter != nil && c.ID != *filter {
			continue
		}
		out = append(out, componentChecksView{Path: c.Path, Checks: checkViews(checksByComponent[c.ID])})
	}

	return toolJSON(name, map[string]any{"components": out})
}

func checkViews(checks []domain.ComponentCheck) []checkView {
	views := make([]checkView, 0, len(checks))
	for _, chk := range checks {
		job := chk.JobName
		if job == "" {
			job = chk.JobKey
		}
		cmds := chk.LocalCommands.Get()
		display := make([]string, 0, len(cmds))
		for _, cmd := range cmds {
			display = append(display, cmd.Display())
		}
		views = append(views, checkView{
			Workflow:      chk.Workflow,
			Job:           job,
			Purpose:       string(chk.Purpose.Get()),
			Gate:          string(chk.Gate.Get()),
			LocalCommands: display,
			Required:      chk.Required(),
			Missing:       chk.Missing,
		})
	}
	return views
}
