package domain

import (
	"sort"

	"github.com/google/uuid"
)

type AssigneeMode string

const (
	AssigneeModeNone     AssigneeMode = "none"
	AssigneeModeDefault  AssigneeMode = "default"
	AssigneeModeOverride AssigneeMode = "override"
)

func ValidAssigneeMode(m AssigneeMode) bool {
	switch m {
	case AssigneeModeNone, AssigneeModeDefault, AssigneeModeOverride:
		return true
	default:
		return false
	}
}

type StageKind string

const (
	StageKindIntake   StageKind = "intake"
	StageKindQueue    StageKind = "queue"
	StageKindWork     StageKind = "work"
	StageKindReview   StageKind = "review"
	StageKindApproval StageKind = "approval"
	StageKindRework   StageKind = "rework"
	StageKindParked   StageKind = "parked"
	StageKindTerminal StageKind = "terminal"
)

func ValidStageKind(k StageKind) bool {
	switch k {
	case StageKindIntake, StageKindQueue, StageKindWork, StageKindReview,
		StageKindApproval, StageKindRework, StageKindParked, StageKindTerminal:
		return true
	default:
		return false
	}
}

type ParticipantMode string

const (
	ParticipantModeWorker   ParticipantMode = "worker"
	ParticipantModeApprover ParticipantMode = "approver"
)

func ValidParticipantMode(m ParticipantMode) bool {
	switch m {
	case ParticipantModeWorker, ParticipantModeApprover:
		return true
	default:
		return false
	}
}

type BehaviourRef struct {
	Key    BehaviourKey
	Params map[string]string
}

type TaskTypeDef struct {
	Key            TaskType
	Label          string
	KeyPrefix      string
	Position       int
	IsDefault      bool
	IsDefect       bool
	AssigneeRoleID *uuid.UUID
	AssigneeMode   AssigneeMode
	Behaviours     []BehaviourRef
	BuiltIn        bool
	TaskCount      int
}

func (t TaskTypeDef) Has(key BehaviourKey) bool {
	for _, b := range t.Behaviours {
		if b.Key == key {
			return true
		}
	}
	return false
}

type StageParticipant struct {
	RoleID       uuid.UUID
	Mode         ParticipantMode
	Instructions string
	Position     int
}

type WorkflowStage struct {
	ID           uuid.UUID
	TaskType     TaskType
	Column       TaskColumn
	Position     int
	OnPath       bool
	Kind         StageKind
	Behaviours   []BehaviourRef
	Instructions string
	Participants []StageParticipant
}

func (s WorkflowStage) Has(key BehaviourKey) bool {
	for _, b := range s.Behaviours {
		if b.Key == key {
			return true
		}
	}
	return false
}

func (s WorkflowStage) Param(key BehaviourKey, name string) (string, bool) {
	for _, b := range s.Behaviours {
		if b.Key != key {
			continue
		}
		v, ok := b.Params[name]
		return v, ok
	}
	return "", false
}

type Workflow struct {
	Type   TaskTypeDef
	Stages []WorkflowStage
}

// ReservedForRole reports whether the type hands its tasks to one role's
// agent (design to the designer, analiz to the analyst). Such a task with no
// assignee waits for that role rather than going to whoever watches its column.
func (w Workflow) ReservedForRole() bool {
	return w.Type.AssigneeRoleID != nil &&
		(w.Type.AssigneeMode == AssigneeModeOverride || w.Type.AssigneeMode == AssigneeModeDefault)
}

func (w Workflow) Stage(col TaskColumn) (WorkflowStage, bool) {
	for _, s := range w.Stages {
		if s.Column == col {
			return s, true
		}
	}
	return WorkflowStage{}, false
}

func (w Workflow) Has(col TaskColumn, key BehaviourKey) bool {
	stage, ok := w.Stage(col)
	return ok && stage.Has(key)
}

func (w Workflow) Param(col TaskColumn, key BehaviourKey, name string) (string, bool) {
	stage, ok := w.Stage(col)
	if !ok {
		return "", false
	}
	return stage.Param(key, name)
}

func (w Workflow) TypeHas(key BehaviourKey) bool {
	return w.Type.Has(key)
}

// KindOf is StageKind for a column, or "" when the column has no stage.
func (w Workflow) KindOf(col TaskColumn) StageKind {
	stage, ok := w.Stage(col)
	if !ok {
		return ""
	}
	return stage.Kind
}

