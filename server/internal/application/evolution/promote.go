package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

var ErrPromotionInFlight = fmt.Errorf("a shared-memory promotion is already running")

// Shares the reflection in-flight map; it can never collide with an agent id key, which are UUID strings.
const promotionInflightKey = "__shared_memory_promotion__"

// The sweep writes NOTHING: the old code created skills and deleted memories in one call, so the operator found out only afterwards what it decided.
func (s *Service) PlanSharedMemoryPromotion(ctx context.Context) (domain.MemoryPromotionPlan, error) {
	plan := domain.MemoryPromotionPlan{Candidates: []domain.MemoryPromotionCandidate{}, Agents: []string{}}
	if s.llm == nil || s.memories == nil || s.manager == nil || s.board == nil || s.catalog == nil {
		return plan, fmt.Errorf("memory promotion is not configured")
	}
	release, err := s.holdPromotion()
	if err != nil {
		return plan, err
	}
	defer release()

	memories, err := s.memories.List(ctx, domain.MemoryQuery{
		Owner: domain.MemoryOwnerTeam, Repo: domain.MemoryRepoScopeAny, Limit: 200,
	})
	if err != nil {
		return plan, fmt.Errorf("listing team memories: %w", err)
	}
	plan.Scanned = len(memories)
	if len(memories) == 0 {
		return plan, nil
	}

	// An operator sweep may target agents with self-evolution off — the operator decides.
	agents, err := s.promotionTargets(ctx, false)
	if err != nil {
		return plan, err
	}
	if len(agents) == 0 {
		return plan, fmt.Errorf("no enabled agents to receive skills")
	}
	for _, a := range agents {
		plan.Agents = append(plan.Agents, a.Name)
	}

	model, provider := s.promotionModel()
	var output promotionOutput
	if err := s.promotionChat(ctx,
		teamPromotionSystemPrompt,
		teamPromotionUserMessage(memories, agents),
		model, provider,
		memoryPromotionSchemaKey.Name(), memoryPromotionSchemaKey.Map(), &output,
	); err != nil {
		return plan, fmt.Errorf("memory promotion llm call failed: %w", err)
	}

	memByID := map[string]domain.AgentMemory{}
	for _, m := range memories {
		memByID[m.ID.String()] = m
	}
	agentByName := map[string]domain.Agent{}
	for _, a := range agents {
		agentByName[strings.ToLower(strings.TrimSpace(a.Name))] = a
	}

	for _, item := range output.Promotions {
		mem, ok := memByID[strings.TrimSpace(item.MemoryID)]
		if !ok {
			continue // model invented an id — never act on it
		}
		if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Content) == "" {
			continue
		}
		targets := make([]string, 0, len(item.Agents))
		for _, name := range item.Agents {
			if a, ok := agentByName[strings.ToLower(strings.TrimSpace(name))]; ok {
				targets = append(targets, a.Name)
			}
		}
		// No recognizable targets means "fits everyone"; spelled out so the operator sees the real roster.
		if len(targets) == 0 {
			targets = append(targets, plan.Agents...)
		}
		plan.Candidates = append(plan.Candidates, domain.MemoryPromotionCandidate{
			MemoryID: mem.ID, MemoryContent: mem.Content,
			SkillName: item.Name, Description: item.Description,
			Category: item.Category, Content: item.Content, Agents: targets,
		})
	}
	log.Info().Int("scanned", plan.Scanned).Int("candidates", len(plan.Candidates)).Msg("shared-memory promotion planned")
	return plan, nil
}

