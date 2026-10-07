package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type CatalogStore struct {
	pool *DB
}

func NewCatalogStore(pool *DB) *CatalogStore {
	return &CatalogStore{pool: pool}
}

func (s *CatalogStore) CreateSkill(ctx context.Context, skill domain.Skill) (domain.Skill, error) {
	embJSON, err := json.Marshal(skill.Embedding)
	if err != nil {
		return domain.Skill{}, err
	}
	tags := skill.Tags
	if tags == nil {
		tags = []string{}
	}
	var out domain.Skill
	err = s.pool.QueryRow(ctx, `
		INSERT INTO skills (agent_id, name, description, category, tags, content, embedding, enabled, tech_stack_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, agent_id, name, description, category, tags, content, embedding, enabled, tech_stack_id, catalog_sha, created_at
	`, skill.AgentID, skill.Name, skill.Description, skill.Category, tags, skill.Content, embJSON, skill.Enabled, skill.TechStackID).Scan(
		&out.ID, &out.AgentID, &out.Name, &out.Description, &out.Category, &out.Tags, &out.Content, &embJSON, &out.Enabled, &out.TechStackID, &out.CatalogSha, &out.CreatedAt,
	)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("create skill: %w", err)
	}
	_ = json.Unmarshal(embJSON, &out.Embedding)
	return out, nil
}

func (s *CatalogStore) GetSkill(ctx context.Context, id uuid.UUID) (domain.Skill, error) {
	var out domain.Skill
	var embJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, name, description, category, tags, content, embedding, enabled, tech_stack_id, catalog_sha, created_at
		FROM skills WHERE id = $1
	`, id).Scan(&out.ID, &out.AgentID, &out.Name, &out.Description, &out.Category, &out.Tags, &out.Content, &embJSON, &out.Enabled, &out.TechStackID, &out.CatalogSha, &out.CreatedAt)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("get skill: %w", err)
	}
	_ = json.Unmarshal(embJSON, &out.Embedding)
	return out, nil
}

func (s *CatalogStore) ListSkills(ctx context.Context) ([]domain.Skill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, name, description, category, tags, content, embedding, enabled, tech_stack_id, catalog_sha, created_at
		FROM skills ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()
	return scanSkills(rows)
}

func (s *CatalogStore) ListSkillsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.Skill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, name, description, category, tags, content, embedding, enabled, tech_stack_id, catalog_sha, created_at
		FROM skills WHERE agent_id = $1 ORDER BY name ASC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list skills by agent: %w", err)
	}
	defer rows.Close()
	return scanSkills(rows)
}

func (s *CatalogStore) GetSkillByAgentAndName(ctx context.Context, agentID uuid.UUID, name string) (domain.Skill, error) {
	var out domain.Skill
	var embJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, name, description, category, tags, content, embedding, enabled, tech_stack_id, catalog_sha, created_at
		FROM skills WHERE agent_id = $1 AND name = $2
	`, agentID, name).Scan(&out.ID, &out.AgentID, &out.Name, &out.Description, &out.Category, &out.Tags, &out.Content, &embJSON, &out.Enabled, &out.TechStackID, &out.CatalogSha, &out.CreatedAt)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("get skill by agent and name: %w", err)
	}
	_ = json.Unmarshal(embJSON, &out.Embedding)
	return out, nil
}

func (s *CatalogStore) UpdateSkill(ctx context.Context, skill domain.Skill) (domain.Skill, error) {
	embJSON, err := json.Marshal(skill.Embedding)
	if err != nil {
		return domain.Skill{}, err
	}
	tags := skill.Tags
	if tags == nil {
		tags = []string{}
	}
	var out domain.Skill
	err = s.pool.QueryRow(ctx, `
		UPDATE skills SET name=$2, description=$3, category=$4, tags=$5, content=$6, embedding=$7, enabled=$8, tech_stack_id=$10, catalog_sha=$11
		WHERE id=$1 AND agent_id=$9
		RETURNING id, agent_id, name, description, category, tags, content, embedding, enabled, tech_stack_id, catalog_sha, created_at
	`, skill.ID, skill.Name, skill.Description, skill.Category, tags, skill.Content, embJSON, skill.Enabled, skill.AgentID, skill.TechStackID, skill.CatalogSha).Scan(
		&out.ID, &out.AgentID, &out.Name, &out.Description, &out.Category, &out.Tags, &out.Content, &embJSON, &out.Enabled, &out.TechStackID, &out.CatalogSha, &out.CreatedAt,
	)
	if err != nil {
		return domain.Skill{}, fmt.Errorf("update skill: %w", err)
	}
	_ = json.Unmarshal(embJSON, &out.Embedding)
	return out, nil
}