func (w Workflow) sortedOnPath() []WorkflowStage {
	out := make([]WorkflowStage, 0, len(w.Stages))
	for _, s := range w.Stages {
		if s.OnPath {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

func (w Workflow) NextOnPath(col TaskColumn) (TaskColumn, bool) {
	path := w.sortedOnPath()
	for i, s := range path {
		if s.Column == col && i+1 < len(path) {
			return path[i+1].Column, true
		}
	}
	return "", false
}

func (w Workflow) WorkColumn() (TaskColumn, bool) {
	for _, s := range w.sortedOnPath() {
		if s.Kind == StageKindWork {
			return s.Column, true
		}
	}
	return "", false
}

func (w Workflow) ReviewChain() []ReviewStage {
	path := w.sortedOnPath()
	order := make(map[TaskColumn]int, len(path))
	for i, s := range path {
		order[s.Column] = i
	}
	var chain []ReviewStage
	for _, s := range w.Stages {
		label, hasLabel := s.Param(BehaviourReviewChainStage, "label")
		if !hasLabel {
			continue
		}
		remedy, _ := s.Param(BehaviourReviewChainStage, "remedy")
		chain = append(chain, ReviewStage{Column: s.Column, Label: label, Remedy: remedy})
	}
	sort.SliceStable(chain, func(i, j int) bool {
		oi, oki := order[chain[i].Column]
		oj, okj := order[chain[j].Column]
		if !oki || !okj {
			return false
		}
		return oi < oj
	})
	return chain
}

// ReviewStagesBetween is the review chain stages lying strictly between from
// and to on the happy path, in path order. Empty when either column is off
// the path or to does not come after from.
func (w Workflow) ReviewStagesBetween(from, to TaskColumn) []ReviewStage {
	path := w.sortedOnPath()
	fromIdx, toIdx := -1, -1
	for i, s := range path {
		switch s.Column {
		case from:
			fromIdx = i
		case to:
			toIdx = i
		}
	}
	if fromIdx < 0 || toIdx <= fromIdx+1 {
		return nil
	}
	between := make(map[TaskColumn]bool, toIdx-fromIdx-1)
	for _, s := range path[fromIdx+1 : toIdx] {
		between[s.Column] = true
	}
	var out []ReviewStage
	for _, stage := range w.ReviewChain() {
		if between[stage.Column] {
			out = append(out, stage)
		}
	}
	return out
}

// StageExit is where a task leaves col on the happy path: the stage's own
// review_verdict_sweep pass_to, else the next on-path stage.
func (w Workflow) StageExit(col TaskColumn) (TaskColumn, bool) {
	if to, ok := w.Param(col, BehaviourReviewVerdictSweep, "pass_to"); ok && to != "" {
		return TaskColumn(to), true
	}
	return w.NextOnPath(col)
}

// WithoutStage is the workflow with col switched off: its stage is gone and
// every behaviour param that named it now names its exit, so the previous
// stage hands straight to the next one. A workflow without that stage (or
// whose stage has no exit) comes back with the stage dropped and nothing
// rewritten. ensure_pr_on_enter moves to the exit so the PR still exists
// when the task reaches it. The receiver is never mutated.
func (w Workflow) WithoutStage(col TaskColumn) Workflow {
	stage, found := w.Stage(col)
	if !found {
		return w
	}
	exit, hasExit := w.StageExit(col)
	if hasExit && exit == col {
		hasExit = false
	}
	out := Workflow{Type: w.Type, Stages: make([]WorkflowStage, 0, len(w.Stages)-1)}
	for _, s := range w.Stages {
		if s.Column == col {
			continue
		}
		if hasExit {
			s.Behaviours = rewriteBehaviourColumn(s.Behaviours, string(col), string(exit))
			if s.Column == exit && stage.Has(BehaviourEnsurePROnEnter) && !s.Has(BehaviourEnsurePROnEnter) {
				s.Behaviours = append(append([]BehaviourRef(nil), s.Behaviours...), BehaviourRef{Key: BehaviourEnsurePROnEnter})
			}
		}
		out.Stages = append(out.Stages, s)
	}
	return out
}

func rewriteBehaviourColumn(refs []BehaviourRef, from, to string) []BehaviourRef {
	var out []BehaviourRef
	for i, b := range refs {
		var params map[string]string
		for k, v := range b.Params {
			if v != from {
				continue
			}
			if params == nil {
				params = make(map[string]string, len(b.Params))
				for k2, v2 := range b.Params {
					params[k2] = v2
				}
			}
			params[k] = to
		}
		if params == nil {
			continue
		}
		if out == nil {
			out = append([]BehaviourRef(nil), refs...)
		}
		out[i].Params = params
	}
	if out == nil {
		return refs
	}
	return out
}