// Everything is re-resolved against server state, because the candidates travelled through the browser — what comes back is a request, not a fact. A candidate nobody could take leaves its memory alone: deleting it would lose the lesson.
func (s *Service) ApplySharedMemoryPromotion(ctx context.Context, candidates []domain.MemoryPromotionCandidate) (domain.MemoryPromotionResult, error) {
	result := domain.MemoryPromotionResult{Promoted: []domain.MemoryPromotion{}}
	if s.memories == nil || s.manager == nil || s.board == nil || s.catalog == nil {
		return result, fmt.Errorf("memory promotion is not configured")
	}
	if len(candidates) == 0 {
		return result, nil
	}
	release, err := s.holdPromotion()
	if err != nil {
		return result, err
	}
	defer release()

	memories, err := s.memories.List(ctx, domain.MemoryQuery{
		Owner: domain.MemoryOwnerTeam, Repo: domain.MemoryRepoScopeAny, Limit: 200,
	})
	if err != nil {
		return result, fmt.Errorf("listing team memories: %w", err)
	}
	result.Scanned = len(candidates)
	memByID := map[uuid.UUID]domain.AgentMemory{}
	for _, m := range memories {
		memByID[m.ID] = m
	}

	agents, err := s.promotionTargets(ctx, false)
	if err != nil {
		return result, err
	}
	agentByName := map[string]domain.Agent{}
	for _, a := range agents {
		agentByName[strings.ToLower(strings.TrimSpace(a.Name))] = a
	}

	for _, item := range candidates {
		mem, ok := memByID[item.MemoryID]
		if !ok {
			log.Info().Str("memory", item.MemoryID.String()).Msg("promotion skipped: memory is gone")
			continue
		}
		if strings.TrimSpace(item.SkillName) == "" || strings.TrimSpace(item.Content) == "" {
			continue
		}
		targets := make([]domain.Agent, 0, len(item.Agents))
		for _, name := range item.Agents {
			if a, ok := agentByName[strings.ToLower(strings.TrimSpace(name))]; ok {
				targets = append(targets, a)
			}
		}
		if len(targets) == 0 {
			targets = agents
		}
		granted := s.createSkillForAgents(ctx, targets, domain.CreateSkillRequest{
			Name: item.SkillName, Description: item.Description, Category: item.Category,
			Content: item.Content, Enabled: true,
		})
		if len(granted) == 0 {
			continue // memory stays: nobody could take the skill (budget/collision)
		}
		if err := s.memories.Delete(ctx, uuid.Nil, mem.ID); err != nil {
			log.Warn().Err(err).Str("memory", mem.ID.String()).Msg("promoted memory delete failed")
		}
		result.Promoted = append(result.Promoted, domain.MemoryPromotion{
			MemoryID: mem.ID, SkillName: item.SkillName, Agents: granted,
		})
	}
	log.Info().Int("approved", len(candidates)).Int("promoted", len(result.Promoted)).Msg("shared-memory promotion applied")
	return result, nil
}

// PromoteSharedMemories is one-shot plan-then-apply, for callers that want the old behaviour.
func (s *Service) PromoteSharedMemories(ctx context.Context) (domain.MemoryPromotionResult, error) {
	plan, err := s.PlanSharedMemoryPromotion(ctx)
	if err != nil {
		return domain.MemoryPromotionResult{Promoted: []domain.MemoryPromotion{}}, err
	}
	result, err := s.ApplySharedMemoryPromotion(ctx, plan.Candidates)
	if err != nil {
		return result, err
	}
	result.Scanned = plan.Scanned
	return result, nil
}

// Single-flight across both halves, so a second browser tab cannot plan against memories another apply is deleting.
func (s *Service) holdPromotion() (func(), error) {
	s.mu.Lock()
	if s.inflight[promotionInflightKey] {
		s.mu.Unlock()
		return nil, ErrPromotionInFlight
	}
	s.inflight[promotionInflightKey] = true
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.inflight, promotionInflightKey)
		s.mu.Unlock()
	}, nil
}

// The save_memory hook: reusable know-how is written into the skill catalog instead. agentID == uuid.Nil targets team memory (skill to every enabled self-evolving board agent). Best-effort by design: an error means "let the caller save it as a memory".
func (s *Service) MaybePromoteMemory(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID, content, category string) (bool, string, error) {
	if !s.cfg.Enabled || s.llm == nil || s.manager == nil || s.catalog == nil {
		return false, "", nil
	}
	var targets []domain.Agent
	if agentID == uuid.Nil {
		if s.board == nil {
			return false, "", nil
		}
		agents, err := s.promotionTargets(ctx, true)
		if err != nil || len(agents) == 0 {
			return false, "", err
		}
		targets = agents
	} else {
		rec, err := s.catalog.GetAgent(ctx, agentID)
		if err != nil {
			return false, "", err
		}
		if !rec.Enabled || !rec.SelfEvolutionEnabled {
			return false, "", nil
		}
		targets = []domain.Agent{rec}
	}

	scopeNote := "global (valid in every repository)"
	if repositoryID != nil {
		scopeNote = "bound to a single repository"
	}
	var out saveClassification
	model, provider := s.promotionModel()
	if err := s.promotionChat(ctx,
		saveClassifierSystemPrompt,
		fmt.Sprintf("Scope: %s\nCategory: %s\nContent:\n%s", scopeNote, category, content),
		model, provider,
		memoryOrSkillSchemaKey.Name(), memoryOrSkillSchemaKey.Map(), &out,
	); err != nil {
		return false, "", err
	}
	if !out.Skill || strings.TrimSpace(out.Name) == "" || strings.TrimSpace(out.Content) == "" {
		return false, "", nil
	}
	granted := s.createSkillForAgents(ctx, targets, domain.CreateSkillRequest{
		Name: out.Name, Description: out.Description, Category: out.Category,
		Content: out.Content, Enabled: true,
	})
	if len(granted) == 0 {
		return false, "", nil
	}
	return true, out.Name, nil
}

