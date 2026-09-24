package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const queryRuntimeLogsToolName = "query_runtime_logs"

const (
	defaultLogSince = time.Hour
	defaultLogLimit = 100
	maxLogLimit     = 500
)

type queryRuntimeLogsTool struct {
	kit *ToolKit
}

func (t *queryRuntimeLogsTool) Name() string { return queryRuntimeLogsToolName }

func (t *queryRuntimeLogsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: queryRuntimeLogsToolName,
			Description: "Read an environment's live application logs — what the running product itself printed, not a CI job's " +
				"output (that is get_deploy_logs). Use this to see what a request actually did in production or on stage " +
				"instead of guessing from the code.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"component":   componentSchema(),
					"environment": environmentSchema("\"production\""),
					"since": map[string]interface{}{
						"type":        "string",
						"description": "How far back to read, as a duration: \"30m\", \"2h\", \"1d\". Defaults to \"1h\".",
					},
					"min_severity": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"debug", "info", "warning", "error", "critical"},
						"description": "Only entries at or above this severity.",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Free-text filter over the log message.",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum entries to return. Defaults to 100, capped at 500.",
					},
					repositoryIDProperty: repositoryIDSchema(),
				},
			},
		},
	}
}

type queryRuntimeLogsArgs struct {
	Component    string `json:"component"`
	Environment  string `json:"environment"`
	Since        string `json:"since"`
	MinSeverity  string `json:"min_severity"`
	Text         string `json:"text"`
	Limit        int    `json:"limit"`
	RepositoryID string `json:"repository_id"`
}

func validLogSeverity(s string) bool {
	switch domain.LogSeverity(s) {
	case domain.LogDebug, domain.LogInfo, domain.LogWarning, domain.LogError, domain.LogCritical:
		return true
	}
	return false
}

type logEntryView struct {
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Path      string `json:"path,omitempty"`
	Status    int    `json:"status_code,omitempty"`
}

func (t *queryRuntimeLogsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args queryRuntimeLogsArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}
	if args.MinSeverity != "" && !validLogSeverity(args.MinSeverity) {
		return toolError(name, fmt.Sprintf("invalid min_severity %q", args.MinSeverity))
	}

	since := defaultLogSince
	if args.Since != "" {
		d, err := parseSinceDuration(args.Since)
		if err != nil {
			return toolError(name, err.Error())
		}
		since = d
	}

	limit := args.Limit
	if limit <= 0 {
		limit = defaultLogLimit
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	repositoryID, err := resolveRepositoryID(ctx, args.RepositoryID, name)
	if err != nil {
		return toolError(name, err.Error())
	}
	env, comp, err := resolveBoundEnvironment(ctx, t.kit, repositoryID, args.Component, args.Environment)
	if err != nil {
		return toolError(name, err.Error())
	}

	now := time.Now().UTC()
	page, err := t.kit.Cloud.Logs(ctx, env.ID, domain.RuntimeLogQuery{
		Since:       now.Add(-since),
		Until:       now,
		MinSeverity: domain.LogSeverity(args.MinSeverity),
		Text:        args.Text,
		Limit:       limit,
	})
	if err != nil {
		return toolError(name, err.Error())
	}

	entries := make([]logEntryView, 0, len(page.Entries))
	for _, e := range page.Entries {
		entries = append(entries, logEntryView{
			Timestamp: e.Timestamp.Format(time.RFC3339),
			Severity:  string(e.Severity),
			Message:   e.Message,
			Path:      e.Path,
			Status:    e.StatusCode,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp > entries[j].Timestamp })

	out := map[string]any{
		"component":   comp.Path,
		"environment": string(env.Environment),
		"entries":     entries,
		"count":       len(entries),
	}
	if page.Truncated {
		out["note"] = "the provider capped this window; older entries in range were not returned"
	}
	return toolJSON(name, out)
}
