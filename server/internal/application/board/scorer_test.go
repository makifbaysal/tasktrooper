package board_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type recordedDelta struct {
	taskID    uuid.UUID
	agentID   uuid.UUID
	eventType string
	delta     float64
}

type fakePerfStore struct{ applied []recordedDelta }

func (f *fakePerfStore) ApplyDelta(_ context.Context, in domain.ApplyScoreInput) (domain.AgentPerformanceScore, error) {
	var taskID uuid.UUID
	if in.TaskID != nil {
		taskID = *in.TaskID
	}
	f.applied = append(f.applied, recordedDelta{taskID, in.AgentID, in.EventType, in.Delta})
	return domain.AgentPerformanceScore{}, nil
}

type fakeOwners struct{ owners map[string]uuid.UUID }

func (f *fakeOwners) OwnersForTask(context.Context, uuid.UUID) (map[string]uuid.UUID, error) {
	return f.owners, nil
}

type fakeTestCases struct {
	items  []domain.TaskTestCase
	marked []uuid.UUID
}

func (f *fakeTestCases) ListByTask(context.Context, uuid.UUID) ([]domain.TaskTestCase, error) {
	return f.items, nil
}

func (f *fakeTestCases) MarkScored(_ context.Context, ids []uuid.UUID, _ time.Time) error {
	f.marked = append(f.marked, ids...)
	for i := range f.items {
		for _, id := range ids {
			if f.items[i].ID == id {
				now := time.Now()
				f.items[i].ScoredAt = &now
			}
		}
	}
	return nil
}

type fakeEvents struct{ perf *fakePerfStore }

func (f *fakeEvents) HasEventForTask(_ context.Context, taskID uuid.UUID, eventType string) (bool, error) {
	for _, a := range f.perf.applied {
		if a.taskID == taskID && a.eventType == eventType {
			return true, nil
		}
	}
	return false, nil
}

type ScorerSuite struct {
	suite.Suite
	dev, qa, pm, architect uuid.UUID
}

func TestScorerSuite(t *testing.T) { suite.Run(t, new(ScorerSuite)) }

func (s *ScorerSuite) SetupTest() {
	s.dev, s.qa, s.pm, s.architect = uuid.New(), uuid.New(), uuid.New(), uuid.New()
}

func (s *ScorerSuite) owners() *fakeOwners {
	return &fakeOwners{owners: map[string]uuid.UUID{
		"in_progress": s.dev,
		"in_qa":       s.qa,
		"pm_uat":      s.pm,
		"code_review": s.architect,
	}}
}

func (s *ScorerSuite) TestHumanUATFailurePenalisesDevQAAndPM() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	assignee := uuid.New() // deliberately nobody's span owner

	tracker.OnColumnTransition(context.Background(),
		domain.BoardTask{ID: uuid.New(), AssigneeAgentID: &assignee},
		domain.TaskColumnHumanUAT, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 3)
	got := map[uuid.UUID]float64{}
	for _, a := range perf.applied {
		s.Equal(domain.ScoreEventHumanUATFailed, a.eventType)
		got[a.agentID] = a.delta
	}
	s.Equal(domain.ScoreDeltaHumanUATFailed, got[s.dev])
	s.Equal(domain.ScoreDeltaHumanUATFailed, got[s.qa])
	s.Equal(domain.ScoreDeltaHumanUATFailed, got[s.pm])
	s.NotContains(got, assignee)
}

func (s *ScorerSuite) TestPMUATFailurePenalisesDevAndQAOnly() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnPMUAT, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 2)
	for _, a := range perf.applied {
		s.Equal(domain.ScoreEventPMUATFailed, a.eventType)
		s.Equal(domain.ScoreDeltaPMUATFailed, a.delta)
		s.Contains([]uuid.UUID{s.dev, s.qa}, a.agentID)
	}
}

func (s *ScorerSuite) TestArchitectRejectionPenalisesDevOnly() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 1)
	s.Equal(s.dev, perf.applied[0].agentID)
	s.Equal(domain.ScoreEventRevisionRequested, perf.applied[0].eventType)
}

func (s *ScorerSuite) TestOneAgentHoldingTwoStagesIsChargedOnce() {
	perf := &fakePerfStore{}
	solo := uuid.New()
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(&fakeOwners{owners: map[string]uuid.UUID{
		"in_progress": solo,
		"in_qa":       solo,
		"pm_uat":      solo,
	}})

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnHumanUAT, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 1)
	s.Equal(solo, perf.applied[0].agentID)
}

