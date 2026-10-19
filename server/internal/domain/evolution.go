package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	ReflectionTriggerPeriodic = "periodic"
	ReflectionTriggerRevision = "revision"
	ReflectionTriggerManual   = "manual"

	ReflectionStatusRunning   = "running"
	ReflectionStatusCompleted = "completed"
	ReflectionStatusFailed    = "failed"

	EvolutionImpactPending          = "pending"
	EvolutionImpactEffective        = "effective"
	EvolutionImpactRegressed        = "regressed"
	EvolutionImpactNeutral          = "neutral"
	EvolutionImpactInsufficientData = "insufficient_data"

	EvolutionChangeSkillCreated  = "skill_created"
	EvolutionChangeSkillUpdated  = "skill_updated"
	EvolutionChangeSkillDeleted  = "skill_deleted"
	EvolutionChangeRuleCreated   = "rule_created"
	EvolutionChangeRuleUpdated   = "rule_updated"
	EvolutionChangeRuleDeleted   = "rule_deleted"
	EvolutionChangeMemoryCreated = "memory_created"
	EvolutionChangeMemoryDeleted = "memory_deleted"
	EvolutionChangeRevert        = "revert"

	EvolutionTargetSkill  = "skill"
	EvolutionTargetRule   = "rule"
	EvolutionTargetMemory = "memory"

	ReflectionOutcomeApplied    = "applied"
	ReflectionOutcomeRejected   = "rejected"    // budget full etc.
	ReflectionOutcomeSkipped    = "skipped"     // self-evolution off, unknown id, run-log memory, cap reached
	ReflectionOutcomeFailed     = "failed"      // store error
	ReflectionOutcomeRolledBack = "rolled_back" // golden gate reverted it (reflections from before changes were probed first)
	ReflectionOutcomeNotApplied = "not_applied" // legacy reflection, parsed on read, never applied
	ReflectionOutcomeNotAdopted = "not_adopted" // probed and not adopted, or nothing could probe it; never written

	// What a skill/rule change is about, so the probe can test the two kinds separately and tell which one moved the result.
	ReflectionDimensionProcedure = "procedure" // how the agent works: tool use, order of steps, checks, finishing
	ReflectionDimensionDomain    = "domain"    // what a practitioner knows: requirements, conventions, the deliverable's shape

	// Per-change verdict from the probe; confounded when the change was only ever tested together with others.
	ProbeVerdictHelped     = "helped"
	ProbeVerdictHurt       = "hurt"
	ProbeVerdictNoEffect   = "no_effect"
	ProbeVerdictConfounded = "confounded"

	ProbeVariantAll = "all"

	ProbeCaseGolden = "golden"
	ProbeCaseTask   = "task"
	ProbeCaseChat   = "chat"
)

// PerformanceSnapshot is stored on a completed reflection and serves as the
// baseline for the next one (incremental analysis, no re-review).
type PerformanceSnapshot struct {
	Score          float64            `json:"score"`
	KPIComposite   float64            `json:"kpi_composite"`
	KPIs           map[string]float64 `json:"kpis,omitempty"` // metric_key -> attainment
	GoldenPassRate *float64           `json:"golden_pass_rate,omitempty"`
	// GoldenPassRateBefore is the same suite run against the pre-change
	// skills/rules — the pair is what the gate judged the changes on.
	GoldenPassRateBefore *float64  `json:"golden_pass_rate_before,omitempty"`
	CapturedAt           time.Time `json:"captured_at"`
}

