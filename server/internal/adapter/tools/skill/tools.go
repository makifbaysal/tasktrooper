package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	loadSkillToolName   = "load_skill"
	createSkillToolName = "create_skill"
)

// SkillCreator is the slice of the catalog service create_skill needs; the
// embedding happens inside.
type SkillCreator interface {
	CreateSkillForAgent(ctx context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) (domain.Skill, error)
}

type ToolKit struct {
	Catalog port.CatalogStore
	// Creator enables create_skill; without it only load_skill is registered.
	Creator SkillCreator
	// Events records runtime skill creations on the evolution timeline so they
	// are impact-evaluated and revertible like reflection-made changes.
	Events port.AgentEvolutionStore
	Perf   port.AgentPerformanceStore
	// MaxSkills caps how many skills one agent may hold. At the cap create_skill
	// refuses and points the agent back at its index: an agent that keeps
	// minting near-duplicate skills ends up with an index it cannot choose from.
	// Zero means unbounded.
	MaxSkills int
}

func NewExecutors(kit *ToolKit) []port.ToolExecutor {
	if kit == nil || kit.Catalog == nil {
		return nil
	}
	executors := []port.ToolExecutor{&loadSkillTool{kit: kit}}
	if kit.Creator != nil {
		executors = append(executors, &createSkillTool{kit: kit})
	}
	return executors
}

type loadSkillTool struct {
	kit *ToolKit
}

func (t *loadSkillTool) Name() string { return loadSkillToolName }

func (t *loadSkillTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: loadSkillToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"skill"},
				"properties": map[string]interface{}{
					"skill": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
}