func (s *ScorerSuite) TestCompletionCreditsTheAssignee() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	assignee := uuid.New()

	tracker.OnColumnTransition(context.Background(),
		domain.BoardTask{ID: uuid.New(), AssigneeAgentID: &assignee},
		domain.TaskColumnPMUAT, domain.TaskColumnDone)

	s.Require().Len(perf.applied, 1)
	s.Equal(assignee, perf.applied[0].agentID)
	s.Equal(domain.ScoreDeltaTaskCompleted, perf.applied[0].delta)
}

func (s *ScorerSuite) TestReviewEscapeChargesTheApprovingReviewer() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())

	tracker.ApplyReviewEscape(context.Background(), domain.BoardTask{ID: uuid.New()}, s.architect)

	s.Require().Len(perf.applied, 1)
	s.Equal(s.architect, perf.applied[0].agentID)
	s.Equal(domain.ScoreEventReviewEscape, perf.applied[0].eventType)
	s.Equal(domain.ScoreDeltaReviewEscape, perf.applied[0].delta)
}

func (s *ScorerSuite) TestWithoutSpansFallsBackToAssignee() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	assignee := uuid.New()

	tracker.OnColumnTransition(context.Background(),
		domain.BoardTask{ID: uuid.New(), AssigneeAgentID: &assignee},
		domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 1)
	s.Equal(assignee, perf.applied[0].agentID)
}

func (s *ScorerSuite) TestQABugFoundOnRevisionCreditsQA() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	tc := &fakeTestCases{items: []domain.TaskTestCase{
		{ID: uuid.New(), Status: domain.TestCaseStatusFailed},
		{ID: uuid.New(), Status: domain.TestCaseStatusFailed},
		{ID: uuid.New(), Status: domain.TestCaseStatusPassed},
	}}
	tracker.SetTestCases(tc)

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnInQA, domain.TaskColumnNeedRevision)

	var devDelta, qaDelta *recordedDelta
	for i, a := range perf.applied {
		switch a.agentID {
		case s.dev:
			devDelta = &perf.applied[i]
		case s.qa:
			qaDelta = &perf.applied[i]
		}
	}
	s.Require().NotNil(devDelta)
	s.Equal(domain.ScoreEventRevisionRequested, devDelta.eventType)
	s.Require().NotNil(qaDelta)
	s.Equal(domain.ScoreEventQABugFound, qaDelta.eventType)
	s.Equal(domain.ScoreDeltaQABugFound*2, qaDelta.delta)
	s.Len(tc.marked, 2)
}

func (s *ScorerSuite) TestQAForwardExitConfirmsValidAndInvalidScenarios() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	tc := &fakeTestCases{items: []domain.TaskTestCase{
		{ID: uuid.New(), Status: domain.TestCaseStatusPassed},
		{ID: uuid.New(), Status: domain.TestCaseStatusPassed},
		{ID: uuid.New(), Status: domain.TestCaseStatusPassed},
		{ID: uuid.New(), Status: domain.TestCaseStatusInvalid},
		{ID: uuid.New(), Status: domain.TestCaseStatusSkipped},
	}}
	tracker.SetTestCases(tc)

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnInQA, domain.TaskColumnPMUAT)

	s.Require().Len(perf.applied, 2)
	got := map[string]float64{}
	for _, a := range perf.applied {
		s.Equal(s.qa, a.agentID)
		got[a.eventType] = a.delta
	}
	s.Equal(domain.ScoreDeltaQAValidScenarioConfirmed*3, got[domain.ScoreEventQAValidScenarioConfirmed])
	s.Equal(domain.ScoreDeltaQAInvalidScenarioConfirmed*1, got[domain.ScoreEventQAInvalidScenarioConfirmed])
	s.Len(tc.marked, 5)
}

func (s *ScorerSuite) TestScoredTestCaseIsNeverCountedTwice() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	already := time.Now()
	tc := &fakeTestCases{items: []domain.TaskTestCase{
		{ID: uuid.New(), Status: domain.TestCaseStatusFailed, ScoredAt: &already},
	}}
	tracker.SetTestCases(tc)

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnInQA, domain.TaskColumnNeedRevision)

	for _, a := range perf.applied {
		s.NotEqual(domain.ScoreEventQABugFound, a.eventType)
	}
	s.Empty(tc.marked)
}

func (s *ScorerSuite) TestQARoundForwardExitDoneAlsoCredits() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	tc := &fakeTestCases{items: []domain.TaskTestCase{
		{ID: uuid.New(), Status: domain.TestCaseStatusPassed},
	}}
	tracker.SetTestCases(tc)

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnInQA, domain.TaskColumnDone)

	var qaEvent *recordedDelta
	for i, a := range perf.applied {
		if a.eventType == domain.ScoreEventQAValidScenarioConfirmed {
			qaEvent = &perf.applied[i]
		}
	}
	s.Require().NotNil(qaEvent)
	s.Equal(s.qa, qaEvent.agentID)
}