type AgentReflection struct {
	ID                  uuid.UUID            `json:"id"`
	AgentID             uuid.UUID            `json:"agent_id"`
	Trigger             string               `json:"trigger"`
	Status              string               `json:"status"`
	WindowStart         time.Time            `json:"window_start"`
	WindowEnd           time.Time            `json:"window_end"`
	Summary             string               `json:"summary"`
	PerformanceSnapshot *PerformanceSnapshot `json:"performance_snapshot,omitempty"`
	RawOutput           string               `json:"raw_output,omitempty"`
	Error               string               `json:"error,omitempty"`
	Decision            *ReflectionDecision  `json:"decision,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	CompletedAt         *time.Time           `json:"completed_at,omitempty"`
}

type AgentEvolutionEvent struct {
	ID                uuid.UUID       `json:"id"`
	ReflectionID      *uuid.UUID      `json:"reflection_id,omitempty"`
	AgentID           uuid.UUID       `json:"agent_id"`
	ChangeType        string          `json:"change_type"`
	TargetKind        string          `json:"target_kind"`
	TargetID          *uuid.UUID      `json:"target_id,omitempty"`
	TargetName        string          `json:"target_name"`
	Before            json.RawMessage `json:"before,omitempty"`
	After             json.RawMessage `json:"after,omitempty"`
	RevertedEventID   *uuid.UUID      `json:"reverted_event_id,omitempty"`
	ScoreAtChange     float64         `json:"score_at_change"`
	Impact            string          `json:"impact"`
	ImpactEvaluatedAt *time.Time      `json:"impact_evaluated_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
}

// ReflectionOutput is the JSON contract the reflection LLM call must produce.
type ReflectionOutput struct {
	SelfAssessment string                   `json:"self_assessment"`
	Skills         []ReflectionSkillChange  `json:"skills"`
	Rules          []ReflectionRuleChange   `json:"rules"`
	Memories       []ReflectionMemoryChange `json:"memories"`
	Reverts        []ReflectionRevert       `json:"reverts"`
}

