package domain

import (
	"errors"

	"github.com/google/uuid"
)

// ReadinessStepID names one line of a repository's "get this project ready"
// checklist. The wording a person reads is the UI's per locale; the wording an
// agent reads is catalog/system/prompts/readiness/<step>.md.
type ReadinessStepID string

const (
	ReadinessScan          ReadinessStepID = "scan"
	ReadinessToolchain     ReadinessStepID = "toolchain"
	ReadinessIndex         ReadinessStepID = "index"
	ReadinessGuide         ReadinessStepID = "guide"
	ReadinessTests         ReadinessStepID = "tests"
	ReadinessCI            ReadinessStepID = "ci"
	ReadinessDelivery      ReadinessStepID = "delivery"
	ReadinessObservability ReadinessStepID = "observability"
	ReadinessFindings      ReadinessStepID = "findings"
	ReadinessDesign        ReadinessStepID = "design"
	ReadinessStore         ReadinessStepID = "store"
)

// ReadinessCardKey is the reserved step_id under which a repository's own
// "hide this card" choice is stored.
const ReadinessCardKey = "_card"

// ReadinessStepOrder is the order the steps are listed and "run all" works
// through them: what everything else reads from comes first.
func ReadinessStepOrder() []ReadinessStepID {
	return []ReadinessStepID{
		ReadinessScan, ReadinessToolchain, ReadinessIndex, ReadinessGuide, ReadinessTests, ReadinessCI,
		ReadinessDelivery, ReadinessObservability, ReadinessFindings, ReadinessDesign, ReadinessStore,
	}
}

func ValidReadinessStepID(id string) bool {
	for _, s := range ReadinessStepOrder() {
		if string(s) == id {
			return true
		}
	}
	return false
}

type ReadinessStatus string

const (
	ReadinessDone ReadinessStatus = "done"
	ReadinessTodo ReadinessStatus = "todo"
	// ReadinessRunning: a scan or an index pass is under way, or the board
	// task this step opened is still open.
	ReadinessRunning ReadinessStatus = "running"
	// ReadinessBlocked: a step this one reads from is not done yet.
	ReadinessBlocked ReadinessStatus = "blocked"
	ReadinessSkipped ReadinessStatus = "skipped"
	// ReadinessUnknown: the server cannot tell; the app resolves it (the
	// developer tools on this computer) or the person decides.
	ReadinessUnknown ReadinessStatus = "unknown"
)

// ReadinessAction is what "do it" means for a step.
type ReadinessAction string

const (
	// ReadinessActionRun starts something the server does itself (a scan, an
	// index pass).
	ReadinessActionRun ReadinessAction = "run"
	// ReadinessActionTask opens a board task for the right agent.
	ReadinessActionTask ReadinessAction = "task"
	// ReadinessActionNavigate needs a decision or a credential only the person
	// has: the app opens the place that asks for it.
	ReadinessActionNavigate ReadinessAction = "navigate"
	// ReadinessActionDesktop is the desktop shell's: the developer tools on
	// this computer.
	ReadinessActionDesktop ReadinessAction = "desktop"
)

type ReadinessTask struct {
	ID     uuid.UUID  `json:"id"`
	Key    string     `json:"key"`
	Title  string     `json:"title"`
	Column TaskColumn `json:"column"`
}

type ReadinessStep struct {
	ID        ReadinessStepID `json:"id"`
	Status    ReadinessStatus `json:"status"`
	Action    ReadinessAction `json:"action"`
	DependsOn []string        `json:"depends_on"`
	// Reason is a short code the UI words ("no_readme", "gaps"); Facts carries
	// the numbers and names it fills in.
	Reason string         `json:"reason,omitempty"`
	Facts  map[string]any `json:"facts,omitempty"`
	// Task is the board task this step opened and that is still open.
	Task *ReadinessTask `json:"task,omitempty"`
}

// ReadinessRunAll is what "run all in order" would do right now, so the
// person sees the cost before clicking.
type ReadinessRunAll struct {
	Tasks int `json:"tasks"`
	Runs  int `json:"runs"`
	// Steps are the step ids it would start, in order.
	Steps []ReadinessStepID `json:"steps"`
}

type RepositoryReadiness struct {
	RepositoryID uuid.UUID       `json:"repository_id"`
	Steps        []ReadinessStep `json:"steps"`
	Done         int             `json:"done"`
	Skipped      int             `json:"skipped"`
	Total        int             `json:"total"`
	// Complete: every step is done or skipped.
	Complete  bool            `json:"complete"`
	Dismissed bool            `json:"dismissed"`
	RunAll    ReadinessRunAll `json:"run_all"`
}

// ReadinessStepState is a person's choice on one step, plus the board task a
// step opened. Everything else about a step is derived from live data.
type ReadinessStepState struct {
	StepID  string
	Skipped bool
	TaskID  *uuid.UUID
}

type ReadinessRunResult struct {
	Step ReadinessStepID `json:"step"`
	// Task is set when the step opened a board task.
	Task *ReadinessTask `json:"task,omitempty"`
	// Started is true when the step started work without a task (a scan, an
	// index pass).
	Started bool `json:"started"`
}

type ReadinessRunAllResult struct {
	Results []ReadinessRunResult `json:"results"`
}

var (
	ErrReadinessUnknownStep = errors.New("unknown readiness step")
	// ErrReadinessNotRunnable: the step is done, skipped, blocked, or is one
	// the app does itself.
	ErrReadinessNotRunnable = errors.New("this step cannot be started from here")
	// ErrReadinessNotApproved: starting a task spends an agent's time, so the
	// request has to say the person agreed to it.
	ErrReadinessNotApproved = errors.New("starting this step needs the person's approval")
	// ErrReadinessPlanChanged: the person approved a different number of tasks
	// than "run all" would now open.
	ErrReadinessPlanChanged = errors.New("the checklist changed since it was shown")
)
