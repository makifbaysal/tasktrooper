package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ---- JSON shapes ----

type behaviourRefJSON struct {
	Key    string            `json:"key"`
	Params map[string]string `json:"params,omitempty"`
}

func toBehaviourRefsJSON(in []domain.BehaviourRef) []behaviourRefJSON {
	out := make([]behaviourRefJSON, 0, len(in))
	for _, b := range in {
		out = append(out, behaviourRefJSON{Key: string(b.Key), Params: b.Params})
	}
	return out
}

func fromBehaviourRefsJSON(in []behaviourRefJSON) []domain.BehaviourRef {
	out := make([]domain.BehaviourRef, 0, len(in))
	for _, b := range in {
		out = append(out, domain.BehaviourRef{Key: domain.BehaviourKey(b.Key), Params: b.Params})
	}
	return out
}

type taskTypeJSON struct {
	Key            string             `json:"key"`
	Label          string             `json:"label"`
	KeyPrefix      string             `json:"key_prefix"`
	Position       int                `json:"position"`
	IsDefault      bool               `json:"is_default"`
	IsDefect       bool               `json:"is_defect"`
	AssigneeRoleID *uuid.UUID         `json:"assignee_role_id"`
	AssigneeMode   string             `json:"assignee_mode"`
	Behaviours     []behaviourRefJSON `json:"behaviours"`
	BuiltIn        bool               `json:"built_in"`
	TaskCount      int                `json:"task_count"`
}

func toTaskTypeJSON(t domain.TaskTypeDef) taskTypeJSON {
	return taskTypeJSON{
		Key: string(t.Key), Label: t.Label, KeyPrefix: t.KeyPrefix, Position: t.Position,
		IsDefault: t.IsDefault, IsDefect: t.IsDefect, AssigneeRoleID: t.AssigneeRoleID,
		AssigneeMode: string(t.AssigneeMode), Behaviours: toBehaviourRefsJSON(t.Behaviours),
		BuiltIn: t.BuiltIn, TaskCount: t.TaskCount,
	}
}

type stageParticipantJSON struct {
	RoleID       uuid.UUID `json:"role_id"`
	Mode         string    `json:"mode"`
	Instructions string    `json:"instructions,omitempty"`
	Position     int       `json:"position"`
}

type stageJSON struct {
	ID           uuid.UUID              `json:"id,omitempty"`
	ColumnSlug   string                 `json:"column_slug"`
	Position     int                    `json:"position"`
	OnPath       bool                   `json:"on_path"`
	Kind         string                 `json:"kind"`
	Behaviours   []behaviourRefJSON     `json:"behaviours"`
	Instructions string                 `json:"instructions,omitempty"`
	Participants []stageParticipantJSON `json:"participants"`
}

func toStageJSON(s domain.WorkflowStage) stageJSON {
	out := stageJSON{
		ID: s.ID, ColumnSlug: string(s.Column), Position: s.Position, OnPath: s.OnPath, Kind: string(s.Kind),
		Behaviours: toBehaviourRefsJSON(s.Behaviours), Instructions: s.Instructions,
		Participants: make([]stageParticipantJSON, 0, len(s.Participants)),
	}
	for _, p := range s.Participants {
		out.Participants = append(out.Participants, stageParticipantJSON{
			RoleID: p.RoleID, Mode: string(p.Mode), Instructions: p.Instructions, Position: p.Position,
		})
	}
	return out
}

func fromStageJSON(taskType domain.TaskType, in stageJSON) domain.WorkflowStage {
	st := domain.WorkflowStage{
		TaskType: taskType, Column: domain.TaskColumn(in.ColumnSlug), Position: in.Position, OnPath: in.OnPath,
		Kind: domain.StageKind(in.Kind), Behaviours: fromBehaviourRefsJSON(in.Behaviours), Instructions: in.Instructions,
	}
	for _, p := range in.Participants {
		st.Participants = append(st.Participants, domain.StageParticipant{
			RoleID: p.RoleID, Mode: domain.ParticipantMode(p.Mode), Instructions: p.Instructions, Position: p.Position,
		})
	}
	return st
}

func toWorkflowJSON(wf domain.Workflow) fiber.Map {
	stages := make([]stageJSON, 0, len(wf.Stages))
	for _, s := range wf.Stages {
		stages = append(stages, toStageJSON(s))
	}
	return fiber.Map{"task_type": string(wf.Type.Key), "stages": stages}
}

// ---- Task types ----

