package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	PlanStatusPending   = "pending"
	PlanStatusRunning   = "running"
	PlanStatusCompleted = "completed"
	PlanStatusFailed    = "failed"
	// PlanStatusIncomplete is a plan that ran to the end without its result
	// ever being confirmed: nothing died and the output is handed back, unlike
	// "failed" (the process stopped). Terminal like "completed" — nothing waits
	// on it, and only a run still claiming to run gets closed by the
	// reconciler.
	PlanStatusIncomplete = "incomplete"

	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	// TaskStatusIncomplete is a subtask that returned output without doing
	// what it was for; the run does not stop, the result is kept, and like
	// "failed" a resume re-runs it.
	TaskStatusIncomplete = "incomplete"
)

type Skill struct {
	ID          uuid.UUID `json:"id"`
	AgentID     uuid.UUID `json:"agent_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Tags        []string  `json:"tags"`
	Content     string    `json:"content"`
	Embedding   []float32 `json:"embedding,omitempty"`
	Enabled     bool      `json:"enabled"`
	// TechStackID is the one TechStack of this agent the skill belongs to; nil
	// is the general skill, a different thing from "unset", so it always
	// renders.
	TechStackID *uuid.UUID `json:"tech_stack_id"`
	// CatalogSha is the content hash of the catalog revision this skill was
	// last applied from; empty means it never came from the external catalog.
	CatalogSha string    `json:"catalog_sha,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Agent struct {
	ID           uuid.UUID       `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SubagentType string          `json:"subagent_type"`
	SystemPrompt string          `json:"system_prompt"`
	ProviderType LLMProviderType `json:"provider_type"`
	Model        string          `json:"model"`
	// ModelHeavy is an optional stronger model for subtasks the planner rates
	// as "hard"; empty = always Model.
	ModelHeavy string `json:"model_heavy"`
	// MaxTurns caps one claude_code CLI session; 0 = executor default. Per-agent
	// because a session's cost grows with the SQUARE of its turns — a global
	// ceiling priced for the most open-ended agent was charged to all of them.
	MaxTurns int `json:"max_turns"`
	// Effort is the CLI's --effort level (low…max); empty = CLI default. The
	// right level is a property of the work, which is why it is per-agent.
	Effort               string      `json:"effort"`
	ToolPolicy           ToolPolicy  `json:"tool_policy"`
	SkillIDs             []uuid.UUID `json:"skill_ids"`
	Enabled              bool        `json:"enabled"`
	SelfEvolutionEnabled bool        `json:"self_evolution_enabled"`
	// CatalogSlug names this agent's definition in the external catalog; empty
	// means no sync touches it.
	CatalogSlug string `json:"catalog_slug,omitempty"`
	// CatalogEtag is the hash of the catalog revision applied; the sync
	// rewrites it only when an update was actually applied.
	CatalogEtag string `json:"catalog_etag,omitempty"`
	// AutoPullAgentUpdates lets the user keep their own prompt/roles edits
	// without losing the skills the catalog still supplies.
	AutoPullAgentUpdates bool `json:"auto_pull_agent_updates"`
	// KeepSkillsUpdated gates LLM-merged skill conflict resolution.
	KeepSkillsUpdated bool `json:"keep_skills_updated"`
	// CommitCoauthor replaces the built-in Co-authored-by trailers on this
	// agent's commits ("Name <email>"); empty = the built-in ones.
	CommitCoauthor string `json:"commit_coauthor"`
	// CommitCoauthorDisabled drops every Co-authored-by trailer from this
	// agent's commits.
	CommitCoauthorDisabled bool      `json:"commit_coauthor_disabled"`
	CreatedAt              time.Time `json:"created_at"`
}

type OrchestratorRule struct {
	ID        uuid.UUID `json:"id"`
	AgentID   uuid.UUID `json:"agent_id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	Priority  int       `json:"priority"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type OrchestrationPlan struct {
	ID        uuid.UUID       `json:"id"`
	RunID     uuid.UUID       `json:"run_id"`
	Status    string          `json:"status"`
	Summary   string          `json:"summary"`
	PlanJSON  json.RawMessage `json:"plan_json,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type GoalIntake struct {
	Ready       bool                    `json:"ready"`
	Purpose     string                  `json:"purpose"`
	Goal        string                  `json:"goal"`
	Constraints []string                `json:"constraints"`
	Questions   []ClarificationQuestion `json:"questions"`
}

type VerificationResult struct {
	Passed  bool     `json:"passed"`
	Issues  []string `json:"issues"`
	Summary string   `json:"summary"`
}

type PlanTask struct {
	ID          uuid.UUID   `json:"id"`
	PlanID      uuid.UUID   `json:"plan_id"`
	TaskKey     string      `json:"task_key"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	AgentID     *uuid.UUID  `json:"agent_id,omitempty"`
	SkillIDs    []uuid.UUID `json:"skill_ids"`
	ToolNames   []string    `json:"tool_names"`
	DependsOn   []string    `json:"depends_on"`
	// Difficulty is the planner's easy/hard rating; the executor uses it to
	// pick the assigned agent's Model vs ModelHeavy.
	Difficulty string    `json:"difficulty,omitempty"`
	Status     string    `json:"status"`
	Result     string    `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type PlanView struct {
	ID           uuid.UUID           `json:"id"`
	RunID        uuid.UUID           `json:"run_id"`
	Status       string              `json:"status"`
	Summary      string              `json:"summary"`
	Purpose      string              `json:"purpose,omitempty"`
	Goal         string              `json:"goal,omitempty"`
	Verification *VerificationResult `json:"verification,omitempty"`
	Tasks        []PlanTask          `json:"tasks"`
}

type CreateSkillRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags"`
	Content     string     `json:"content"`
	Enabled     bool       `json:"enabled"`
	TechStackID *uuid.UUID `json:"tech_stack_id"`
}