func (s *CatalogStore) DeleteSkill(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM skills WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("skill not found")
	}
	return nil
}

func (s *CatalogStore) SearchSkills(ctx context.Context, queryEmbedding []float32, topK int, agentID *uuid.UUID) ([]domain.Skill, error) {
	if topK <= 0 {
		topK = 5
	}
	var rows interface {
		Close()
		Next() bool
		Scan(dest ...any) error
		Err() error
	}
	var err error
	if agentID != nil {
		rows, err = s.pool.Query(ctx, `
			SELECT id, agent_id, name, description, category, tags, content, embedding, enabled, tech_stack_id, catalog_sha, created_at
			FROM skills WHERE enabled = true AND embedding IS NOT NULL AND agent_id = $1
		`, *agentID)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id, agent_id, name, description, category, tags, content, embedding, enabled, tech_stack_id, catalog_sha, created_at
			FROM skills WHERE enabled = true AND embedding IS NOT NULL
		`)
	}
	if err != nil {
		return nil, fmt.Errorf("search skills: %w", err)
	}
	defer rows.Close()

	type scored struct {
		skill domain.Skill
		score float64
	}
	var results []scored
	for rows.Next() {
		var sk domain.Skill
		var embJSON []byte
		if err := rows.Scan(&sk.ID, &sk.AgentID, &sk.Name, &sk.Description, &sk.Category, &sk.Tags, &sk.Content, &embJSON, &sk.Enabled, &sk.TechStackID, &sk.CatalogSha, &sk.CreatedAt); err != nil {
			return nil, err
		}
		var emb []float32
		if err := json.Unmarshal(embJSON, &emb); err != nil || len(emb) == 0 {
			continue
		}
		sk.Embedding = emb
		results = append(results, scored{skill: sk, score: cosineSimilarity(queryEmbedding, emb)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
	if len(results) > topK {
		results = results[:topK]
	}
	out := make([]domain.Skill, len(results))
	for i, r := range results {
		out[i] = r.skill
	}
	return out, rows.Err()
}

func (s *CatalogStore) CreateTechStack(ctx context.Context, stack domain.TechStack) (domain.TechStack, error) {
	var out domain.TechStack
	err := s.pool.QueryRow(ctx, `
		INSERT INTO agent_tech_stacks (agent_id, name, description, position)
		VALUES ($1, $2, $3, $4)
		RETURNING id, agent_id, name, description, position, created_at
	`, stack.AgentID, stack.Name, stack.Description, stack.Position).Scan(
		&out.ID, &out.AgentID, &out.Name, &out.Description, &out.Position, &out.CreatedAt,
	)
	if err != nil {
		return domain.TechStack{}, fmt.Errorf("create tech stack: %w", err)
	}
	return out, nil
}

func (s *CatalogStore) GetTechStack(ctx context.Context, id uuid.UUID) (domain.TechStack, error) {
	var out domain.TechStack
	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, name, description, position, created_at FROM agent_tech_stacks WHERE id = $1
	`, id).Scan(&out.ID, &out.AgentID, &out.Name, &out.Description, &out.Position, &out.CreatedAt)
	if err != nil {
		return domain.TechStack{}, fmt.Errorf("get tech stack: %w", err)
	}
	return out, nil
}

func (s *CatalogStore) ListTechStacksByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.TechStack, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, name, description, position, created_at FROM agent_tech_stacks
		WHERE agent_id = $1 ORDER BY position ASC, name ASC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list tech stacks by agent: %w", err)
	}
	defer rows.Close()
	var stacks []domain.TechStack
	for rows.Next() {
		var st domain.TechStack
		if err := rows.Scan(&st.ID, &st.AgentID, &st.Name, &st.Description, &st.Position, &st.CreatedAt); err != nil {
			return nil, err
		}
		stacks = append(stacks, st)
	}
	return stacks, rows.Err()
}