// requireSelfEvolution applies to the save_memory path only; an operator sweep may not inherit it.
func (s *Service) promotionTargets(ctx context.Context, requireSelfEvolution bool) ([]domain.Agent, error) {
	return s.rosterAgents(ctx, requireSelfEvolution)
}

func (s *Service) createSkillForAgents(ctx context.Context, targets []domain.Agent, req domain.CreateSkillRequest) []string {
	ctx = catalog.WithVersionSource(ctx, catalog.VersionSource{Source: domain.CatalogVersionSourceEvolution})
	var granted []string
	for _, agentRec := range targets {
		existing, err := s.catalog.ListSkillsByAgent(ctx, agentRec.ID)
		if err != nil {
			log.Warn().Err(err).Str("agent", agentRec.Name).Msg("memory promotion: list skills failed")
			continue
		}
		collision := false
		for _, sk := range existing {
			if strings.EqualFold(strings.TrimSpace(sk.Name), strings.TrimSpace(req.Name)) {
				collision = true
				break
			}
		}
		if collision {
			continue
		}
		if s.cfg.MaxSkillsPerAgent > 0 && len(existing) >= s.cfg.MaxSkillsPerAgent {
			log.Info().Str("agent", agentRec.Name).Str("skill", req.Name).Msg("memory promotion: agent at skill budget")
			continue
		}
		created, err := s.manager.CreateSkillForAgent(ctx, agentRec.ID, req)
		if err != nil {
			log.Warn().Err(err).Str("agent", agentRec.Name).Str("skill", req.Name).Msg("memory promotion: skill create failed")
			continue
		}
		s.recordPromotionEvent(ctx, agentRec, created)
		granted = append(granted, agentRec.Name)
	}
	return granted
}

func (s *Service) recordPromotionEvent(ctx context.Context, agentRec domain.Agent, created domain.Skill) {
	if s.store == nil {
		return
	}
	scoreAt := 100.0
	if s.perf != nil {
		if score, err := s.perf.GetScore(ctx, agentRec.ID); err == nil {
			scoreAt = score.Score
		}
	}
	if _, err := s.store.CreateEvent(ctx, domain.AgentEvolutionEvent{
		AgentID: agentRec.ID, ChangeType: domain.EvolutionChangeSkillCreated,
		TargetKind: domain.EvolutionTargetSkill, TargetID: &created.ID, TargetName: created.Name,
		After: snapshotSkill(created, nil), ScoreAtChange: scoreAt,
		Impact: domain.EvolutionImpactNeutral,
	}); err != nil {
		log.Warn().Err(err).Str("skill", created.Name).Msg("memory promotion: event persist failed")
	}
}

// Judge model first, then the evolution override, then the configured default (both empty route at whatever is configured). It used to be agents[0]'s model — provider roulette that also picked up claude_code agents, who cannot produce these JSON-schema calls at all.
func (s *Service) promotionModel() (string, domain.LLMProviderType) {
	var model string
	var provider domain.LLMProviderType
	if s.cfg.Model != "" {
		model = s.cfg.Model
	}
	if s.cfg.ProviderType != "" {
		provider = domain.LLMProviderType(s.cfg.ProviderType)
	}
	if s.cfg.JudgeModel != "" {
		model = s.cfg.JudgeModel
	}
	if s.cfg.JudgeProviderType != "" {
		provider = domain.LLMProviderType(s.cfg.JudgeProviderType)
	}
	return model, provider
}

func (s *Service) promotionChat(ctx context.Context, system, user, model string, provider domain.LLMProviderType, schemaName string, schema map[string]interface{}, out any) error {
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: system},
		{Role: domain.RoleUser, Content: user},
	}
	call := func(msgs []domain.Message) (string, error) {
		resp, err := s.llm.Chat(ctx, domain.AgentRequest{
			Messages: msgs, Model: model, ProviderType: provider,
			ResponseFormat: domain.JSONSchemaResponseFormat(schemaName, schema),
		})
		if err != nil {
			return "", err
		}
		return resp.Message.Content, nil
	}
	raw, err := call(messages)
	if err != nil {
		return err
	}
	parseErr := parsePromotionJSON(raw, out)
	if parseErr == nil {
		return nil
	}
	retry := append(messages,
		domain.Message{Role: domain.RoleAssistant, Content: raw},
		domain.Message{Role: domain.RoleUser, Content: retryNotJSONMessage(parseErr)},
	)
	raw2, err := call(retry)
	if err != nil {
		return err
	}
	return parsePromotionJSON(raw2, out)
}

