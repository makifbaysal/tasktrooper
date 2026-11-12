// Package audience exposes audience research to the Product Manager as
// ordinary tools: write an audience of synthetic people from a project's
// brief, run a study on it (would they pay, at what price, what is missing),
// read the report and talk to the people. They go through the same service the
// Audience screen does. Anything that spends model calls — building an
// audience, starting a study, asking everyone — needs `user_approved: true`,
// which the agent may set only after the user said yes, and its refusal tells
// the agent how many calls it would spend.
package audience

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Service is application/audience.Service.
type Service interface {
	ListAudiences(ctx context.Context) ([]domain.Audience, error)
	Audience(ctx context.Context, id uuid.UUID) (audience.AudienceDetail, error)
	CreateAudience(ctx context.Context, in audience.CreateAudienceInput) (domain.Audience, error)
	UpdateAudience(ctx context.Context, id uuid.UUID, in audience.UpdateAudienceInput) (domain.Audience, error)
	Build(ctx context.Context, id uuid.UUID) (domain.Audience, error)
	Chat(ctx context.Context, personaID uuid.UUID, message string) (domain.ChatMessage, error)
	ListStudies(ctx context.Context, audienceID *uuid.UUID) ([]domain.Study, error)
	Study(ctx context.Context, id uuid.UUID) (audience.StudyDetail, error)
	PlanStudy(ctx context.Context, audienceID uuid.UUID, in audience.PlanStudyInput) (domain.Study, error)
	UpdateStudy(ctx context.Context, id uuid.UUID, in audience.PlanStudyInput) (domain.Study, error)
	GenerateScript(ctx context.Context, id uuid.UUID) (domain.Study, error)
	ApproveScript(ctx context.Context, id uuid.UUID, version int, by string) (domain.Study, error)
	GenerateReport(ctx context.Context, id uuid.UUID) (domain.Study, error)
	StartStudy(ctx context.Context, id uuid.UUID) (domain.Study, error)
	CancelStudy(ctx context.Context, id uuid.UUID) error
	FollowUp(ctx context.Context, studyID, personaID uuid.UUID, question string) (domain.ChatMessage, error)
	FollowUpCalls(ctx context.Context, studyID uuid.UUID) (int, error)
	AskEveryone(ctx context.Context, studyID uuid.UUID, question string) (uuid.UUID, int, error)
	AskResearch(ctx context.Context, studyID uuid.UUID, question string) (string, error)
	Rerun(ctx context.Context, studyID uuid.UUID, changes string) (domain.Study, error)
	CompareStudies(ctx context.Context, beforeID, afterID uuid.UUID) (audience.StudyComparison, error)
}

// Waker brings the calling chat back once background work ended.
type Waker interface {
	WakeSessionOn(ctx context.Context, kind string, refID uuid.UUID, label string)
}

type ToolKit struct {
	Waker   Waker
	Service Service
}

// maxResultBytes keeps one answer inside a context budget.
const maxResultBytes = 48 << 10

type approvalInput struct {
	Action string
	Calls  int
}

type detailInput struct{ Detail string }

var (
	scriptNotApprovedKey = prompt.Define[struct{}]("guard.audience_script_not_approved", struct{}{})
	scriptChangedKey     = prompt.Define[struct{}]("guard.audience_script_changed", struct{}{})
	scriptNeedsYesKey    = prompt.Define[struct{}]("guard.audience_script_approval_needs_yes", struct{}{})
	noRuntimeKey         = prompt.Define[struct{}]("guard.audience_no_runtime", struct{}{})
	needsApprovalKey     = prompt.Define("guard.audience_needs_approval", approvalInput{Action: "study_start", Calls: 13})
	callFailedKey        = prompt.Define("guard.audience_call_failed", detailInput{Detail: "the model did not answer in time"})
)

// tool is one entry of the table below. approvalCalls, when set, answers how
// many model calls the tool would spend (reading whatever it needs); the tool
// then refuses unless the call carries `user_approved: true`.
type tool struct {
	name          string
	params        map[string]interface{}
	run           func(ctx context.Context, k *ToolKit, args map[string]any) (any, error)
	approvalCalls func(ctx context.Context, k *ToolKit, args map[string]any) (int, error)
}

func (t *tool) Name() string { return t.name }

func (t *tool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{Name: t.name, Parameters: t.params}}
}

type boundTool struct {
	*tool
	kit *ToolKit
}

