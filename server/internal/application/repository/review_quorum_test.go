package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type quorumLedger struct {
	span     domain.TaskColumnSpan
	verdicts []domain.TaskReviewVerdict
	required []domain.ReviewerRef
}

func (l *quorumLedger) TaskSpans(context.Context, uuid.UUID) ([]domain.TaskColumnSpan, error) {
	return []domain.TaskColumnSpan{l.span}, nil
}

func (l *quorumLedger) RecordReviewVerdict(_ context.Context, v domain.TaskReviewVerdict) error {
	v.DecidedAt = time.Now()
	l.verdicts = append(l.verdicts, v)
	return nil
}

func (l *quorumLedger) ListReviewVerdicts(context.Context, uuid.UUID) ([]domain.TaskReviewVerdict, error) {
	return l.verdicts, nil
}

func (l *quorumLedger) RequiredReviewers(context.Context, string, string) ([]domain.ReviewerRef, error) {
	return l.required, nil
}

type quorumFixture struct {
	svc       *Service
	tasks     *fakeReleaseTaskStore
	repoID    uuid.UUID
	taskID    uuid.UUID
	architect uuid.UUID
	security  uuid.UUID
}

func newQuorumFixture() quorumFixture {
	f := quorumFixture{repoID: uuid.New(), taskID: uuid.New(), architect: uuid.New(), security: uuid.New()}
	f.tasks = &fakeReleaseTaskStore{task: domain.BoardTask{
		ID: f.taskID, RepositoryID: f.repoID, TaskType: "task", Column: domain.TaskColumnCodeReview,
	}}
	ledger := &quorumLedger{
		span: domain.TaskColumnSpan{ID: uuid.New(), BoardColumn: string(domain.TaskColumnCodeReview), EnteredAt: time.Now()},
		required: []domain.ReviewerRef{
			{ID: f.architect, Name: "system-architect"},
			{ID: f.security, Name: "security-agent"},
		},
	}
	f.svc = &Service{
		repos:         &fakeReleaseRepoStore{repo: domain.Repository{ID: f.repoID}},
		tasks:         f.tasks,
		spans:         visited(domain.TaskColumnInProgress, domain.TaskColumnCodeReview),
		pipelineStore: &fakeDeployPipelines{},
		workflows:     workflowtest.Default().Reader(),
		columns:       boardWith(domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskColumnNeedRevision),
	}
	f.svc.SetReviewQuorum(board.NewReviewQuorum(ledger, workflowtest.Default().Reader()))
	return f
}

func (f quorumFixture) agentMove(t *testing.T, agent uuid.UUID, to domain.TaskColumn) domain.BoardTask {
	t.Helper()
	got, err := f.svc.UpdateTask(context.Background(), f.repoID, f.taskID, domain.UpdateBoardTaskRequest{
		Column: &to, Actor: domain.TaskActorAgent, ActorAgentID: &agent,
	})
	if err != nil {
		t.Fatalf("move to %s: %v", to, err)
	}
	if got.Column != domain.TaskColumnCodeReview {
		f.tasks.task = got
	}
	return got
}

func TestUpdateTaskHoldsTheFirstReviewersApproval(t *testing.T) {
	f := newQuorumFixture()

	got := f.agentMove(t, f.architect, domain.TaskColumnReadyForQA)

	if got.Column != domain.TaskColumnCodeReview {
		t.Fatalf("the card must wait in code_review for the security review, got %s", got.Column)
	}
	if f.tasks.updated.ID != uuid.Nil {
		t.Fatal("a held verdict must not write the task")
	}

	got = f.agentMove(t, f.security, domain.TaskColumnReadyForQA)
	if got.Column != domain.TaskColumnReadyForQA {
		t.Fatalf("both approvals move the card on, got %s", got.Column)
	}
}

func TestUpdateTaskSendsAnApprovalBackWhenTheOtherReviewerRejected(t *testing.T) {
	f := newQuorumFixture()

	f.agentMove(t, f.security, domain.TaskColumnNeedRevision)
	got := f.agentMove(t, f.architect, domain.TaskColumnReadyForQA)

	if got.Column != domain.TaskColumnNeedRevision {
		t.Fatalf("a security rejection must win over the architect's approval, got %s", got.Column)
	}
	if f.tasks.updated.Column != domain.TaskColumnNeedRevision {
		t.Fatalf("the stored task must land in need_revision, got %s", f.tasks.updated.Column)
	}
}

func TestUpdateTaskLetsAHumanOverrideAPendingReview(t *testing.T) {
	f := newQuorumFixture()
	f.agentMove(t, f.architect, domain.TaskColumnReadyForQA)

	to := domain.TaskColumnReadyForQA
	got, err := f.svc.UpdateTask(context.Background(), f.repoID, f.taskID, domain.UpdateBoardTaskRequest{
		Column: &to, Actor: domain.TaskActorHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Column != domain.TaskColumnReadyForQA {
		t.Fatalf("a person moving the card is never held, got %s", got.Column)
	}
}
