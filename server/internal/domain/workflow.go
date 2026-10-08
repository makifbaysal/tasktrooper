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
