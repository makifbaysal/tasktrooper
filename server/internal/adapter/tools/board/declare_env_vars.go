package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const declareEnvVarsToolName = "declare_env_vars"

// EnvDeclarer is application/envreq.Service.Declare. Built after this
// registration, so it is read at call time like Releases.
type EnvDeclarer interface {
	Declare(ctx context.Context, repositoryID uuid.UUID, componentID *uuid.UUID, taskID uuid.UUID, reqs []domain.EnvRequirement) ([]domain.EnvRequirement, error)
}

type declareEnvVarsTool struct{ kit *ToolKit }

func newDeclareEnvVarsTool(kit *ToolKit) port.ToolExecutor { return &declareEnvVarsTool{kit: kit} }

func (t *declareEnvVarsTool) Name() string { return declareEnvVarsToolName }

func (t *declareEnvVarsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: declareEnvVarsToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"vars"},
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"vars": map[string]interface{}{
						"type":     "array",
						"minItems": 1,
						"items": map[string]interface{}{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"name", "kind"},
							"properties": map[string]interface{}{
								"name": map[string]interface{}{"type": "string"},
								"kind": map[string]interface{}{
									"type": "string",
									"enum": []string{
										string(domain.EnvKindValue), string(domain.EnvKindGenerated),
										string(domain.EnvKindHumanSecret), string(domain.EnvKindHumanBcrypt),
										string(domain.EnvKindOptional),
									},
								},
								"value":       map[string]interface{}{"type": "string"},
								"description": map[string]interface{}{"type": "string"},
							},
						},
					},
				},
			},
		},
	}
}

type declaredEnvVar struct {
	Name        string            `json:"name"`
	Kind        domain.EnvVarKind `json:"kind"`
	Value       string            `json:"value,omitempty"`
	Description string            `json:"description,omitempty"`
}

type declareEnvVarsResult struct {
	Declared []declaredEnvVar `json:"declared"`
	// KeptHumanDecision are names a human already classified differently;
	// their recorded kind stands.
	KeptHumanDecision []string `json:"kept_human_decision,omitempty"`
}

func (t *declareEnvVarsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string           `json:"task_id"`
		Vars   []declaredEnvVar `json:"vars"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(declareEnvVarsToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if len(args.Vars) == 0 {
		return toolError(declareEnvVarsToolName, "vars is empty: declare at least one variable")
	}
	if t.kit.EnvRequirements == nil {
		return toolError(declareEnvVarsToolName, "environment variable declarations are not configured on this deployment")
	}
	taskID, repositoryID, res := t.kit.resolveDeployTask(ctx, declareEnvVarsToolName, args.TaskID)
	if res != nil {
		return *res
	}
	task, err := t.kit.Tasks.GetTask(ctx, repositoryID, taskID)
	if err != nil {
		return toolError(declareEnvVarsToolName, err.Error())
	}
	reqs := make([]domain.EnvRequirement, 0, len(args.Vars))
	for _, v := range args.Vars {
		reqs = append(reqs, domain.EnvRequirement{Name: v.Name, Kind: v.Kind, Value: v.Value, Description: v.Description})
	}
	stored, err := t.kit.EnvRequirements.Declare(ctx, repositoryID, task.ComponentID, taskID, reqs)
	if err != nil {
		return toolError(declareEnvVarsToolName, err.Error())
	}
	out := declareEnvVarsResult{Declared: make([]declaredEnvVar, 0, len(stored))}
	for _, r := range stored {
		out.Declared = append(out.Declared, declaredEnvVar{Name: r.Name, Kind: r.Kind, Value: r.Value, Description: r.Description})
		if r.Source == domain.EnvSourceHuman {
			out.KeptHumanDecision = append(out.KeptHumanDecision, r.Name)
		}
	}
	return toolJSON(declareEnvVarsToolName, out)
}
