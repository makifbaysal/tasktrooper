package board_test

// CriteriaLoopGuard: the third machine-talking-to-itself shape, closed the
// same way as its two siblings (review_loop_guard_test.go,
// pipeline_bounce_guard's own tests) — a bounded number of unattended repeats
// ends in a park on ResourceHumanDecision, with a comment naming what stopped.
//
// Unlike those two this loop is not detected on an incoming board event: a
// task sitting on three unsettled-criteria failures with no run in flight
// produces no event at all, so it is the reconciler's own periodic sweep that
// has to notice, and these tests drive it through ReconcilerSuite rather than
// DispatcherSuite.

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// unsettledCriteriaSummaryForTest mirrors board.unsettledCriteriaSummary's
// output. It cannot call the unexported function directly (this file is
// package board_test), so the text is reproduced here; criteria_sweep_test.go
// (package board) is what actually pins the production format.
func unsettledCriteriaSummaryForTest(open int) string {
	return "unsettled acceptance criteria: " + strconv.Itoa(open) + " still open after the criteria sweep"
}

// criteriaListStore answers CriteriaReader for the park comment.
type criteriaListStore struct {
	criteria []domain.AcceptanceCriterion
}

func (c *criteriaListStore) ListTaskCriteria(context.Context, uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	return c.criteria, nil
}

// installCriteriaLoopGuard wires the guard onto the suite's reconciler exactly
// as runtime does — journal included, since the park is a move nothing else
// records.
func (s *ReconcilerSuite) installCriteriaLoopGuard(criteria []domain.AcceptanceCriterion) (*loopParker, *loopCommenter, *fakeSpanStore) {
	parker := &loopParker{}
	commenter := &loopCommenter{parker: parker}
	spans := &fakeSpanStore{}
	guard := board.NewCriteriaLoopGuard(s.events, parker, &criteriaListStore{criteria: criteria})
	guard.SetCommenter(commenter)
	guard.SetParkJournal(board.NewParkJournal(s.events, spans))
	s.rec.SetCriteriaLoopGuard(guard)
	return parker, commenter, spans
}

func unsettledCriteriaRun(taskID, assignee uuid.UUID, open int, createdAt time.Time) domain.TaskAgentRun {
	return domain.TaskAgentRun{
		ID: uuid.New(), TaskID: taskID, AgentID: assignee,
		Status:    domain.TaskAgentRunStatusFailed,
		Summary:   unsettledCriteriaSummaryForTest(open),
		CreatedAt: createdAt,
	}
}

// threeUnsettledCriteriaRuns appends three unsettled-criteria Failed runs,
// oldest first, an hour apart, so ListByTask's newest-first order and the
// guard's "oldest of the window" read both land on the run the test intends.
func (s *ReconcilerSuite) threeUnsettledCriteriaRuns(taskID, assignee uuid.UUID) (oldest, newest time.Time) {
	base := time.Now().Add(-3 * time.Hour)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Hour)
		s.runs.runs = append(s.runs.runs, unsettledCriteriaRun(taskID, assignee, 1, at))
		newest = at
	}
	return base, newest
}

// Below the cap, an unsettled-criteria failure is retried through exactly the
// same path as any other Failed run — the guard has nothing to say yet.
func (s *ReconcilerSuite) TestUnsettledCriteriaFailureBelowTheCapIsRetriedLikeAnyOtherFailure() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	s.installCriteriaLoopGuard(nil)
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    repositoryID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.runs = []domain.TaskAgentRun{unsettledCriteriaRun(taskID, assignee, 2, time.Now())}

	s.rec.Run(context.Background())

	s.Require().Len(s.runner.jobs, 1, "a run failed for unsettled criteria is retried the same as a crash, below the cap")
	s.Equal(assignee, s.runner.jobs[0].Run.AgentID)
}