func (t *loadSkillTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Skill string `json:"skill"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(loadSkillToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	query := strings.TrimSpace(args.Skill)
	if query == "" {
		return toolError(loadSkillToolName, "skill is required")
	}
	agentID := registry.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return toolError(loadSkillToolName, "load_skill requires an agent context")
	}
	skills, err := t.kit.Catalog.ListSkillsByAgent(ctx, agentID)
	if err != nil {
		return toolError(loadSkillToolName, err.Error())
	}
	var match *domain.Skill
	for i := range skills {
		sk := skills[i]
		if !sk.Enabled {
			continue
		}
		if strings.EqualFold(sk.Name, query) || sk.ID.String() == query {
			match = &sk
			break
		}
	}
	if match == nil {
		names := make([]string, 0, len(skills))
		for _, sk := range skills {
			if sk.Enabled {
				names = append(names, sk.Name)
			}
		}
		return toolError(loadSkillToolName, prompt.SkillNotFoundText(query, strings.Join(names, ", ")))
	}
	fields := map[string]any{
		"name":        match.Name,
		"description": match.Description,
		"category":    match.Category,
		"content":     match.Content,
	}
	if match.TechStackID != nil {
		if stacks, err := t.kit.Catalog.ListTechStacksByAgent(ctx, agentID); err == nil {
			for _, st := range stacks {
				if st.ID == *match.TechStackID {
					fields["tech_stack"] = st.Name
					break
				}
			}
		}
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return toolError(loadSkillToolName, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: loadSkillToolName, Content: string(payload)}
}

type createSkillTool struct {
	kit *ToolKit
}

func (t *createSkillTool) Name() string { return createSkillToolName }

func (t *createSkillTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: createSkillToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"name", "description", "content"},
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type": "string",
					},
					"description": map[string]interface{}{
						"type": "string",
					},
					"category": map[string]interface{}{
						"type": "string",
					},
					"content": map[string]interface{}{
						"type": "string",
					},
					"tech_stack": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
}

func (t *createSkillTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Content     string `json:"content"`
		TechStack   string `json:"tech_stack"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(createSkillToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	name := strings.TrimSpace(args.Name)
	description := strings.TrimSpace(args.Description)
	content := strings.TrimSpace(args.Content)
	if name == "" || description == "" || content == "" {
		return toolError(createSkillToolName, "name, description and content are required")
	}
	agentID := registry.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return toolError(createSkillToolName, "create_skill requires an agent context")
	}
	agentRec, err := t.kit.Catalog.GetAgent(ctx, agentID)
	if err != nil {
		return toolError(createSkillToolName, fmt.Sprintf("agent lookup failed: %v", err))
	}
	if !agentRec.SelfEvolutionEnabled {
		return toolError(createSkillToolName, prompt.SkillSelfEvolutionDisabledText())
	}
	skills, err := t.kit.Catalog.ListSkillsByAgent(ctx, agentID)
	if err != nil {
		return toolError(createSkillToolName, err.Error())
	}
	if t.kit.MaxSkills > 0 && len(skills) >= t.kit.MaxSkills {
		return toolError(createSkillToolName, prompt.SkillMaxSkillsText(len(skills)))
	}
	for _, sk := range skills {
		if strings.EqualFold(sk.Name, name) {
			return toolError(createSkillToolName, prompt.SkillNameExistsText(sk.Name))
		}
	}
	stackID, stackErr := t.resolveStack(ctx, agentID, args.TechStack)
	if stackErr != nil {
		return toolError(createSkillToolName, stackErr.Error())
	}
	// The agent wrote this skill about itself, so its version history must read as self-evolution, not as a user edit — that is what keeps catalog sync and a reader of the history from mistaking it for the user's own.
	versionCtx := catalog.WithVersionSource(ctx, catalog.VersionSource{
		Source: domain.CatalogVersionSourceEvolution, Reason: "create_skill during a run",
	})
	created, err := t.kit.Creator.CreateSkillForAgent(versionCtx, agentID, domain.CreateSkillRequest{
		Name: name, Description: description, Category: strings.TrimSpace(args.Category),
		Content: content, Enabled: true, TechStackID: stackID,
	})
	if err != nil {
		return toolError(createSkillToolName, fmt.Sprintf("create skill: %v", err))
	}
	t.recordEvent(ctx, agentID, created)
	payload, err := json.Marshal(map[string]any{
		"id":      created.ID,
		"name":    created.Name,
		"message": prompt.SkillCreatedMessage(),
	})
	if err != nil {
		return toolError(createSkillToolName, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: createSkillToolName, Content: string(payload)}
}

// resolveStack turns the tech_stack argument into one of the agent's own stack
// ids. An unknown name is refused rather than silently filed as general: a
// skill on the wrong shelf is invisible to the runs that need it, and the model
// can retry with a name from the list the error carries.
func (t *createSkillTool) resolveStack(ctx context.Context, agentID uuid.UUID, name string) (*uuid.UUID, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	stacks, err := t.kit.Catalog.ListTechStacksByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for _, st := range stacks {
		if strings.EqualFold(st.Name, name) {
			id := st.ID
			return &id, nil
		}
	}
	if len(stacks) == 0 {
		return nil, errors.New(prompt.SkillNoTechStacksText(fmt.Sprintf("%q", name)))
	}
	names := make([]string, 0, len(stacks))
	for _, st := range stacks {
		names = append(names, st.Name)
	}
	return nil, errors.New(prompt.SkillUnknownTechStackText(fmt.Sprintf("%q", name), strings.Join(names, ", ")))
}

// recordEvent puts the runtime creation on the evolution timeline with no
// reflection id. Failures are swallowed: the skill exists either way, and a
// missing timeline row must not fail the agent's tool call.
func (t *createSkillTool) recordEvent(ctx context.Context, agentID uuid.UUID, created domain.Skill) {
	if t.kit.Events == nil {
		return
	}
	scoreAt := 100.0
	if t.kit.Perf != nil {
		if score, err := t.kit.Perf.GetScore(ctx, agentID); err == nil {
			scoreAt = score.Score
		}
	}
	after, _ := json.Marshal(map[string]any{
		"id": created.ID, "name": created.Name, "description": created.Description,
		"category": created.Category, "content": created.Content, "enabled": created.Enabled,
	})
	_, _ = t.kit.Events.CreateEvent(ctx, domain.AgentEvolutionEvent{
		AgentID:       agentID,
		ChangeType:    domain.EvolutionChangeSkillCreated,
		TargetKind:    domain.EvolutionTargetSkill,
		TargetID:      &created.ID,
		TargetName:    created.Name,
		After:         after,
		ScoreAtChange: scoreAt,
	})
}

func toolError(tool, message string) domain.ToolResult {
	return domain.ToolResult{Name: tool, Content: message, IsError: true}
}
