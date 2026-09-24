package projectmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"

	appprojectmodel "github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
)

type recordNoteTool struct {
	kit *ToolKit
}

func (t *recordNoteTool) Name() string { return appprojectmodel.RecordNoteToolName }

func (t *recordNoteTool) Definition() domain.ToolDefinition {
	topics := make([]string, 0, len(domain.AllNoteTopics()))
	for _, topic := range domain.AllNoteTopics() {
		topics = append(topics, string(topic))
	}
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: appprojectmodel.RecordNoteToolName,
			Description: "Record judgment about this repository or one of its components that the code alone does not make obvious: purpose, entrypoints, " +
				"conventions, invariants, danger zones, change recipes, gotchas. " +
				"Never restate what is already derived by the platform — stack, commands, CI checks and links are scanned from the tree and shown by " +
				"get_project_brief, list_component_checks and list_links; repeating them here is rejected as noise, not judgment. " +
				"Every note needs at least one evidence path that exists in the working copy, or it is rejected. " +
				"A note the human already wrote or locked is refused — ask them to edit or unlock it instead.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"notes"},
				"properties": map[string]interface{}{
					"notes": map[string]interface{}{
						"type":        "array",
						"description": "The notes to record. Each REPLACES the prior agent-written note of the same topic and scope.",
						"items": map[string]interface{}{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"topic", "body_md", "evidence"},
							"properties": map[string]interface{}{
								"topic": map[string]interface{}{
									"type":        "string",
									"enum":        topics,
									"description": "What kind of judgment this is.",
								},
								"body_md": map[string]interface{}{
									"type":        "string",
									"description": "The note body in markdown. Specific to this codebase — anything equally true of another repo with the same stack does not belong here.",
								},
								"evidence": map[string]interface{}{
									"type":        "array",
									"description": "Files backing this note. At least one must exist in the repository or the note is rejected.",
									"items": map[string]interface{}{
										"type":                 "object",
										"additionalProperties": false,
										"required":             []string{"path"},
										"properties": map[string]interface{}{
											"path": map[string]interface{}{"type": "string", "description": "Repo-relative file path"},
											"line": map[string]interface{}{"type": "integer", "description": "Line number, when it sharpens the claim"},
											"note": map[string]interface{}{"type": "string", "description": "What this file shows"},
										},
									},
								},
								"component_path": map[string]interface{}{
									"type":        "string",
									"description": "A component's own path to scope this note to just that component. Omit for a repository-wide note.",
								},
							},
						},
					},
					repositoryIDProperty: repositoryIDSchema(),
				},
			},
		},
	}
}

type noteEvidenceArg struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Note string `json:"note,omitempty"`
}

type noteArg struct {
	Topic         string            `json:"topic"`
	BodyMD        string            `json:"body_md"`
	Evidence      []noteEvidenceArg `json:"evidence"`
	ComponentPath string            `json:"component_path,omitempty"`
}

func (t *recordNoteTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args struct {
		Notes        []noteArg `json:"notes"`
		RepositoryID string    `json:"repository_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}
	if len(args.Notes) == 0 {
		return toolError(name, "notes is required: send at least one {topic, body_md, evidence} entry")
	}

	repositoryID, err := resolveRepositoryID(ctx, args.RepositoryID, name)
	if err != nil {
		return toolError(name, err.Error())
	}

	writes := make([]appprojectmodel.NoteWrite, 0, len(args.Notes))
	for _, n := range args.Notes {
		evidence := make([]domain.SourceEvidence, 0, len(n.Evidence))
		for _, e := range n.Evidence {
			evidence = append(evidence, domain.SourceEvidence{Path: e.Path, Line: e.Line, Note: e.Note})
		}
		writes = append(writes, appprojectmodel.NoteWrite{
			Topic:         domain.NoteTopic(strings.TrimSpace(n.Topic)),
			BodyMD:        n.BodyMD,
			Evidence:      evidence,
			ComponentPath: strings.TrimSpace(n.ComponentPath),
		})
	}

	results, err := t.kit.Model.RecordAgentNotes(ctx, repositoryID, writes)
	if err != nil {
		return toolError(name, err.Error())
	}

	accepted := 0
	rejected := make([]string, 0, len(results))
	for _, r := range results {
		if r.Accepted {
			accepted++
			continue
		}
		rejected = append(rejected, r.Topic+": "+r.Reason)
	}
	if accepted == 0 {
		return toolError(name, "no note was recorded — "+strings.Join(rejected, "; "))
	}

	payload := map[string]any{"results": results}
	if len(rejected) > 0 {
		payload["next_step"] = "Fix the rejected notes and call the tool again with just those."
	}
	return toolJSON(name, payload)
}
