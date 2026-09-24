package domain

// BehaviourKey names one unit of engine behaviour a stage (or, for the three
// marked "(type)" below, a task type itself) can carry, replacing a hardcoded
// branch the engine used to read literally.
type BehaviourKey string

const (
	BehaviourDispatchSuspended        BehaviourKey = "dispatch_suspended"
	BehaviourRouteToSubscribers       BehaviourKey = "route_to_subscribers"
	BehaviourMergePROnEnter           BehaviourKey = "merge_pr_on_enter"
	BehaviourWatchDeployOnResume      BehaviourKey = "watch_deploy_on_resume"
	BehaviourBlockOnDependencies      BehaviourKey = "block_on_dependencies"
	BehaviourWaitForCI                BehaviourKey = "wait_for_ci"
	BehaviourEnsurePROnEnter          BehaviourKey = "ensure_pr_on_enter"
	BehaviourDetectMigrationOnEnter   BehaviourKey = "detect_migration_on_enter"
	BehaviourStageDeployOnEnter       BehaviourKey = "stage_deploy_on_enter"
	BehaviourAutoEnter                BehaviourKey = "auto_enter"
	BehaviourAdvanceOnDiff            BehaviourKey = "advance_on_diff"
	BehaviourAdvanceOnDocument        BehaviourKey = "advance_on_document"
	BehaviourBuildVerify              BehaviourKey = "build_verify"
	BehaviourCommitOnFinish           BehaviourKey = "commit_on_finish"
	BehaviourRequirePRForReview       BehaviourKey = "require_pr_for_review"
	BehaviourRequireCriteriaComplete  BehaviourKey = "require_criteria_complete"
	BehaviourCriterionVerdict         BehaviourKey = "criterion_verdict"
	BehaviourForwardExit              BehaviourKey = "forward_exit"
	BehaviourRequireTestCases         BehaviourKey = "require_test_cases"
	BehaviourReviewVerdictSweep       BehaviourKey = "review_verdict_sweep"
	BehaviourRequireExecutionEvidence BehaviourKey = "require_execution_evidence"
	BehaviourRequireProductCheck      BehaviourKey = "require_product_check"
	BehaviourReviewChainStage         BehaviourKey = "review_chain_stage"
	BehaviourStripWriters             BehaviourKey = "strip_writers"
	BehaviourNoCodeReading            BehaviourKey = "no_code_reading"

	// The two (type)-scoped behaviours live on TaskTypeDef.Behaviours.
	BehaviourNoWorkspaceWrites    BehaviourKey = "no_workspace_writes"
	BehaviourRequireRepoGrounding BehaviourKey = "require_repo_grounding"
)

// BehaviourScope says whether a behaviour attaches to a stage or the task type
// itself.
type BehaviourScope string

const (
	BehaviourScopeStage BehaviourScope = "stage"
	BehaviourScopeType  BehaviourScope = "type"
)

// BehaviourGroup is when a stage-scoped behaviour's effect fires, for the
// Settings UI to present as three labeled sections instead of one flat list.
// It is a presentation grouping, not a new firing rule — every behaviour
// already fires at its own fixed point in the engine; this only classifies
// where that point is. A type-scoped behaviour (BehaviourScopeType) has no
// meaningful entry/exit and is always BehaviourGroupOther.
type BehaviourGroup string

const (
	// BehaviourGroupEntry fires on/gates a task's arrival into the stage.
	BehaviourGroupEntry BehaviourGroup = "entry"
	// BehaviourGroupExit is a run-finish hook or a gate checked before the
	// move out to the next stage is honored.
	BehaviourGroupExit BehaviourGroup = "exit"
	// BehaviourGroupOther is a type-wide or run-shape flag not tied to a
	// specific column transition (tool-policy stripping, run metadata).
	BehaviourGroupOther BehaviourGroup = "other"
)

// ParamType is the shape a behaviour param's value must parse as (params are
// plain strings on the wire).
type ParamType string