// Three in a row, all unsettled-criteria, nobody having looked: the card is
// parked instead of retried a fourth time, and the comment names the open
// criteria.
func (s *ReconcilerSuite) TestThreeConsecutiveUnsettledCriteriaFailuresParkTheTask() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	openCriteria := []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "the export includes archived rows"}}
	parker, commenter, spans := s.installCriteriaLoopGuard(openCriteria)
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    repositoryID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.threeUnsettledCriteriaRuns(taskID, assignee)

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "three unattended unsettled-criteria failures must not be retried a fourth time")
	s.Require().Len(parker.resources, 1)
	s.Equal(domain.ResourceHumanDecision, parker.resources[0])
	s.Contains(parker.details[0], "criteria loop")
	s.Require().Len(commenter.contents, 1)
	s.Contains(commenter.contents[0], "3 run in a row")
	s.Contains(commenter.contents[0], "the export includes archived rows")

	s.Require().Len(spans.moves, 1, "the park closes the open column span")
	s.Require().NotEmpty(s.events.events)
	var park map[string]interface{}
	s.Require().NoError(json.Unmarshal(s.events.events[len(s.events.events)-1].Payload, &park))
	s.Equal(string(domain.TaskColumnBlocked), park["to_column"])
	s.Equal(domain.MoveReasonCriteriaLoopParked, park[domain.EventPayloadReason])
}

// A crash-caused streak must not be swept up by this guard: three ordinary
// Failed runs (no unsettled-criteria marker) keep the reconciler's old,
// silent give-up behaviour.
func (s *ReconcilerSuite) TestCrashFailuresAreNotParkedByTheCriteriaLoopGuard() {
	assignee := uuid.New()
	taskID := uuid.New()
	parker, commenter, _ := s.installCriteriaLoopGuard(nil)
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	base := time.Now().Add(-3 * time.Hour)
	for i := 0; i < 3; i++ {
		s.runs.runs = append(s.runs.runs, domain.TaskAgentRun{
			ID: uuid.New(), TaskID: taskID, AgentID: assignee,
			Status: domain.TaskAgentRunStatusFailed, Summary: "panic: nil pointer",
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		})
	}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "a crash streak still stops at the cap, exactly as before")
	s.Empty(parker.resources, "a crash is not this guard's loop")
	s.Empty(commenter.contents)
}

// A human touch since the streak started must stop the guard from parking —
// see the guard-wide reset rule ReviewLoopGuard also follows. Exercised
// directly against Hold rather than through a full reconciler sweep: in
// production the human's own comment is what re-dispatches the task (through
// the ordinary Dispatch path, not the reconciler), so there is no realistic
// "3 old failures plus a bare comment, nothing else happens" state to drive
// dispatchNeverStarted through — what matters here is that Hold itself does
// not park once it sees the touch, leaving that dispatch free to happen.
func (s *ReconcilerSuite) TestHumanTouchResetsTheCriteriaLoopStreak() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	parker, commenter, _ := s.installCriteriaLoopGuard(nil)
	task := domain.BoardTask{ID: taskID, RepositoryID: repositoryID, Column: domain.TaskColumnInProgress, AssigneeAgentID: &assignee}
	s.tasks.tasks = []domain.BoardTask{task}
	base := time.Now().Add(-3 * time.Hour)
	runs := []domain.TaskAgentRun{
		unsettledCriteriaRun(taskID, assignee, 1, base.Add(2*time.Hour)),
		unsettledCriteriaRun(taskID, assignee, 1, base.Add(time.Hour)),
		unsettledCriteriaRun(taskID, assignee, 1, base),
	}

	raw, err := json.Marshal(map[string]interface{}{"author_type": "user"})
	s.Require().NoError(err)
	humanEvent, err := s.events.Create(context.Background(), domain.BoardEvent{
		TaskID: taskID, EventType: domain.BoardEventTaskCommented, Payload: raw,
	})
	s.Require().NoError(err)
	// The fake event store does not stamp CreatedAt on its own; set it
	// explicitly, between the oldest and the newest of the three failing
	// runs, to prove a touch DURING the streak resets it.
	for i := range s.events.events {
		if s.events.events[i].ID == humanEvent.ID {
			s.events.events[i].CreatedAt = base.Add(90 * time.Minute)
		}
	}

	guard := board.NewCriteriaLoopGuard(s.events, parker, &criteriaListStore{})
	guard.SetCommenter(commenter)
	held := guard.Hold(context.Background(), repositoryID, task, runs)

	s.False(held, "a human touch during the streak must stop the park")
	s.Empty(parker.resources)
	s.Empty(commenter.contents)
}
