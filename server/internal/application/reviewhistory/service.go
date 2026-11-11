package reviewhistory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TaskReader interface {
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

// Service gathers the records Build reads and writes the few it owns: what a
// reviewer reported and which flags a person has looked at.
type Service struct {
	Tasks     TaskReader
	Reviews   port.TaskReviewStore
	Events    port.BoardEventStore
	Runs      port.TaskAgentRunStore
	Traces    port.RunTraceStore
	Criteria  port.AcceptanceCriterionStore
	TestCases port.TaskTestCaseStore
	Findings  port.ReviewFindingStore
	Rules     *Rules
	// AgentNames maps agent ids to names; HeadSHA is the task branch's
	// current commit; Diff is the pull request's changed files and patch.
	// Any may be nil.
	AgentNames func(ctx context.Context) map[uuid.UUID]string
	HeadSHA    func(ctx context.Context, taskID uuid.UUID) string
	Diff       func(ctx context.Context, taskID uuid.UUID) ([]DiffFile, string)
	Now        func() time.Time
}

const (
	maxEvents        = 2000
	maxRuns          = 200
	maxTraceEvents   = 5000
	traceEventsLimit = 1000
)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) enabled() bool { return s != nil && s.Findings != nil }

// History is the task's review story.
func (s *Service) History(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.ReviewHistory, error) {
	if !s.enabled() {
		return domain.ReviewHistory{TaskID: taskID, Rounds: []domain.ReviewHistoryRound{}, Findings: []domain.ReviewHistoryFinding{}, Flags: []domain.ReviewFlag{}}, nil
	}
	task, err := s.Tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.ReviewHistory{}, err
	}
	in := Inputs{Task: task, Rules: s.Rules, Now: s.now()}
	if in.Findings, err = s.Findings.ListByTask(ctx, taskID); err != nil {
		return domain.ReviewHistory{}, err
	}
	if in.Acks, err = s.Findings.ListAcks(ctx, taskID); err != nil {
		return domain.ReviewHistory{}, err
	}
	if s.Reviews != nil {
		if in.Spans, err = s.Reviews.TaskSpans(ctx, taskID); err != nil {
			return domain.ReviewHistory{}, err
		}
		if in.Verdicts, err = s.Reviews.ListReviewVerdicts(ctx, taskID); err != nil {
			return domain.ReviewHistory{}, err
		}
	}
	if s.Events != nil {
		if in.Events, err = s.Events.ListByTask(ctx, taskID, maxEvents); err != nil {
			return domain.ReviewHistory{}, err
		}
	}
	if s.Runs != nil {
		if in.Runs, err = s.Runs.ListByTask(ctx, taskID, maxRuns); err != nil {
			return domain.ReviewHistory{}, err
		}
	}
	if s.Traces != nil {
		if in.Traces, err = s.Traces.ListByTask(ctx, taskID); err != nil {
			return domain.ReviewHistory{}, err
		}
	}
	if s.Criteria != nil {
		if in.Criteria, err = s.Criteria.ListByTask(ctx, taskID); err != nil {
			return domain.ReviewHistory{}, err
		}
	}
	if s.TestCases != nil {
		if in.TestCases, err = s.TestCases.ListByTask(ctx, taskID); err != nil {
			return domain.ReviewHistory{}, err
		}
	}
	if s.AgentNames != nil {
		names := s.AgentNames(ctx)
		in.AgentName = func(id uuid.UUID) string { return names[id] }
	}
	if s.Diff != nil {
		in.Diff, in.Patch = s.Diff(ctx, taskID)
	}
	if s.Traces != nil {
		in.RunEvents = s.runEvents(ctx)
	}
	return Build(in), nil
}

