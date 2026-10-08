package board_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type memReviewStore struct {
	spans    []domain.TaskColumnSpan
	verdicts []domain.TaskReviewVerdict
	required []domain.ReviewerRef
	clock    time.Time
}

func (m *memReviewStore) TaskSpans(context.Context, uuid.UUID) ([]domain.TaskColumnSpan, error) {
	return m.spans, nil
}

func (m *memReviewStore) RecordReviewVerdict(_ context.Context, v domain.TaskReviewVerdict) error {
	m.clock = m.clock.Add(time.Minute)
	v.DecidedAt = m.clock
	for i, existing := range m.verdicts {
		if existing.SpanID == v.SpanID && existing.AgentID != nil && v.AgentID != nil && *existing.AgentID == *v.AgentID {
			m.verdicts[i] = v
			return nil
		}
	}
	for _, r := range m.required {
		if v.AgentID != nil && r.ID == *v.AgentID {
			v.AgentName = r.Name
		}
	}
	m.verdicts = append(m.verdicts, v)
	return nil
}

func (m *memReviewStore) ListReviewVerdicts(context.Context, uuid.UUID) ([]domain.TaskReviewVerdict, error) {
	return m.verdicts, nil
}

func (m *memReviewStore) RequiredReviewers(context.Context, string, string) ([]domain.ReviewerRef, error) {
	return m.required, nil
}

func (m *memReviewStore) visit(column domain.TaskColumn, open bool) domain.TaskColumnSpan {
	m.clock = m.clock.Add(time.Hour)
	sp := domain.TaskColumnSpan{ID: uuid.New(), BoardColumn: string(column), EnteredAt: m.clock}
	if n := len(m.spans); n > 0 && m.spans[n-1].LeftAt == nil {
		left := m.clock
		m.spans[n-1].LeftAt = &left
	}
	if !open {
		left := m.clock.Add(time.Minute)
		sp.LeftAt = &left
	}
	m.spans = append(m.spans, sp)
	return sp
}

type ReviewQuorumSuite struct {
	suite.Suite
	store     *memReviewStore
	quorum    *board.ReviewQuorum
	task      domain.BoardTask
	architect uuid.UUID
	security  uuid.UUID
}

func TestReviewQuorumSuite(t *testing.T) { suite.Run(t, new(ReviewQuorumSuite)) }

func (s *ReviewQuorumSuite) SetupTest() {
	s.architect, s.security = uuid.New(), uuid.New()
	s.store = &memReviewStore{
		clock: time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC),
		required: []domain.ReviewerRef{
			{ID: s.security, Name: "security-agent"},
			{ID: s.architect, Name: "system-architect"},
		},
	}
	s.quorum = board.NewReviewQuorum(s.store, workflowtest.Default().Reader())
	s.task = domain.BoardTask{ID: uuid.New(), TaskType: "task", Column: domain.TaskColumnCodeReview}
	s.store.visit(domain.TaskColumnInProgress, false)
	s.store.visit(domain.TaskColumnCodeReview, true)
}

func (s *ReviewQuorumSuite) move(agent uuid.UUID, to domain.TaskColumn) board.QuorumDecision {
	d, err := s.quorum.Decide(context.Background(), s.task, domain.TaskColumnCodeReview, to, domain.TaskActorAgent, &agent)
	s.Require().NoError(err)
	return d
}

func (s *ReviewQuorumSuite) TestFirstApprovalIsHeldForTheOtherReviewer() {
	d := s.move(s.architect, domain.TaskColumnReadyForQA)

	s.True(d.Hold)
	s.Equal([]string{"security-agent"}, d.Pending)
	s.True(s.quorum.HasDecided(context.Background(), s.task.ID, domain.TaskColumnCodeReview, s.architect))
	s.False(s.quorum.HasDecided(context.Background(), s.task.ID, domain.TaskColumnCodeReview, s.security))
}

func (s *ReviewQuorumSuite) TestEveryApprovalLetsTheCardThrough() {
	s.move(s.architect, domain.TaskColumnReadyForQA)
	d := s.move(s.security, domain.TaskColumnReadyForQA)

	s.False(d.Hold)
	s.Equal(domain.TaskColumnReadyForQA, d.Target)
}

func (s *ReviewQuorumSuite) TestASecurityRejectionOverridesTheArchitectsApproval() {
	s.move(s.architect, domain.TaskColumnReadyForQA)
	d := s.move(s.security, domain.TaskColumnNeedRevision)

	s.False(d.Hold)
	s.Equal(domain.TaskColumnNeedRevision, d.Target)
}

func (s *ReviewQuorumSuite) TestAnEarlyRejectionStillWaitsForTheOtherReviewer() {
	first := s.move(s.security, domain.TaskColumnNeedRevision)
	s.True(first.Hold, "the developer gets both reviews in one round, not one at a time")

	last := s.move(s.architect, domain.TaskColumnReadyForQA)
	s.False(last.Hold)
	s.Equal(domain.TaskColumnNeedRevision, last.Target, "an approval completing a rejected round sends the card back")
}

func (s *ReviewQuorumSuite) TestAReviewerCanChangeItsVerdictWithinTheRound() {
	s.move(s.security, domain.TaskColumnNeedRevision)
	s.move(s.security, domain.TaskColumnReadyForQA)
	d := s.move(s.architect, domain.TaskColumnReadyForQA)

	s.Equal(domain.TaskColumnReadyForQA, d.Target)
}

