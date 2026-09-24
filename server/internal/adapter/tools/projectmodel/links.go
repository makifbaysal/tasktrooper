package projectmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const listLinksToolName = "list_links"

type linksTool struct {
	kit *ToolKit
}

func (t *linksTool) Name() string { return listLinksToolName }

func (t *linksTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listLinksToolName,
			Description: "List what a component talks to and what talks to it: other components (in this repository or another), " +
				"and system resources (databases, queues, third-party APIs). Use this before touching an integration point to see who else depends on it.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"component": map[string]interface{}{
						"type":        "string",
						"description": "A component's own path (e.g. \"apps/web\", or \".\" for a single-purpose repo) to list just that component's links.",
					},
					"direction": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"out", "in", "both"},
						"description": "\"out\" for what the component calls, \"in\" for what calls it, \"both\" (default) for everything.",
					},
					repositoryIDProperty: repositoryIDSchema(),
				},
			},
		},
	}
}

type linksArgs struct {
	Component    string `json:"component"`
	Direction    string `json:"direction"`
	RepositoryID string `json:"repository_id"`
}

type linkView struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Protocol   string   `json:"protocol"`
	EnvVars    []string `json:"env_vars,omitempty"`
	Status     string   `json:"status"`
	Confidence string   `json:"confidence,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

func (t *linksTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args linksArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}

	direction := strings.TrimSpace(args.Direction)
	if direction == "" {
		direction = "both"
	}
	if direction != "out" && direction != "in" && direction != "both" {
		return toolError(name, fmt.Sprintf("invalid direction %q: must be \"out\", \"in\" or \"both\"", direction))
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

	componentLabels := map[uuid.UUID]string{}
	for _, c := range model.Components {
		componentLabels[c.ID] = model.Repository.Name + "/" + c.Path
	}
	for _, lc := range model.LinkedComponents {
		componentLabels[lc.ID] = lc.RepositoryName + "/" + lc.Path
	}
	resourceLabels := map[uuid.UUID]string{}
	for _, r := range model.Resources {
		label := r.Name
		if label == "" {
			label = r.Vendor
		}
		resourceLabels[r.ID] = fmt.Sprintf("%s (%s)", label, r.Kind)
	}

	toLabel := func(l domain.ComponentLink) string {
		switch {
		case l.ToComponentID != nil:
			if label, ok := componentLabels[*l.ToComponentID]; ok {
				return label
			}
			return l.Hint
		case l.ToResourceID != nil:
			if label, ok := resourceLabels[*l.ToResourceID]; ok {
				return label
			}
			return l.Hint
		default:
			return l.Hint
		}
	}

	var views []linkView
	if direction == "out" || direction == "both" {
		for _, l := range model.Links {
			if filter != nil && l.FromComponentID != *filter {
				continue
			}
			views = append(views, linkView{
				From:       componentLabels[l.FromComponentID],
				To:         toLabel(l),
				Protocol:   string(l.Protocol),
				EnvVars:    l.EnvVars,
				Status:     string(l.Status),
				Confidence: string(l.Confidence),
				Reason:     l.Reason,
			})
		}
	}
	if direction == "in" || direction == "both" {
		for _, l := range model.IncomingLinks {
			if filter != nil && (l.ToComponentID == nil || *l.ToComponentID != *filter) {
				continue
			}
			views = append(views, linkView{
				From:       componentLabels[l.FromComponentID],
				To:         toLabel(l),
				Protocol:   string(l.Protocol),
				EnvVars:    l.EnvVars,
				Status:     string(l.Status),
				Confidence: string(l.Confidence),
				Reason:     l.Reason,
			})
		}
	}

	return toolJSON(name, map[string]any{"links": views})
}
