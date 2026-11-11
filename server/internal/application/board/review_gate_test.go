package board_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

func newReviewGate(spans board.VerdictStore, escapes board.EscapeCharger) *board.ReviewGate {
	gate := board.NewReviewGate(spans, escapes)
	gate.SetWorkflows(workflowtest.Default().Reader())
	return gate
}

type verdictSpans struct {
	set     map[string]string
	open    domain.TaskColumnSpan
	hasOpen bool
}

func (v *verdictSpans) SetReviewVerdict(_ context.Context, _ uuid.UUID, column, verdict string) error {
	if v.set == nil {
		v.set = map[string]string{}
	}
	v.set[column] = verdict
	return nil
}

func (v *verdictSpans) OpenSpan(context.Context, uuid.UUID) (domain.TaskColumnSpan, bool, error) {
	return v.open, v.hasOpen, nil
}

type escapeRecorder struct{ charged []uuid.UUID }

func (e *escapeRecorder) ApplyReviewEscape(_ context.Context, _ domain.BoardTask, agentID uuid.UUID) {
	e.charged = append(e.charged, agentID)
}

type ReviewGateSuite struct{ suite.Suite }

func TestReviewGateSuite(t *testing.T) { suite.Run(t, new(ReviewGateSuite)) }

func (s *ReviewGateSuite) TestAgentApprovalIsHeldAsVerdict() {
	spans := &verdictSpans{}
	gate := newReviewGate(spans, &escapeRecorder{})

	allow := gate.InterceptAgentMove(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskActorAgent,
		domain.Repository{RequireHumanReview: true})

	s.False(allow, "the human, not the architect, advances an approved task")
	s.Equal(domain.ReviewVerdictApprove, spans.set["code_review"])
}

func (s *ReviewGateSuite) TestAgentRejectionMovesImmediately() {
	spans := &verdictSpans{}
	gate := newReviewGate(spans, &escapeRecorder{})

	allow := gate.InterceptAgentMove(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.TaskActorAgent,
		domain.Repository{RequireHumanReview: true})

	s.True(allow, "human approval gates letting work through, not sending it back")
	s.Equal(domain.ReviewVerdictReject, spans.set["code_review"])
}

func (s *ReviewGateSuite) TestQAApprovalIsNotHeldBecauseHumanUATIsTheHumanGate() {
	spans := &verdictSpans{}
	gate := newReviewGate(spans, &escapeRecorder{})

	allow := gate.InterceptAgentMove(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnInQA, domain.TaskColumnHumanUAT, domain.TaskActorAgent,
		domain.Repository{RequireHumanReview: true})

	s.True(allow)
	s.Empty(spans.set)
}

func (s *ReviewGateSuite) TestHumanMoveIsNeverIntercepted() {
	spans := &verdictSpans{}
	gate := newReviewGate(spans, &escapeRecorder{})

	allow := gate.InterceptAgentMove(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskActorHuman,
		domain.Repository{RequireHumanReview: true})

	s.True(allow)
	s.Empty(spans.set)
}

func (s *ReviewGateSuite) TestNoInterceptionWhenHumanReviewIsOff() {
	spans := &verdictSpans{}
	gate := newReviewGate(spans, &escapeRecorder{})

	allow := gate.InterceptAgentMove(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskActorAgent,
		domain.Repository{RequireHumanReview: false})

	s.True(allow)
	s.Empty(spans.set)
}

func (s *ReviewGateSuite) TestNonReviewColumnIsNeverIntercepted() {
	spans := &verdictSpans{}
	gate := newReviewGate(spans, &escapeRecorder{})

	allow := gate.InterceptAgentMove(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnInProgress, domain.TaskColumnCodeReview, domain.TaskActorAgent,
		domain.Repository{RequireHumanReview: true})

	s.True(allow)
	s.Empty(spans.set)
}

func (s *ReviewGateSuite) TestHumanRejectionOfApprovedSpanChargesTheReviewer() {
	architect := uuid.New()
	spans := &verdictSpans{
		open: domain.TaskColumnSpan{
			BoardColumn:   "code_review",
			AgentID:       &architect,
			ReviewVerdict: domain.ReviewVerdictApprove,
		},
		hasOpen: true,
	}
	escapes := &escapeRecorder{}
	gate := newReviewGate(spans, escapes)

	gate.OnHumanRejection(context.Background(), domain.BoardTask{ID: uuid.New()}, domain.TaskColumnCodeReview)

	s.Equal([]uuid.UUID{architect}, escapes.charged)
}

func (s *ReviewGateSuite) TestHumanRejectionOfRejectedSpanChargesNobody() {
	architect := uuid.New()
	spans := &verdictSpans{
		open: domain.TaskColumnSpan{
			BoardColumn:   "code_review",
			AgentID:       &architect,
			ReviewVerdict: domain.ReviewVerdictReject,
		},
		hasOpen: true,
	}
	escapes := &escapeRecorder{}
	gate := newReviewGate(spans, escapes)

	gate.OnHumanRejection(context.Background(), domain.BoardTask{ID: uuid.New()}, domain.TaskColumnCodeReview)

	s.Empty(escapes.charged)
}

func (s *ReviewGateSuite) TestHumanRejectionWithNoReviewerChargesNobody() {
	spans := &verdictSpans{
		open:    domain.TaskColumnSpan{BoardColumn: "code_review", ReviewVerdict: domain.ReviewVerdictApprove},
		hasOpen: true,
	}
	escapes := &escapeRecorder{}
	gate := newReviewGate(spans, escapes)

	gate.OnHumanRejection(context.Background(), domain.BoardTask{ID: uuid.New()}, domain.TaskColumnCodeReview)

	s.Empty(escapes.charged)
}