func (s *ReviewQuorumSuite) TestAParkKeepsTheRoundsVerdicts() {
	s.move(s.architect, domain.TaskColumnReadyForQA)
	s.store.visit(domain.TaskColumnBlocked, false)
	s.store.visit(domain.TaskColumnCodeReview, true)

	d := s.move(s.security, domain.TaskColumnReadyForQA)

	s.False(d.Hold, "the architect's approval before the park still counts")
	s.Equal(domain.TaskColumnReadyForQA, d.Target)
}

func (s *ReviewQuorumSuite) TestANewRoundStartsWithNoVerdicts() {
	s.move(s.architect, domain.TaskColumnReadyForQA)
	s.move(s.security, domain.TaskColumnNeedRevision)
	s.store.visit(domain.TaskColumnNeedRevision, false)
	s.store.visit(domain.TaskColumnInProgress, false)
	s.store.visit(domain.TaskColumnCodeReview, true)

	d := s.move(s.security, domain.TaskColumnReadyForQA)

	s.True(d.Hold)
	s.Equal([]string{"system-architect"}, d.Pending)
}

func (s *ReviewQuorumSuite) TestASingleReviewerIsNotHeld() {
	s.store.required = s.store.required[:1]

	d := s.move(s.security, domain.TaskColumnReadyForQA)

	s.False(d.Hold)
	s.Equal(domain.TaskColumnReadyForQA, d.Target)
}

func (s *ReviewQuorumSuite) TestHumansAndOtherColumnsAreNotVerdicts() {
	human, err := s.quorum.Decide(context.Background(), s.task, domain.TaskColumnCodeReview,
		domain.TaskColumnReadyForQA, domain.TaskActorHuman, nil)
	s.Require().NoError(err)
	s.False(human.Hold)

	qa := s.architect
	other, err := s.quorum.Decide(context.Background(), s.task, domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA, domain.TaskActorAgent, &qa)
	s.Require().NoError(err)
	s.False(other.Hold)
	s.Equal(domain.TaskColumnInQA, other.Target)

	sideways := s.move(s.architect, domain.TaskColumnTodo)
	s.False(sideways.Hold, "a move to neither exit is not a verdict")
	s.Empty(s.store.verdicts)
}

func (s *ReviewQuorumSuite) TestTaskReviewsShowsVerdictsAndPendingReviewers() {
	s.move(s.architect, domain.TaskColumnReadyForQA)
	s.move(s.security, domain.TaskColumnNeedRevision)
	s.store.visit(domain.TaskColumnNeedRevision, false)
	s.store.visit(domain.TaskColumnInProgress, false)
	s.store.visit(domain.TaskColumnCodeReview, true)
	s.move(s.architect, domain.TaskColumnReadyForQA)

	reviews, err := s.quorum.TaskReviews(context.Background(), s.task)
	s.Require().NoError(err)

	s.Equal("code_review", reviews.Column)
	s.Require().Len(reviews.Rounds, 2)

	first := reviews.Rounds[0]
	s.Equal(1, first.Round)
	s.Equal(domain.ReviewRoundRejected, first.Outcome)
	s.Require().Len(first.Reviewers, 2)
	verdicts := map[string]string{}
	for _, r := range first.Reviewers {
		verdicts[r.AgentName] = r.Verdict
		s.NotNil(r.DecidedAt)
	}
	s.Equal(map[string]string{
		"system-architect": domain.ReviewVerdictApprove,
		"security-agent":   domain.ReviewVerdictReject,
	}, verdicts)

	second := reviews.Rounds[1]
	s.Equal(domain.ReviewRoundOpen, second.Outcome)
	s.Require().Len(second.Reviewers, 2)
	s.Equal("system-architect", second.Reviewers[0].AgentName)
	s.Equal(domain.ReviewVerdictApprove, second.Reviewers[0].Verdict)
	s.Equal("security-agent", second.Reviewers[1].AgentName)
	s.Equal(domain.ReviewVerdictPending, second.Reviewers[1].Verdict)
	s.True(second.Reviewers[1].Required)
	s.Nil(second.Reviewers[1].DecidedAt)
}

func (s *ReviewQuorumSuite) TestTaskReviewsOfAnUnreviewedTaskIsEmpty() {
	fresh := board.NewReviewQuorum(&memReviewStore{}, workflowtest.Default().Reader())
	reviews, err := fresh.TaskReviews(context.Background(), domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnTodo})
	s.Require().NoError(err)
	s.NotNil(reviews.Rounds)

	var nilQuorum *board.ReviewQuorum
	empty, err := nilQuorum.TaskReviews(context.Background(), s.task)
	s.Require().NoError(err)
	s.Empty(empty.Rounds)
}

func (s *ReviewQuorumSuite) TestAHumanRejectionChargesEveryApprover() {
	s.move(s.architect, domain.TaskColumnReadyForQA)
	s.move(s.security, domain.TaskColumnReadyForQA)

	spans := &verdictSpans{
		open:    domain.TaskColumnSpan{BoardColumn: string(domain.TaskColumnCodeReview), ReviewVerdict: domain.ReviewVerdictApprove, AgentID: &s.architect},
		hasOpen: true,
	}
	escapes := &escapeRecorder{}
	gate := newReviewGate(spans, escapes)
	gate.SetQuorum(s.quorum)

	gate.OnHumanRejection(context.Background(), s.task, domain.TaskColumnCodeReview)

	s.ElementsMatch([]uuid.UUID{s.architect, s.security}, escapes.charged)
}