const (
	// ParamTypeColumn is a board column slug; UpdateColumns must not silently
	// orphan it.
	ParamTypeColumn ParamType = "column"
	ParamTypeString ParamType = "string"
	ParamTypeEnum   ParamType = "enum"
	ParamTypeBool   ParamType = "bool"
)

// ParamSpec describes one named parameter a behaviour reference may (or must)
// carry.
type ParamSpec struct {
	Name     string
	Type     ParamType
	Options  []string
	Required bool
}

// BehaviourSpec is one entry of BehaviourRegistry: everything validation and
// the UI's behaviour picker need without the engine package in scope.
type BehaviourSpec struct {
	Scope BehaviourScope
	Group BehaviourGroup
	// Kinds are the stage kinds this behaviour is offered on and accepted
	// for. Empty means "any kind" — used only by the handful that genuinely
	// apply everywhere. Everything else has exactly one job and one place to
	// do it (merge_pr_on_enter only ever means anything on a terminal stage),
	// so listing them stops a workflow from being authored with a behaviour
	// that could never fire.
	Kinds       []StageKind
	Label       string
	Description string
	Params      []ParamSpec
}

// AppliesToKind reports whether a behaviour may be attached to a stage of
// this kind. A behaviour with no Kinds applies to every kind.
func (s BehaviourSpec) AppliesToKind(kind StageKind) bool {
	if len(s.Kinds) == 0 {
		return true
	}
	for _, k := range s.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// BehaviourRegistry is the single source of truth for behaviour keys, scopes
// and params; workflow.ValidateStages and the behaviours endpoint read it.
var BehaviourRegistry = map[BehaviourKey]BehaviourSpec{
	BehaviourDispatchSuspended: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindTerminal}, Label: "Dispatch suspended",
		Description: "The dispatcher never starts a run for a task sitting in this stage.",
	},
	BehaviourRouteToSubscribers: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindQueue, StageKindReview, StageKindApproval}, Label: "Route to subscribers",
		Description: "A task entering this stage wakes every agent subscribed to the column instead of only its assignee.",
	},
	BehaviourMergePROnEnter: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindTerminal}, Label: "Merge PR on enter",
		Description: "Entering this stage wakes the merge flow for the task's pull request.",
	},
	BehaviourWatchDeployOnResume: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindTerminal}, Label: "Watch deploy on resume",
		Description: "A task resuming into this stage is watched for its production deploy.",
	},
	BehaviourBlockOnDependencies: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindQueue, StageKindWork}, Label: "Block on dependencies",
		Description: "The task parks here while an unfinished blocker (`blocks`) exists.",
		Params:      []ParamSpec{{Name: "refuse_move", Type: ParamTypeBool}},
	},
	BehaviourWaitForCI: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindQueue, StageKindReview}, Label: "Wait for CI",
		Description: "Dispatch into this stage waits for the pipeline gate to open.",
	},
	BehaviourEnsurePROnEnter: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindReview, StageKindTerminal}, Label: "Ensure PR on enter",
		Description: "Entering this stage opens the task's pull request if it does not exist yet.",
	},
	BehaviourDetectMigrationOnEnter: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindQueue, StageKindReview}, Label: "Detect migration on enter",
		Description: "Entering this stage checks the branch diff for a schema migration.",
	},
	BehaviourStageDeployOnEnter: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindQueue, StageKindReview}, Label: "Stage deploy on enter",
		Description: "Entering this stage triggers a stage deploy, per the repository's test strategy.",
		Params:      []ParamSpec{{Name: "when", Type: ParamTypeEnum, Options: []string{"qa", "per_step"}, Required: true}},
	},
	BehaviourAutoEnter: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindQueue, StageKindRework}, Label: "Auto-enter",
		Description: "The dispatcher moves the task straight into the named column on assignment/wake.",
		Params: []ParamSpec{
			{Name: "to", Type: ParamTypeColumn, Required: true},
			{Name: "assignee_only", Type: ParamTypeBool},
		},
	},
	BehaviourAdvanceOnDiff: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindWork, StageKindRework}, Label: "Advance on diff",
		Description: "A run that ends with a green build and a real diff is moved to the named column automatically.",
		Params:      []ParamSpec{{Name: "to", Type: ParamTypeColumn, Required: true}},
	},
	BehaviourAdvanceOnDocument: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindWork, StageKindRework}, Label: "Advance on document",
		Description: "A run that ends with a document attached is moved to the named column automatically.",
		Params:      []ParamSpec{{Name: "to", Type: ParamTypeColumn, Required: true}},
	},
	BehaviourBuildVerify: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Label: "Build verify",
		Description: "A run in this stage runs the build-gate fix round before finishing.",
	},
	BehaviourCommitOnFinish: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Label: "Commit on finish",
		Description: "A run in this stage commits its diff on the way out.",
	},
	BehaviourRequirePRForReview: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindReview}, Label: "Require PR for review",
		Description: "A run here refuses without an open pull request to review.",
	},
	BehaviourRequireCriteriaComplete: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupEntry, Kinds: []StageKind{StageKindQueue, StageKindReview, StageKindTerminal}, Label: "Require criteria complete",
		Description: "Entering this stage is refused while an acceptance criterion is still open.",
	},
	BehaviourCriterionVerdict: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupOther, Kinds: []StageKind{StageKindQueue, StageKindReview}, Label: "Criterion verdict",
		Description: "A verdict recorded in this stage is attributed to the named review channel.",
		Params:      []ParamSpec{{Name: "channel", Type: ParamTypeEnum, Options: []string{"qa", "pm"}, Required: true}},
	},
	BehaviourForwardExit: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindReview, StageKindApproval, StageKindTerminal}, Label: "Forward exit",
		Description: "A move out of this stage counts as a forward review exit for scoring.",
	},
	BehaviourRequireTestCases: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindQueue, StageKindReview}, Label: "Require test cases",
		Description: "This stage requires the task's generated test cases to be scored.",
	},
	BehaviourReviewVerdictSweep: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindQueue, StageKindReview, StageKindApproval}, Label: "Review verdict sweep",
		Description: "A finished review round in this stage is swept to a verdict and passed on.",
		Params:      []ParamSpec{{Name: "pass_to", Type: ParamTypeColumn, Required: true}},
	},
	BehaviourRequireExecutionEvidence: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindQueue, StageKindReview}, Label: "Require execution evidence",
		Description: "A QA round in this stage is rejected as ungrounded without evidence the product was actually run.",
	},
	BehaviourRequireProductCheck: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindReview, StageKindApproval}, Label: "Require product check",
		Description: "A UAT round in this stage is rejected without evidence the product was checked.",
	},
	BehaviourReviewChainStage: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupExit, Kinds: []StageKind{StageKindReview, StageKindApproval}, Label: "Review chain stage",
		Description: "This stage is a mandatory step of the type's review chain, required before done/released.",
		Params: []ParamSpec{
			{Name: "label", Type: ParamTypeString, Required: true},
			{Name: "remedy", Type: ParamTypeString, Required: true},
		},
	},
	BehaviourStripWriters: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupOther, Kinds: []StageKind{StageKindQueue, StageKindReview, StageKindApproval, StageKindTerminal}, Label: "Strip writers",
		Description: "Write/verdict tools are stripped from the policy in this stage, except any named in allow.",
		Params:      []ParamSpec{{Name: "allow", Type: ParamTypeString}},
	},
	BehaviourNoCodeReading: {
		Scope: BehaviourScopeStage, Group: BehaviourGroupOther, Kinds: []StageKind{StageKindReview, StageKindApproval}, Label: "No code reading",
		Description: "Code-reading tools are stripped from the policy in this stage.",
	},
	BehaviourNoWorkspaceWrites: {
		Scope: BehaviourScopeType, Group: BehaviourGroupOther, Label: "No workspace writes",
		Description: "Runs on this task type never get workspace write tools.",
	},
	BehaviourRequireRepoGrounding: {
		Scope: BehaviourScopeType, Group: BehaviourGroupOther, Label: "Require repo grounding",
		Description: "A run on this task type is rejected as ungrounded without evidence the repository was actually read.",
	},
}