// UpdateSkillRequest replaces every field, TechStackID included — omitting it
// files the skill as general, the same way omitting Tags clears them.
type UpdateSkillRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags"`
	Content     string     `json:"content"`
	Enabled     bool       `json:"enabled"`
	TechStackID *uuid.UUID `json:"tech_stack_id"`
}

type CreateAgentRequest struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SubagentType string          `json:"subagent_type"`
	SystemPrompt string          `json:"system_prompt"`
	ProviderType LLMProviderType `json:"provider_type"`
	Model        string          `json:"model"`
	ModelHeavy   string          `json:"model_heavy"`
	// MaxTurns caps one claude_code CLI session; 0 = executor default. Per-agent
	// because a session's cost grows with the SQUARE of its turns.
	MaxTurns int `json:"max_turns"`
	// Effort is the CLI's --effort level (low…max); empty = CLI default.
	Effort               string     `json:"effort"`
	ToolPolicy           ToolPolicy `json:"tool_policy"`
	Enabled              bool       `json:"enabled"`
	SelfEvolutionEnabled bool       `json:"self_evolution_enabled"`
	CommitCoauthor       string     `json:"commit_coauthor"`
	// CommitCoauthorDisabled turns every Co-authored-by trailer off.
	CommitCoauthorDisabled bool `json:"commit_coauthor_disabled"`
}

type UpdateAgentRequest struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SubagentType string          `json:"subagent_type"`
	SystemPrompt string          `json:"system_prompt"`
	ProviderType LLMProviderType `json:"provider_type"`
	Model        string          `json:"model"`
	ModelHeavy   string          `json:"model_heavy"`
	// MaxTurns caps one claude_code CLI session; 0 = executor default. Per-agent
	// because a session's cost grows with the SQUARE of its turns.
	MaxTurns int `json:"max_turns"`
	// Effort is the CLI's --effort level (low…max); empty = CLI default.
	Effort               string     `json:"effort"`
	ToolPolicy           ToolPolicy `json:"tool_policy"`
	Enabled              bool       `json:"enabled"`
	SelfEvolutionEnabled bool       `json:"self_evolution_enabled"`
	// Pointers so a PUT from a client that omits them cannot silently switch a
	// synced agent's sync off. CatalogSlug/CatalogEtag are not writable here at
	// all — the catalog sync owns them.
	AutoPullAgentUpdates *bool `json:"auto_pull_agent_updates"`
	KeepSkillsUpdated    *bool `json:"keep_skills_updated"`
	// Pointers: omitted keeps the stored value, so catalog sync and older
	// clients never reset a user's co-author choice.
	CommitCoauthor         *string `json:"commit_coauthor"`
	CommitCoauthorDisabled *bool   `json:"commit_coauthor_disabled"`
}

type CreateOrchestratorRuleRequest struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

type UpdateOrchestratorRuleRequest struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

type PlannerTask struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	AgentID      string   `json:"agent_id"`
	SkillIDs     []string `json:"skill_ids"`
	ToolNames    []string `json:"tool_names"`
	SubtaskRules []string `json:"subtask_rules"`
	DependsOn    []string `json:"depends_on"`
	// Difficulty ("easy"/"hard") drives per-subtask model selection: hard runs
	// on the assigned agent's ModelHeavy.
	Difficulty    string `json:"difficulty"`
	ParallelGroup int    `json:"parallel_group"`
}

type PlannerOutput struct {
	Ready     bool                    `json:"ready"`
	Purpose   string                  `json:"purpose"`
	Goal      string                  `json:"goal"`
	Summary   string                  `json:"summary"`
	Tasks     []PlannerTask           `json:"tasks"`
	Questions []ClarificationQuestion `json:"questions"`
}

const (
	SubtaskHistoryModeIsolated = "isolated"
	SubtaskHistoryModeFull     = "full"
)

// Subtask difficulty ratings emitted by the planner, consumed by the executor
// to pick between an agent's Model and its ModelHeavy.
const (
	TaskDifficultyEasy = "easy"
	TaskDifficultyHard = "hard"
)

type OrchestrationConfig struct {
	Enabled                  bool   `koanf:"enabled"`
	FastPath                 bool   `koanf:"fast_path"`
	MaxParallelTasks         int    `koanf:"max_parallel_tasks"`
	MaxPlanTasks             int    `koanf:"max_plan_tasks"`
	SkillRetrievalTopK       int    `koanf:"skill_retrieval_top_k"`
	SubtaskHistoryMode       string `koanf:"subtask_history_mode"`
	DependencyOutputMaxChars int    `koanf:"dependency_output_max_chars"`
	SynthesisEnabled         bool   `koanf:"synthesis_enabled"`
	VerificationEnabled      bool   `koanf:"verification_enabled"`
	MaxReplanIterations      int    `koanf:"max_replan_iterations"`
}