func (s *CatalogStore) UpdateTechStack(ctx context.Context, stack domain.TechStack) (domain.TechStack, error) {
	var out domain.TechStack
	err := s.pool.QueryRow(ctx, `
		UPDATE agent_tech_stacks SET name=$2, description=$3, position=$4
		WHERE id=$1 AND agent_id=$5
		RETURNING id, agent_id, name, description, position, created_at
	`, stack.ID, stack.Name, stack.Description, stack.Position, stack.AgentID).Scan(
		&out.ID, &out.AgentID, &out.Name, &out.Description, &out.Position, &out.CreatedAt,
	)
	if err != nil {
		return domain.TechStack{}, fmt.Errorf("update tech stack: %w", err)
	}
	return out, nil
}

func (s *CatalogStore) DeleteTechStack(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM agent_tech_stacks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tech stack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tech stack not found")
	}
	return nil
}

func (s *CatalogStore) CreateAgent(ctx context.Context, agent domain.Agent) (domain.Agent, error) {
	policyJSON, err := json.Marshal(agent.ToolPolicy)
	if err != nil {
		return domain.Agent{}, err
	}
	var out domain.Agent
	err = s.pool.QueryRow(ctx, `
		INSERT INTO agents (name, description, subagent_type, system_prompt, provider_type, model, model_heavy, max_turns, effort, tool_policy, enabled, self_evolution_enabled, catalog_slug, auto_pull_agent_updates, keep_skills_updated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, name, description, subagent_type, system_prompt, provider_type, model, model_heavy, max_turns, effort, tool_policy, enabled, self_evolution_enabled, catalog_slug, catalog_etag, auto_pull_agent_updates, keep_skills_updated, created_at
	`, agent.Name, agent.Description, agent.SubagentType, agent.SystemPrompt, agent.ProviderType, agent.Model, agent.ModelHeavy, agent.MaxTurns, agent.Effort, policyJSON, agent.Enabled, agent.SelfEvolutionEnabled, agent.CatalogSlug, agent.AutoPullAgentUpdates, agent.KeepSkillsUpdated).Scan(
		&out.ID, &out.Name, &out.Description, &out.SubagentType, &out.SystemPrompt, &out.ProviderType, &out.Model, &out.ModelHeavy, &out.MaxTurns, &out.Effort, &policyJSON, &out.Enabled, &out.SelfEvolutionEnabled, &out.CatalogSlug, &out.CatalogEtag, &out.AutoPullAgentUpdates, &out.KeepSkillsUpdated, &out.CreatedAt,
	)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("create agent: %w", err)
	}
	_ = json.Unmarshal(policyJSON, &out.ToolPolicy)
	skillIDs, err := s.listAgentSkillIDs(ctx, out.ID)
	if err != nil {
		return domain.Agent{}, err
	}
	out.SkillIDs = skillIDs
	return out, nil
}