func (h *Handler) ListTaskTypes(c *fiber.Ctx) error {
	types, err := h.workflowSvc.ListTaskTypes(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	out := make([]taskTypeJSON, 0, len(types))
	for _, t := range types {
		out = append(out, toTaskTypeJSON(t))
	}
	return c.JSON(fiber.Map{"task_types": out})
}

func (h *Handler) CreateTaskType(c *fiber.Ctx) error {
	var req struct {
		Key       string `json:"key"`
		Label     string `json:"label"`
		KeyPrefix string `json:"key_prefix"`
		CloneFrom string `json:"clone_from"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	created, err := h.workflowSvc.CreateTaskType(h.enrichContext(c), domain.TaskTypeDef{
		Key: domain.TaskType(req.Key), Label: req.Label, KeyPrefix: req.KeyPrefix,
	}, domain.TaskType(req.CloneFrom))
	if err != nil {
		return workflowValidationError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toTaskTypeJSON(created))
}

func (h *Handler) UpdateTaskType(c *fiber.Ctx) error {
	key := domain.TaskType(c.Params("key"))
	var req struct {
		Label          string             `json:"label"`
		KeyPrefix      string             `json:"key_prefix"`
		Position       int                `json:"position"`
		IsDefault      bool               `json:"is_default"`
		IsDefect       bool               `json:"is_defect"`
		AssigneeRoleID *uuid.UUID         `json:"assignee_role_id"`
		AssigneeMode   string             `json:"assignee_mode"`
		Behaviours     []behaviourRefJSON `json:"behaviours"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	updated, err := h.workflowSvc.UpdateTaskType(h.enrichContext(c), domain.TaskTypeDef{
		Key: key, Label: req.Label, KeyPrefix: req.KeyPrefix, Position: req.Position,
		IsDefault: req.IsDefault, IsDefect: req.IsDefect, AssigneeRoleID: req.AssigneeRoleID,
		AssigneeMode: domain.AssigneeMode(req.AssigneeMode), Behaviours: fromBehaviourRefsJSON(req.Behaviours),
	})
	if err != nil {
		if errors.Is(err, workflow.ErrPrefixChangeWithTasks) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
		}
		return workflowValidationError(c, err)
	}
	return c.JSON(toTaskTypeJSON(updated))
}

func (h *Handler) DeleteTaskType(c *fiber.Ctx) error {
	key := domain.TaskType(c.Params("key"))
	if err := h.workflowSvc.DeleteTaskType(h.enrichContext(c), key); err != nil {
		if errors.Is(err, workflow.ErrTaskTypeInUse) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
		}
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetTaskTypeWorkflow(c *fiber.Ctx) error {
	key := domain.TaskType(c.Params("key"))
	stages, err := h.workflowSvc.ListStages(h.enrichContext(c), key)
	if err != nil {
		return internalError(c, err)
	}
	out := make([]stageJSON, 0, len(stages))
	for _, s := range stages {
		out = append(out, toStageJSON(s))
	}
	return c.JSON(fiber.Map{"task_type": string(key), "stages": out})
}

func (h *Handler) PutTaskTypeWorkflow(c *fiber.Ctx) error {
	key := domain.TaskType(c.Params("key"))
	var req struct {
		Stages []stageJSON `json:"stages"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	stages := make([]domain.WorkflowStage, 0, len(req.Stages))
	for _, s := range req.Stages {
		stages = append(stages, fromStageJSON(key, s))
	}
	if err := h.workflowSvc.ReplaceStages(h.enrichContext(c), key, stages); err != nil {
		return workflowValidationError(c, err)
	}
	saved, err := h.workflowSvc.ListStages(h.enrichContext(c), key)
	if err != nil {
		return internalError(c, err)
	}
	out := make([]stageJSON, 0, len(saved))
	for _, s := range saved {
		out = append(out, toStageJSON(s))
	}
	return c.JSON(fiber.Map{"task_type": string(key), "stages": out})
}

func (h *Handler) ListWorkflows(c *fiber.Ctx) error {
	workflows, err := h.workflowSvc.ListWorkflows(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	out := make([]fiber.Map, 0, len(workflows))
	for _, wf := range workflows {
		out = append(out, toWorkflowJSON(wf))
	}
	return c.JSON(fiber.Map{"workflows": out})
}

// ---- Behaviour registry ----

type paramSpecJSON struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Options  []string `json:"options,omitempty"`
	Required bool     `json:"required"`
}

type behaviourSpecJSON struct {
	Key   string `json:"key"`
	Scope string `json:"scope"`
	Group string `json:"group"`
	// Kinds is empty for a behaviour that applies to every stage kind; the
	// picker offers the rest only on the kinds they can actually fire on.
	Kinds       []string        `json:"kinds"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Params      []paramSpecJSON `json:"params"`
}

func (h *Handler) ListWorkflowBehaviours(c *fiber.Ctx) error {
	out := make([]behaviourSpecJSON, 0, len(domain.BehaviourRegistry))
	for key, spec := range domain.BehaviourRegistry {
		params := make([]paramSpecJSON, 0, len(spec.Params))
		for _, p := range spec.Params {
			params = append(params, paramSpecJSON{Name: p.Name, Type: string(p.Type), Options: p.Options, Required: p.Required})
		}
		kinds := make([]string, 0, len(spec.Kinds))
		for _, k := range spec.Kinds {
			kinds = append(kinds, string(k))
		}
		out = append(out, behaviourSpecJSON{
			Key: string(key), Scope: string(spec.Scope), Group: string(spec.Group), Kinds: kinds,
			Label: spec.Label, Description: spec.Description, Params: params,
		})
	}
	kinds := []string{"intake", "queue", "work", "review", "approval", "rework", "parked", "terminal"}
	return c.JSON(fiber.Map{"behaviours": out, "kinds": kinds, "areas": domain.RoleAreas()})
}

// workflowValidationError renders a *workflow.ValidationError as 422 with a
// problems array, and anything else as 400.
func workflowValidationError(c *fiber.Ctx, err error) error {
	var valErr *workflow.ValidationError
	if errors.As(err, &valErr) {
		problems := make([]fiber.Map, 0, len(valErr.Problems))
		for _, p := range valErr.Problems {
			problems = append(problems, fiber.Map{"column_slug": p.ColumnSlug, "field": p.Field, "message": p.Message})
		}
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": err.Error(), "problems": problems})
	}
	return badRequest(c, err.Error())
}