// runEvents loads a run's recorded calls on demand, a thousand at a time up
// to the trace's own cap; a trace that cannot be read is a run with no
// record, never an error on the history.
func (s *Service) runEvents(ctx context.Context) func(uuid.UUID) []domain.RunTraceEvent {
	return func(runID uuid.UUID) []domain.RunTraceEvent {
		var out []domain.RunTraceEvent
		after := 0
		for len(out) < maxTraceEvents {
			page, err := s.Traces.List(ctx, runID, port.RunTraceFilter{Limit: traceEventsLimit, After: after})
			if err != nil {
				log.Warn().Err(err).Str("run_id", runID.String()).Msg("review history: reading a run's trace failed")
				return out
			}
			out = append(out, page...)
			if len(page) < traceEventsLimit {
				break
			}
			after = page[len(page)-1].Seq
		}
		return out
	}
}

// RecordReport stores what a reviewer reported on a task that is in a review
// gate. The gate is the column the card is in: a report from anywhere else
// would belong to no round.
func (s *Service) RecordReport(ctx context.Context, repositoryID, taskID, agentID uuid.UUID, report domain.ReviewReportInput) (int, error) {
	if !s.enabled() {
		return 0, errors.New("this build cannot record review reports")
	}
	if err := report.Validate(); err != nil {
		return 0, err
	}
	task, err := s.Tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return 0, err
	}
	gate := string(task.Column)
	var name string
	if s.AgentNames != nil {
		name = s.AgentNames(ctx)[agentID]
	}
	head := ""
	if s.HeadSHA != nil {
		head = s.HeadSHA(ctx, taskID)
	}
	id := agentID
	rows := report.Rows(taskID, gate, &id, name, head, s.now())
	if err := s.Findings.Insert(ctx, rows); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// RecordCriterionCheck keeps QA's verdict on a criterion as it was given:
// task_criterion_checks holds only the latest one, so the failures of an
// earlier round would otherwise be gone by the time the criterion passes.
func (s *Service) RecordCriterionCheck(ctx context.Context, task domain.BoardTask, criterion domain.AcceptanceCriterion, check domain.CriterionCheck) {
	if !s.enabled() {
		return
	}
	row := domain.TaskReviewFinding{
		TaskID: task.ID, Kind: domain.ReviewItemFinding, Gate: string(task.Column), AgentID: check.AgentID,
		HeadSHA: check.VerifiedSHA, Fingerprint: domain.CriterionFingerprint(criterion.ID),
		Severity: "high", Category: domain.ReviewCategoryAcceptanceCriterion, Message: strings.TrimSpace(check.Note),
		Status: domain.ReviewFindingPassed, CriterionID: &criterion.ID, CreatedAt: s.now(),
	}
	if !check.Approved {
		row.Status = domain.ReviewFindingOpen
		if row.Message == "" {
			row.Message = criterion.Text
		}
	}
	if s.AgentNames != nil && check.AgentID != nil {
		row.AgentName = s.AgentNames(ctx)[*check.AgentID]
	}
	if err := s.Findings.Insert(ctx, []domain.TaskReviewFinding{row}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("review history: recording the criterion verdict failed")
	}
}

// MarkReviewed records that a person looked at one flag. The flag must be one
// the task has now: a key for something that was never raised is a 404.
func (s *Service) MarkReviewed(ctx context.Context, repositoryID, taskID uuid.UUID, flagKey, by string) (domain.ReviewFlag, error) {
	h, err := s.History(ctx, repositoryID, taskID)
	if err != nil {
		return domain.ReviewFlag{}, err
	}
	for _, f := range h.Flags {
		if f.ID != flagKey {
			continue
		}
		ack, err := s.Findings.Ack(ctx, domain.ReviewFlagAck{TaskID: taskID, FlagKey: flagKey, ReviewedBy: by, ReviewedAt: s.now()})
		if err != nil {
			return domain.ReviewFlag{}, err
		}
		at := ack.ReviewedAt
		f.ReviewedBy, f.ReviewedAt = ack.ReviewedBy, &at
		return f, nil
	}
	return domain.ReviewFlag{}, fmt.Errorf("%w: %s", domain.ErrReviewFlagNotFound, flagKey)
}

// OpenFlagCount is how many flags nobody has marked reviewed yet.
func (s *Service) OpenFlagCount(ctx context.Context, repositoryID, taskID uuid.UUID) int {
	h, err := s.History(ctx, repositoryID, taskID)
	if err != nil {
		return 0
	}
	return h.Summary.OpenFlags
}