func (t boundTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	args := map[string]any{}
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(t.name, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	if t.approvalCalls != nil {
		calls, err := t.approvalCalls(ctx, t.kit, args)
		if err != nil {
			return toolError(t.name, failure(err))
		}
		if approved, _ := args["user_approved"].(bool); !approved {
			return toolError(t.name, needsApprovalKey.Render(approvalInput{Action: t.name, Calls: calls}))
		}
	}
	out, err := t.run(ctx, t.kit, args)
	if err != nil {
		return toolError(t.name, failure(err))
	}
	return toolJSON(t.name, out)
}

func NewExecutors(k *ToolKit) []port.ToolExecutor {
	if k == nil || k.Service == nil {
		return nil
	}
	defs := tools()
	out := make([]port.ToolExecutor, 0, len(defs))
	for _, d := range defs {
		out = append(out, boundTool{tool: d, kit: k})
	}
	return out
}

// ToolNames are the names NewExecutors registers.
func ToolNames() []string {
	defs := tools()
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.name)
	}
	return names
}

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

func toolJSON(name string, payload any) domain.ToolResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolError(name, fmt.Sprintf("marshal response: %v", err))
	}
	if len(raw) > maxResultBytes {
		cut, _ := json.Marshal(map[string]any{"truncated": true, "data": string(raw[:maxResultBytes])})
		raw = cut
	}
	return domain.ToolResult{Name: name, Content: string(raw)}
}

func failure(err error) string {
	switch {
	case errors.Is(err, audience.ErrNoRuntime):
		return prompt.Text(noRuntimeKey)
	case errors.Is(err, audience.ErrScriptNotApproved):
		return prompt.Text(scriptNotApprovedKey)
	case errors.Is(err, audience.ErrScriptChanged):
		return prompt.Text(scriptChangedKey)
	case errors.Is(err, audience.ErrCallFailed):
		return callFailedKey.Render(detailInput{Detail: strings.TrimPrefix(err.Error(), audience.ErrCallFailed.Error()+": ")})
	case errors.Is(err, domain.ErrAudienceInvalid):
		return strings.TrimPrefix(err.Error(), domain.ErrAudienceInvalid.Error()+": ")
	default:
		return err.Error()
	}
}

// ---- argument helpers

func argString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

func argInt(args map[string]any, key string) (int, bool) {
	f, ok := args[key].(float64)
	return int(f), ok
}

func argID(args map[string]any, key string) (uuid.UUID, error) {
	raw := argString(args, key)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be the id from the list or the earlier result, not %q", key, raw)
	}
	return id, nil
}

func argStrings(args map[string]any, key string) []string {
	var out []string
	if list, ok := args[key].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func argNumbers(args map[string]any, key string) []float64 {
	var out []float64
	if list, ok := args[key].([]any); ok {
		for _, v := range list {
			if f, ok := v.(float64); ok {
				out = append(out, f)
			}
		}
	}
	return out
}

func argPages(args map[string]any, key string) []domain.PageInput {
	list, _ := args[key].([]any)
	var out []domain.PageInput
	for _, v := range list {
		if m, ok := v.(map[string]any); ok {
			p := domain.PageInput{}
			p.URL, _ = m["url"].(string)
			p.Copy, _ = m["copy"].(string)
			out = append(out, p)
		}
	}
	return out
}

func argScript(args map[string]any, key string) (*domain.StudyScript, bool) {
	list, ok := args[key].([]any)
	if !ok {
		return nil, false
	}
	sc := &domain.StudyScript{Sections: []domain.ScriptSection{}}
	for _, v := range list {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		sec := domain.ScriptSection{}
		sec.Title, _ = m["title"].(string)
		sec.Questions = argStrings(m, "questions")
		sc.Sections = append(sc.Sections, sec)
	}
	return sc, true
}

func argSegments(args map[string]any, key string) ([]domain.AudienceSegment, bool) {
	list, ok := args[key].([]any)
	if !ok {
		return nil, false
	}
	out := []domain.AudienceSegment{}
	for _, v := range list {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		seg := domain.AudienceSegment{}
		seg.Name, _ = m["name"].(string)
		seg.Description, _ = m["description"].(string)
		if c, ok := m["count"].(float64); ok {
			seg.Count = int(c)
		}
		out = append(out, seg)
	}
	return out, true
}

func stringSchema() map[string]interface{} { return map[string]interface{}{"type": "string"} }
func intSchema() map[string]interface{}    { return map[string]interface{}{"type": "integer"} }
func boolSchema() map[string]interface{}   { return map[string]interface{}{"type": "boolean"} }
func stringList() map[string]interface{} {
	return map[string]interface{}{"type": "array", "items": stringSchema()}
}
func numberList() map[string]interface{} {
	return map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "number"}}
}

func objectSchema(props map[string]interface{}, required ...string) map[string]interface{} {
	out := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func (k *ToolKit) wake(ctx context.Context, kind string, id uuid.UUID, label string) {
	if k.Waker != nil {
		k.Waker.WakeSessionOn(ctx, kind, id, label)
	}
}
