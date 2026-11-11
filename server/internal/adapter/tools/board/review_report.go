package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const recordReviewFindingsToolName = "record_review_findings"

// ReviewReporter is the reviewer's side of the review history: the findings
// it raised, the places it wants a person to look and what it could not
// verify, kept optional for the reason AnnotationManager is.
type ReviewReporter interface {
	RecordReviewReport(ctx context.Context, repositoryID, taskID, agentID uuid.UUID, report domain.ReviewReportInput) (int, error)
}

type recordReviewFindingsTool struct{ kit *ToolKit }

func newRecordReviewFindingsTool(kit *ToolKit) port.ToolExecutor {
	return &recordReviewFindingsTool{kit: kit}
}

func (t *recordReviewFindingsTool) Name() string { return recordReviewFindingsToolName }

func (t *recordReviewFindingsTool) Definition() domain.ToolDefinition {
	str := map[string]interface{}{"type": "string"}
	enum := func(values []string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "enum": values}
	}
	finding := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"key":                 str,
			"file":                str,
			"line":                map[string]interface{}{"type": "integer", "minimum": 0},
			"severity":            enum(domain.ReviewSeverities()),
			"category":            str,
			"message":             str,
			"confidence":          enum(domain.ReviewConfidences()),
			"human_review_reason": str,
			"question":            str,
			"status":              enum([]string{domain.ReviewFindingOpen, domain.ReviewFindingWithdrawn}),
		},
		"required": []string{"message"},
	}
	human := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"file":     str,
			"line":     map[string]interface{}{"type": "integer", "minimum": 0},
			"reason":   str,
			"question": str,
		},
		"required": []string{"reason"},
	}
	notReviewed := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]interface{}{"scope": str, "reason": str},
		"required":             []string{"scope", "reason"},
	}
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: recordReviewFindingsToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"findings": map[string]interface{}{
						"type": "array", "maxItems": domain.MaxReviewFindingsPerReport, "items": finding,
					},
					"needs_human_review": map[string]interface{}{
						"type": "array", "maxItems": domain.MaxReviewFlagsPerReport, "items": human,
					},
					"not_reviewed": map[string]interface{}{
						"type": "array", "maxItems": domain.MaxReviewNotReviewed, "items": notReviewed,
					},
					"reviewed_aspects": map[string]interface{}{
						"type": "array", "maxItems": domain.MaxReviewAspects, "items": str,
					},
				},
			},
		},
	}
}

func (t *recordReviewFindingsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
		domain.ReviewReportInput
	}
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(recordReviewFindingsToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(recordReviewFindingsToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(recordReviewFindingsToolName, err.Error())
	}
	agentID, err := resolveAgentID(ctx)
	if err != nil {
		return toolError(recordReviewFindingsToolName, err.Error())
	}
	reporter, ok := t.kit.Tasks.(ReviewReporter)
	if !ok {
		return toolError(recordReviewFindingsToolName, "this build cannot record review findings")
	}
	n, err := reporter.RecordReviewReport(ctx, repositoryID, taskID, agentID, args.ReviewReportInput)
	if err != nil {
		return toolError(recordReviewFindingsToolName, err.Error())
	}
	return toolJSON(recordReviewFindingsToolName, map[string]interface{}{
		"task_id":  taskID.String(),
		"recorded": n,
	})
}