func (s *CatalogStore) listAgentSkillIDs(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM skills WHERE agent_id = $1 ORDER BY name ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent skill ids: %w", err)
	}
	defer rows.Close()
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *CatalogStore) GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error) {
	var out domain.Agent
	var policyJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, description, subagent_type, system_prompt, provider_type, model, model_heavy, max_turns, effort, tool_policy, enabled, self_evolution_enabled, catalog_slug, catalog_etag, auto_pull_agent_updates, keep_skills_updated, created_at
		FROM agents WHERE id = $1
	`, id).Scan(&out.ID, &out.Name, &out.Description, &out.SubagentType, &out.SystemPrompt, &out.ProviderType, &out.Model, &out.ModelHeavy, &out.MaxTurns, &out.Effort, &policyJSON, &out.Enabled, &out.SelfEvolutionEnabled, &out.CatalogSlug, &out.CatalogEtag, &out.AutoPullAgentUpdates, &out.KeepSkillsUpdated, &out.CreatedAt)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("get agent: %w", err)
	}
	_ = json.Unmarshal(policyJSON, &out.ToolPolicy)
	skillIDs, err := s.listAgentSkillIDs(ctx, id)
	if err != nil {
		return domain.Agent{}, err
	}
	out.SkillIDs = skillIDs
	return out, nil
}

func (s *CatalogStore) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name, a.description, a.subagent_type, a.system_prompt, a.provider_type, a.model, a.model_heavy, a.max_turns, a.effort, a.tool_policy, a.enabled, a.self_evolution_enabled, a.catalog_slug, a.catalog_etag, a.auto_pull_agent_updates, a.keep_skills_updated, a.created_at,
		       COALESCE(array_agg(sk.id ORDER BY sk.name) FILTER (WHERE sk.id IS NOT NULL), '{}')
		FROM agents a
		LEFT JOIN skills sk ON sk.agent_id = a.id
		GROUP BY a.id
		ORDER BY a.name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	var agents []domain.Agent
	for rows.Next() {
		var a domain.Agent
		var policyJSON []byte
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.SubagentType, &a.SystemPrompt, &a.ProviderType, &a.Model, &a.ModelHeavy, &a.MaxTurns, &a.Effort, &policyJSON, &a.Enabled, &a.SelfEvolutionEnabled, &a.CatalogSlug, &a.CatalogEtag, &a.AutoPullAgentUpdates, &a.KeepSkillsUpdated, &a.CreatedAt, &a.SkillIDs); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(policyJSON, &a.ToolPolicy)
		if a.SkillIDs == nil {
			a.SkillIDs = []uuid.UUID{}
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (s *CatalogStore) UpdateAgent(ctx context.Context, agent domain.Agent) (domain.Agent, error) {
	policyJSON, err := json.Marshal(agent.ToolPolicy)
	if err != nil {
		return domain.Agent{}, err
	}
	var out domain.Agent
	err = s.pool.QueryRow(ctx, `
		UPDATE agents SET name=$2, description=$3, subagent_type=$4, system_prompt=$5, provider_type=$6, model=$7, model_heavy=$8, max_turns=$9, effort=$10, tool_policy=$11, enabled=$12, self_evolution_enabled=$13, catalog_slug=$14, catalog_etag=$15, auto_pull_agent_updates=$16, keep_skills_updated=$17
		WHERE id=$1
		RETURNING id, name, description, subagent_type, system_prompt, provider_type, model, model_heavy, max_turns, effort, tool_policy, enabled, self_evolution_enabled, catalog_slug, catalog_etag, auto_pull_agent_updates, keep_skills_updated, created_at
	`, agent.ID, agent.Name, agent.Description, agent.SubagentType, agent.SystemPrompt, agent.ProviderType, agent.Model, agent.ModelHeavy, agent.MaxTurns, agent.Effort, policyJSON, agent.Enabled, agent.SelfEvolutionEnabled, agent.CatalogSlug, agent.CatalogEtag, agent.AutoPullAgentUpdates, agent.KeepSkillsUpdated).Scan(
		&out.ID, &out.Name, &out.Description, &out.SubagentType, &out.SystemPrompt, &out.ProviderType, &out.Model, &out.ModelHeavy, &out.MaxTurns, &out.Effort, &policyJSON, &out.Enabled, &out.SelfEvolutionEnabled, &out.CatalogSlug, &out.CatalogEtag, &out.AutoPullAgentUpdates, &out.KeepSkillsUpdated, &out.CreatedAt,
	)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("update agent: %w", err)
	}
	_ = json.Unmarshal(policyJSON, &out.ToolPolicy)
	skillIDs, err := s.listAgentSkillIDs(ctx, agent.ID)
	if err != nil {
		return domain.Agent{}, err
	}
	out.SkillIDs = skillIDs
	return out, nil
}

func (s *CatalogStore) DeleteAgent(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM agents WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent not found")
	}
	return nil
}

func (s *CatalogStore) CreateRule(ctx context.Context, rule domain.OrchestratorRule) (domain.OrchestratorRule, error) {
	var out domain.OrchestratorRule
	err := s.pool.QueryRow(ctx, `
		INSERT INTO orchestrator_rules (agent_id, name, content, priority, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, agent_id, name, content, priority, enabled, created_at
	`, rule.AgentID, rule.Name, rule.Content, rule.Priority, rule.Enabled).Scan(
		&out.ID, &out.AgentID, &out.Name, &out.Content, &out.Priority, &out.Enabled, &out.CreatedAt,
	)
	if err != nil {
		return domain.OrchestratorRule{}, fmt.Errorf("create rule: %w", err)
	}
	return out, nil
}

func (s *CatalogStore) GetRule(ctx context.Context, id uuid.UUID) (domain.OrchestratorRule, error) {
	var out domain.OrchestratorRule
	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, name, content, priority, enabled, created_at FROM orchestrator_rules WHERE id = $1
	`, id).Scan(&out.ID, &out.AgentID, &out.Name, &out.Content, &out.Priority, &out.Enabled, &out.CreatedAt)
	if err != nil {
		return domain.OrchestratorRule{}, fmt.Errorf("get rule: %w", err)
	}
	return out, nil
}

