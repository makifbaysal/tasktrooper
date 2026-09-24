package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const listRuntimeErrorsToolName = "list_runtime_errors"

const (
	defaultErrorsSince   = 24 * time.Hour
	errorSampleLineLimit = 10
)

type listRuntimeErrorsTool struct {
	kit *ToolKit
}

func (t *listRuntimeErrorsTool) Name() string { return listRuntimeErrorsToolName }

func (t *listRuntimeErrorsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listRuntimeErrorsToolName,
			Description: "List an environment's runtime errors, grouped and deduplicated (native grouping where the provider has " +
				"it, a message fingerprint otherwise) — a triage view, not a log dump. `new: true` means the group's first " +
				"occurrence falls inside this window, the \"started with this deploy\" signal.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"component":   componentSchema(),
					"environment": environmentSchema("\"production\""),
					"since": map[string]interface{}{
						"type":        "string",
						"description": "How far back to look, as a duration: \"30m\", \"2h\", \"1d\". Defaults to \"24h\".",
					},
					repositoryIDProperty: repositoryIDSchema(),
				},
			},
		},
	}
}

type listRuntimeErrorsArgs struct {
	Component    string `json:"component"`
	Environment  string `json:"environment"`
	Since        string `json:"since"`
	RepositoryID string `json:"repository_id"`
}

type errorGroupView struct {
	Message   string    `json:"message"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	New       bool      `json:"new"`
	Sample    string    `json:"sample,omitempty"`
	Truncated bool      `json:"sample_truncated,omitempty"`
}

func (t *listRuntimeErrorsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args listRuntimeErrorsArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}

	since := defaultErrorsSince
	if args.Since != "" {
		d, err := parseSinceDuration(args.Since)
		if err != nil {
			return toolError(name, err.Error())
		}
		since = d
	}

	repositoryID, err := resolveRepositoryID(ctx, args.RepositoryID, name)
	if err != nil {
		return toolError(name, err.Error())
	}
	env, comp, err := resolveBoundEnvironment(ctx, t.kit, repositoryID, args.Component, args.Environment)
	if err != nil {
		return toolError(name, err.Error())
	}

	groups, err := t.kit.Cloud.Errors(ctx, env.ID, time.Now().UTC().Add(-since))
	if err != nil {
		return toolError(name, err.Error())
	}

	views := make([]errorGroupView, 0, len(groups))
	for _, g := range groups {
		sample, truncated := firstLines(g.Sample, errorSampleLineLimit)
		views = append(views, errorGroupView{
			Message:   g.Message,
			Count:     g.Count,
			FirstSeen: g.FirstSeen,
			LastSeen:  g.LastSeen,
			New:       g.New,
			Sample:    sample,
			Truncated: truncated,
		})
	}

	return toolJSON(name, map[string]any{
		"component":   comp.Path,
		"environment": string(env.Environment),
		"errors":      views,
		"count":       len(views),
	})
}