func (s *ScorerSuite) TestQARoundWithoutTestCaseStoreIsNoop() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())

	s.NotPanics(func() {
		tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
			domain.TaskColumnInQA, domain.TaskColumnNeedRevision)
	})

	for _, a := range perf.applied {
		s.NotEqual(domain.ScoreEventQABugFound, a.eventType)
	}
}

func (s *ScorerSuite) TestQAForwardExitCreditsQATaskTested() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	tracker.SetEvents(&fakeEvents{perf: perf})
	task := domain.BoardTask{ID: uuid.New()}

	tracker.OnColumnTransition(context.Background(), task, domain.TaskColumnInQA, domain.TaskColumnPMUAT)

	var got *recordedDelta
	for i, a := range perf.applied {
		if a.eventType == domain.ScoreEventQATaskTested {
			got = &perf.applied[i]
		}
	}
	s.Require().NotNil(got)
	s.Equal(s.qa, got.agentID)
	s.Equal(domain.ScoreDeltaQATaskTested, got.delta)
}

func (s *ScorerSuite) TestPMUATForwardExitCreditsPMUATCompleted() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	tracker.SetEvents(&fakeEvents{perf: perf})
	task := domain.BoardTask{ID: uuid.New()}

	tracker.OnColumnTransition(context.Background(), task, domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT)

	var got *recordedDelta
	for i, a := range perf.applied {
		if a.eventType == domain.ScoreEventPMUATCompleted {
			got = &perf.applied[i]
		}
	}
	s.Require().NotNil(got)
	s.Equal(s.pm, got.agentID)
	s.Equal(domain.ScoreDeltaPMUATCompleted, got.delta)
}

func (s *ScorerSuite) TestQATaskTestedIsNotCreditedTwiceAfterReEntry() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	tracker.SetEvents(&fakeEvents{perf: perf})
	task := domain.BoardTask{ID: uuid.New()}

	tracker.OnColumnTransition(context.Background(), task, domain.TaskColumnInQA, domain.TaskColumnPMUAT)
	tracker.OnColumnTransition(context.Background(), task, domain.TaskColumnPMUAT, domain.TaskColumnNeedRevision)
	tracker.OnColumnTransition(context.Background(), task, domain.TaskColumnInQA, domain.TaskColumnPMUAT)

	count := 0
	for _, a := range perf.applied {
		if a.eventType == domain.ScoreEventQATaskTested {
			count++
		}
	}
	s.Equal(1, count)
}

func (s *ScorerSuite) TestQABounceToNeedRevisionDoesNotCreditQATaskTested() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	tracker.SetEvents(&fakeEvents{perf: perf})

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnInQA, domain.TaskColumnNeedRevision)

	for _, a := range perf.applied {
		s.NotEqual(domain.ScoreEventQATaskTested, a.eventType)
	}
}

func (s *ScorerSuite) TestPMUATBounceToNeedRevisionDoesNotCreditPMUATCompleted() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	tracker.SetEvents(&fakeEvents{perf: perf})

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnPMUAT, domain.TaskColumnNeedRevision)

	for _, a := range perf.applied {
		s.NotEqual(domain.ScoreEventPMUATCompleted, a.eventType)
	}
}

func (s *ScorerSuite) TestRoleCompletionWithoutEventsIsNoop() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())

	s.NotPanics(func() {
		tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
			domain.TaskColumnInQA, domain.TaskColumnPMUAT)
	})

	for _, a := range perf.applied {
		s.NotEqual(domain.ScoreEventQATaskTested, a.eventType)
	}
}

func (s *ScorerSuite) TestQABugFoundWithoutSpansSkipsSilently() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tc := &fakeTestCases{items: []domain.TaskTestCase{
		{ID: uuid.New(), Status: domain.TestCaseStatusFailed},
	}}
	tracker.SetTestCases(tc)
	assignee := uuid.New()

	s.NotPanics(func() {
		tracker.OnColumnTransition(context.Background(),
			domain.BoardTask{ID: uuid.New(), AssigneeAgentID: &assignee},
			domain.TaskColumnInQA, domain.TaskColumnNeedRevision)
	})

	for _, a := range perf.applied {
		s.NotEqual(domain.ScoreEventQABugFound, a.eventType)
	}
	s.Empty(tc.marked)
}