func (s *CatalogStore) ListRules(ctx context.Context) ([]domain.OrchestratorRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, name, content, priority, enabled, created_at FROM orchestrator_rules ORDER BY priority DESC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

func (s *CatalogStore) ListRulesByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.OrchestratorRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, name, content, priority, enabled, created_at FROM orchestrator_rules
		WHERE agent_id = $1 ORDER BY priority DESC, name ASC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list rules by agent: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

func (s *CatalogStore) UpdateRule(ctx context.Context, rule domain.OrchestratorRule) (domain.OrchestratorRule, error) {
	var out domain.OrchestratorRule
	err := s.pool.QueryRow(ctx, `
		UPDATE orchestrator_rules SET name=$2, content=$3, priority=$4, enabled=$5
		WHERE id=$1 AND agent_id=$6
		RETURNING id, agent_id, name, content, priority, enabled, created_at
	`, rule.ID, rule.Name, rule.Content, rule.Priority, rule.Enabled, rule.AgentID).Scan(
		&out.ID, &out.AgentID, &out.Name, &out.Content, &out.Priority, &out.Enabled, &out.CreatedAt,
	)
	if err != nil {
		return domain.OrchestratorRule{}, fmt.Errorf("update rule: %w", err)
	}
	return out, nil
}

func (s *CatalogStore) DeleteRule(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM orchestrator_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("rule not found")
	}
	return nil
}