type ReflectionSkillChange struct {
	Action      string   `json:"action"` // create | update | delete
	Dimension   string   `json:"dimension,omitempty"`
	SkillID     string   `json:"skill_id,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Content     string   `json:"content"`
	SourceURLs  []string `json:"source_urls,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

type ReflectionRuleChange struct {
	Action    string `json:"action"` // create | update | delete
	Dimension string `json:"dimension,omitempty"`
	RuleID    string `json:"rule_id,omitempty"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority int    `json:"priority"`
	Reason   string `json:"reason,omitempty"`
}

type ReflectionMemoryChange struct {
	Action   string `json:"action"` // create | delete
	MemoryID string `json:"memory_id,omitempty"`
	Content  string `json:"content"`
	Category string `json:"category"`
	Reason   string `json:"reason,omitempty"`
}

type ReflectionRevert struct {
	EvolutionEventID string `json:"evolution_event_id"`
	Reason           string `json:"reason,omitempty"`
}

// ReflectionDecision is the structured record of what a reflection proposed and
// what actually happened to each proposal — the UI's source of truth instead of
// re-deriving it from the free-text Summary. Legacy rows get one synthesized on
// read; Legacy tells the UI that record was never actually applied.
type ReflectionDecision struct {
	Analysis       string                    `json:"analysis,omitempty"`
	SelfAssessment string                    `json:"self_assessment,omitempty"`
	Baseline       *PerformanceSnapshot      `json:"baseline,omitempty"`
	CatalogBefore  ReflectionCatalogCounts   `json:"catalog_before"`
	CatalogAfter   ReflectionCatalogCounts   `json:"catalog_after"`
	Changes        []ReflectionChangeOutcome `json:"changes"`
	Gate           *ReflectionGateResult     `json:"gate,omitempty"`
	Lesson         *ReflectionLesson         `json:"lesson,omitempty"`
	Legacy         bool                      `json:"legacy,omitempty"`
}

// ReflectionLesson is the one transferable thing a probed change set taught, written for later reflections to read before they make their own edits.
type ReflectionLesson struct {
	Cue        string `json:"cue"`
	Lesson     string `json:"lesson"`
	Evidence   string `json:"evidence"`
	Confidence string `json:"confidence"`
}

type ReflectionCatalogCounts struct {
	Skills      int `json:"skills"`
	Rules       int `json:"rules"`
	Memories    int `json:"memories"`
	SkillBudget int `json:"skill_budget"`
	RuleBudget  int `json:"rule_budget"`
}

type ReflectionChangeOutcome struct {
	Kind      string     `json:"kind"`   // skill|rule|memory|revert
	Action    string     `json:"action"` // create|update|delete|revert
	Name      string     `json:"name"`
	TargetID  string     `json:"target_id,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	Dimension string     `json:"dimension,omitempty"`
	Outcome   string     `json:"outcome"`
	Verdict   string     `json:"verdict,omitempty"`
	Detail    string     `json:"detail,omitempty"`
	EventID   *uuid.UUID `json:"event_id,omitempty"`
}

// ReflectionGateResult is what decided the skill/rule changes. Reflections from before probing carry only the golden before/after rates, Keep and RolledBack; a probed one carries the cases it rehearsed and how each variant of the change set did against the current setup, and nothing it did not adopt was ever written.
type ReflectionGateResult struct {
	BeforeRate *float64 `json:"before_rate,omitempty"`
	AfterRate  *float64 `json:"after_rate,omitempty"`
	Keep       bool     `json:"keep"`
	Reason     string   `json:"reason,omitempty"`
	RolledBack int      `json:"rolled_back"`

	Probed     bool                     `json:"probed,omitempty"`
	Unmeasured bool                     `json:"unmeasured,omitempty"`
	Adopted    string                   `json:"adopted,omitempty"`
	NotAdopted int                      `json:"not_adopted,omitempty"`
	Cases      []ReflectionProbeCase    `json:"cases,omitempty"`
	Variants   []ReflectionProbeVariant `json:"variants,omitempty"`
}

type ReflectionProbeCase struct {
	Kind  string `json:"kind"` // golden|task|chat
	Title string `json:"title"`
	// Measured is false when the current setup could not answer it, so no variant was compared on it.
	Measured bool `json:"measured"`
}

// ReflectionProbeVariant is one subset of the change set rehearsed against the current setup: one dimension's changes, or all of them.
type ReflectionProbeVariant struct {
	Label        string `json:"label"` // procedure|domain|all
	Changes      int    `json:"changes"`
	Wins         int    `json:"wins"`
	Losses       int    `json:"losses"`
	Ties         int    `json:"ties"`
	GoldenLosses int    `json:"golden_losses"`
	Accepted     bool   `json:"accepted"`
}

// MemoryPromotion is one team memory that was turned into a skill: the skill
// now exists in every listed agent's catalog and the memory itself is gone.
type MemoryPromotion struct {
	MemoryID  uuid.UUID `json:"memory_id"`
	SkillName string    `json:"skill_name"`
	Agents    []string  `json:"agents"`
}

// MemoryPromotionResult is what a shared-memory promotion sweep did.
type MemoryPromotionResult struct {
	Scanned  int               `json:"scanned"`
	Promoted []MemoryPromotion `json:"promoted"`
}

// MemoryPromotionCandidate is one proposed memory→skill move: everything the
// operator needs to judge it before anything is written, and everything the
// apply step needs to carry it out. The plan and the apply speak the same shape
// — what the operator approved is exactly what is written, rather than a second
// LLM call deciding differently.
type MemoryPromotionCandidate struct {
	MemoryID uuid.UUID `json:"memory_id"`
	// MemoryContent is the memory as it stands today, so the dialog can show
	// what is about to be turned into a skill and deleted.
	MemoryContent string   `json:"memory_content"`
	SkillName     string   `json:"skill_name"`
	Description   string   `json:"description"`
	Category      string   `json:"category"`
	Content       string   `json:"content"`
	Agents        []string `json:"agents"`
}

// MemoryPromotionPlan is what a promotion WOULD do. Nothing is written to
// produce it: no skill is created and no memory is deleted until the operator
// sends the candidates back to the apply endpoint.
type MemoryPromotionPlan struct {
	Scanned    int                        `json:"scanned"`
	Candidates []MemoryPromotionCandidate `json:"candidates"`
	// Agents is the roster the skills would land on, so the dialog can say who
	// is affected even when a candidate targets everyone.
	Agents []string `json:"agents"`
}