func parsePromotionJSON(raw string, out any) error {
	cleaned := strings.TrimSpace(raw)
	if strings.Contains(cleaned, "```") {
		cleaned = strings.ReplaceAll(cleaned, "```json", "")
		cleaned = strings.ReplaceAll(cleaned, "```", "")
	}
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("no JSON object found")
	}
	if err := json.Unmarshal([]byte(cleaned[start:end+1]), out); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

type promotionOutput struct {
	Promotions []promotionItem `json:"promotions"`
}

type promotionItem struct {
	MemoryID    string   `json:"memory_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Content     string   `json:"content"`
	Agents      []string `json:"agents"`
}

type saveClassification struct {
	Skill       bool   `json:"skill"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Content     string `json:"content"`
}

var teamPromotionSystemKey = prompt.Define[struct{}]("evolution.team_promotion_system", struct{}{})

// teamPromotionSystemPrompt frames the shared-memory-to-skill promotion
// sweep; see catalog/system/prompts/evolution/team_promotion_system.md.
var teamPromotionSystemPrompt = prompt.Text(teamPromotionSystemKey)

type teamPromotionAgentLineInput struct{ Name, Description string }

var teamPromotionAgentLineKey = prompt.Define("evolution.team_promotion_agent_line",
	teamPromotionAgentLineInput{Name: "sample-agent", Description: "does sample work."})

type teamPromotionMemoryLineInput struct{ MemoryID, Category, Scope, Content string }

var teamPromotionMemoryLineKey = prompt.Define("evolution.team_promotion_memory_line", teamPromotionMemoryLineInput{
	MemoryID: "sample-id", Category: "workflow", Scope: "global", Content: "Sample lesson.",
})

// teamPromotionAgentLine and teamPromotionMemoryLine format one roster/memory
// row; teamPromotionUserMessage joins them under their section headers. See
// catalog/system/prompts/evolution/team_promotion_agent_line.md,
// team_promotion_memory_line.md and team_promotion_user.md.
func teamPromotionAgentLine(a domain.Agent) string {
	return teamPromotionAgentLineKey.Render(teamPromotionAgentLineInput{
		Name: a.Name, Description: truncate(a.Description, 160),
	})
}

func teamPromotionMemoryLine(m domain.AgentMemory) string {
	scope := "global"
	if m.RepositoryID != nil {
		scope = "repository-specific"
	}
	category := m.Category
	if category == "" {
		category = "-"
	}
	return teamPromotionMemoryLineKey.Render(teamPromotionMemoryLineInput{
		MemoryID: m.ID.String(), Category: category, Scope: scope,
		Content: strings.ReplaceAll(m.Content, "\n", "\n  "),
	})
}

type teamPromotionUserInput struct {
	AgentLines  []string
	MemoryLines []string
}

var teamPromotionUserKey = prompt.Define("evolution.team_promotion_user", teamPromotionUserInput{
	AgentLines:  []string{"- sample-agent: does sample work."},
	MemoryLines: []string{"- id: sample-id | category: workflow | scope: global\n  Sample lesson."},
})

func teamPromotionUserMessage(memories []domain.AgentMemory, agents []domain.Agent) string {
	agentLines := make([]string, 0, len(agents))
	for _, a := range agents {
		agentLines = append(agentLines, teamPromotionAgentLine(a))
	}
	memoryLines := make([]string, 0, len(memories))
	for _, m := range memories {
		memoryLines = append(memoryLines, teamPromotionMemoryLine(m))
	}
	return teamPromotionUserKey.Render(teamPromotionUserInput{AgentLines: agentLines, MemoryLines: memoryLines})
}

var saveClassifierSystemKey = prompt.Define[struct{}]("evolution.save_classifier_system", struct{}{})

// saveClassifierSystemPrompt decides whether a save_memory note is really
// reusable know-how that belongs in the skill catalog instead; see
// catalog/system/prompts/evolution/save_classifier_system.md.
var saveClassifierSystemPrompt = prompt.Text(saveClassifierSystemKey)