func (s *CatalogStore) ListEnabledRules(ctx context.Context) ([]domain.OrchestratorRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, name, content, priority, enabled, created_at FROM orchestrator_rules
		WHERE enabled = true ORDER BY priority DESC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list enabled rules: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

func (s *CatalogStore) ListEnabledRulesByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.OrchestratorRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, name, content, priority, enabled, created_at FROM orchestrator_rules
		WHERE agent_id = $1 AND enabled = true ORDER BY priority DESC, name ASC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list enabled rules by agent: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

func (s *CatalogStore) CreatePlan(ctx context.Context, plan domain.OrchestrationPlan, tasks []domain.PlanTask) (domain.OrchestrationPlan, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.OrchestrationPlan{}, err
	}
	defer tx.Rollback(ctx)

	var out domain.OrchestrationPlan
	err = tx.QueryRow(ctx, `
		INSERT INTO orchestration_plans (run_id, status, plan_json)
		VALUES ($1, $2, $3)
		RETURNING id, run_id, status, plan_json, created_at
	`, plan.RunID, plan.Status, plan.PlanJSON).Scan(&out.ID, &out.RunID, &out.Status, &out.PlanJSON, &out.CreatedAt)
	if err != nil {
		return domain.OrchestrationPlan{}, fmt.Errorf("create plan: %w", err)
	}
	out.Summary = plan.Summary

	for _, t := range tasks {
		t.PlanID = out.ID
		_, err := tx.Exec(ctx, `
			INSERT INTO plan_tasks (plan_id, task_key, title, description, agent_id, skill_ids, depends_on, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, t.PlanID, t.TaskKey, t.Title, t.Description, t.AgentID, t.SkillIDs, t.DependsOn, t.Status)
		if err != nil {
			return domain.OrchestrationPlan{}, fmt.Errorf("create plan task: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.OrchestrationPlan{}, err
	}
	return out, nil
}

func (s *CatalogStore) GetPlanByRunID(ctx context.Context, runID uuid.UUID) (domain.PlanView, error) {
	var view domain.PlanView
	var planJSON []byte
	var createdAt interface{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, run_id, status, plan_json, created_at FROM orchestration_plans WHERE run_id = $1
	`, runID).Scan(&view.ID, &view.RunID, &view.Status, &planJSON, &createdAt)
	if err != nil {
		return domain.PlanView{}, fmt.Errorf("get plan: %w", err)
	}

	var parsed struct {
		domain.PlannerOutput
		Verification *domain.VerificationResult `json:"verification"`
	}
	_ = json.Unmarshal(planJSON, &parsed)
	view.Summary = parsed.Summary
	view.Purpose = parsed.Purpose
	view.Goal = parsed.Goal
	view.Verification = parsed.Verification

	toolNamesByKey := make(map[string][]string, len(parsed.Tasks))
	for _, t := range parsed.Tasks {
		toolNamesByKey[t.ID] = t.ToolNames
	}

	tasks, err := s.ListPlanTasks(ctx, view.ID)
	if err != nil {
		return domain.PlanView{}, err
	}
	for i := range tasks {
		if names, ok := toolNamesByKey[tasks[i].TaskKey]; ok {
			tasks[i].ToolNames = names
		}
	}
	view.Tasks = tasks
	return view, nil
}

func (s *CatalogStore) UpdatePlanJSON(ctx context.Context, planID uuid.UUID, planJSON []byte) error {
	_, err := s.pool.Exec(ctx, `UPDATE orchestration_plans SET plan_json = $2 WHERE id = $1`, planID, planJSON)
	return err
}

func (s *CatalogStore) AppendPlanTasks(ctx context.Context, planID uuid.UUID, tasks []domain.PlanTask) ([]domain.PlanTask, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var stored []domain.PlanTask
	for _, t := range tasks {
		t.PlanID = planID
		var out domain.PlanTask
		err := tx.QueryRow(ctx, `
			INSERT INTO plan_tasks (plan_id, task_key, title, description, agent_id, skill_ids, depends_on, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, plan_id, task_key, title, description, agent_id, skill_ids, depends_on, status, created_at
		`, t.PlanID, t.TaskKey, t.Title, t.Description, t.AgentID, t.SkillIDs, t.DependsOn, t.Status).Scan(
			&out.ID, &out.PlanID, &out.TaskKey, &out.Title, &out.Description, &out.AgentID, &out.SkillIDs, &out.DependsOn, &out.Status, &out.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("append plan task: %w", err)
		}
		out.ToolNames = t.ToolNames
		stored = append(stored, out)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return stored, nil
}

func (s *CatalogStore) UpdatePlanStatus(ctx context.Context, planID uuid.UUID, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE orchestration_plans SET status = $2 WHERE id = $1`, planID, status)
	return err
}

func (s *CatalogStore) UpdateTaskStatus(ctx context.Context, taskID uuid.UUID, status, result, errMsg string) error {
	var resultVal, errVal any
	if result != "" {
		resultVal = result
	}
	if errMsg != "" {
		errVal = errMsg
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE plan_tasks SET status = $2, result = $3, error = $4 WHERE id = $1
	`, taskID, status, resultVal, errVal)
	return err
}

func (s *CatalogStore) ListPlanTasks(ctx context.Context, planID uuid.UUID) ([]domain.PlanTask, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, plan_id, task_key, title, description, agent_id, skill_ids, depends_on, status, result, error, created_at
		FROM plan_tasks WHERE plan_id = $1 ORDER BY created_at ASC
	`, planID)
	if err != nil {
		return nil, fmt.Errorf("list plan tasks: %w", err)
	}
	defer rows.Close()
	var tasks []domain.PlanTask
	for rows.Next() {
		var t domain.PlanTask
		var result, errMsg sql.NullString
		if err := rows.Scan(&t.ID, &t.PlanID, &t.TaskKey, &t.Title, &t.Description, &t.AgentID, &t.SkillIDs, &t.DependsOn, &t.Status, &result, &errMsg, &t.CreatedAt); err != nil {
			return nil, err
		}
		if result.Valid {
			t.Result = result.String
		}
		if errMsg.Valid {
			t.Error = errMsg.String
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func scanSkills(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]domain.Skill, error) {
	var skills []domain.Skill
	for rows.Next() {
		var sk domain.Skill
		var embJSON []byte
		if err := rows.Scan(&sk.ID, &sk.AgentID, &sk.Name, &sk.Description, &sk.Category, &sk.Tags, &sk.Content, &embJSON, &sk.Enabled, &sk.TechStackID, &sk.CatalogSha, &sk.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(embJSON, &sk.Embedding)
		skills = append(skills, sk)
	}
	return skills, rows.Err()
}

func scanRules(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]domain.OrchestratorRule, error) {
	var rules []domain.OrchestratorRule
	for rows.Next() {
		var r domain.OrchestratorRule
		if err := rows.Scan(&r.ID, &r.AgentID, &r.Name, &r.Content, &r.Priority, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}
