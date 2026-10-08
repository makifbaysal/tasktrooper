package domain

import (
	"time"

	"github.com/google/uuid"
)

// UpstreamAgent is one agent definition as the external catalog stores it, in
// agents/<slug>/.
type UpstreamAgent struct {
	Slug          string
	Name          string
	Description   string
	SubagentType  string
	SystemPrompt  string
	ProviderType  LLMProviderType
	Model         string
	ModelHeavy    string
	Effort        string
	MaxTurns      int
	SelfEvolution bool
	Enabled       bool
	ToolPolicy    ToolPolicy
	Roles         []TemplateRoleSuggestion
	Subscriptions []TaskColumn
	Skills        []UpstreamSkill
	Rules         []UpstreamRule
	// Seeded into agent_column_instructions, never into subscriptions, so a
	// column that dispatches the agent without being watched still gets its
	// instruction.
	ColumnInstructions []UpstreamColumnInstruction
	TechStacks         []CreateTechStackRequest
	KPIs               []CreateKPIRequest
	Etag               string
}

type UpstreamSkill struct {
	Name        string
	Description string
	Category    string
	TechStack   string
	Content     string
	// Mirrors the skill's `enabled:` front-matter; the sync must carry a
	// deferred capability's disabled state onto the live skill.
	Enabled bool
	// The revision marker stored back on the live skill as CatalogSha.
	Sha string
}

type UpstreamRule struct {
	Name     string
	Content  string
	Priority int
	Enabled  bool
}

type UpstreamColumnInstruction struct {
	Column      TaskColumn
	Instruction string
}

// CatalogSyncState is the one-row status of the last external-catalog sync.
type CatalogSyncState struct {
	// The git commit sha, or the local directory used in place.
	RepoRef      string             `json:"repo_ref"`
	LastSyncAt   time.Time          `json:"last_sync_at"`
	LastError    string             `json:"last_error,omitempty"`
	LastSummary  *CatalogSyncResult `json:"last_summary,omitempty"`
	PendingCount int                `json:"pending_count"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type CatalogSyncResult struct {
	RepoRef string `json:"repo_ref"`
	Created int    `json:"created"`
	Updated int    `json:"updated"`
	Merged  int    `json:"merged"`
	Skipped int    `json:"skipped"`
	// Upstream changes parked for the user instead of applied.
	Pending int `json:"pending"`
}

// CatalogSyncProgress is what a catalog sync is doing right now. Embedding a
// new agent's skills takes seconds each, so a sync that brings new agents runs
// for minutes; this is what tells a person it has not stalled.
type CatalogSyncProgress struct {
	Running   bool       `json:"running"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	// Agent is the catalog slug being reconciled; NewAgent marks one this
	// sync is creating rather than updating.
	Agent       string `json:"agent,omitempty"`
	NewAgent    bool   `json:"new_agent"`
	AgentsDone  int    `json:"agents_done"`
	AgentsTotal int    `json:"agents_total"`
	// SkillsDone of SkillsTotal is the current agent's new skills embedded so
	// far — the slow part; a sync with nothing new keeps SkillsTotal at 0.
	SkillsDone  int `json:"skills_done"`
	SkillsTotal int `json:"skills_total"`
	// AgentsAdded names every agent this sync has created so far.
	AgentsAdded []string `json:"agents_added,omitempty"`
}

const (
	CatalogPendingKindAgent    = "agent"
	CatalogPendingKindSkill    = "skill"
	CatalogPendingActionCreate = "create"
	CatalogPendingActionUpdate = "update"
	CatalogPendingActionDelete = "delete"
	CatalogPendingActionMerge  = "merge"
)

type CatalogPending struct {
	ID        uuid.UUID `json:"id"`
	AgentSlug string    `json:"agent_slug"`
	AgentName string    `json:"agent_name"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
