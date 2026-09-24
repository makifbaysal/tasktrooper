package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeStageEvidence struct {
	verdicts map[string]string
	err      error
}

func (f *fakeStageEvidence) LatestVerdicts(context.Context, uuid.UUID) (map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.verdicts, nil
}

func visited(columns ...domain.TaskColumn) *fakeStageEvidence {
	v := make(map[string]string, len(columns))
	for _, c := range columns {
		v[string(c)] = ""
	}
	return &fakeStageEvidence{verdicts: v}
}

func (f *fakeStageEvidence) withVerdict(col domain.TaskColumn, verdict string) *fakeStageEvidence {
	f.verdicts[string(col)] = verdict
	return f
}

type fakeColumns struct {
	slugs map[string]bool
}

func boardWith(columns ...domain.TaskColumn) *fakeColumns {
	m := make(map[string]bool, len(columns))
	for _, c := range columns {
		m[string(c)] = true
	}
	return &fakeColumns{slugs: m}
}

func (f *fakeColumns) ValidateColumn(_ context.Context, slug string) error {
	if f.slugs[slug] {
		return nil
	}
	return fmt.Errorf("invalid column: %s", slug)
}
func (f *fakeColumns) ValidateTransition(context.Context, string, string) error { return nil }

type fakeDeployPipelines struct {
	fakeReleasePipelineStore
	runs []domain.TaskPipeline
	err  error
}

func (f *fakeDeployPipelines) ListByTask(context.Context, uuid.UUID) ([]domain.TaskPipeline, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.runs, nil
}

func deployRun(trigger domain.PipelineTrigger, status domain.PipelineStatus) domain.TaskPipeline {
	return domain.TaskPipeline{ID: uuid.New(), Trigger: trigger, Status: status}
}

func TestReviewChainGate(t *testing.T) {
	cases := []struct {
		name     string
		taskType domain.TaskType
		spans    *fakeStageEvidence
		prev     domain.TaskColumn
		target   domain.TaskColumn
		wantErr  error
		wantSaid []string
	}{
		{
			name:     "task with the full chain reaches done",
			taskType: "task",
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
		},
		{
			name:     "bug with the full chain reaches done",
			taskType: "bug",
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
		},
		{
			name:     "human_uat is not required",
			taskType: "task",
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
		},
		{
			name:     "a task that bounced through need_revision and came back still passes",
			taskType: "task",
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision,
				domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target: domain.TaskColumnDone,
		},
		{
			name:     "in_progress straight to done is refused",
			taskType: "task",
			spans:    visited(domain.TaskColumnTodo, domain.TaskColumnInProgress),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"code review", "QA", "UAT"},
		},
		{
			name:     "missing code review is named",
			taskType: "task",
			spans:    visited(domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"code review", "code_review"},
		},
		{
			name:     "missing QA is named, and being queued for it is not passing it",
			taskType: "task",
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"QA", "in_qa"},
		},
		{
			name:     "missing UAT is named",
			taskType: "task",
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"UAT", "pm_uat"},
		},
		{
			name:     "released is gated too, so done cannot be skipped around",
			taskType: "task",
			spans:    visited(domain.TaskColumnInProgress),
			prev:     domain.TaskColumnHumanUAT,
			target:   domain.TaskColumnReleased,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"released"},
		},
		{

			name:     "the ordinary done to released promotion is not re-checked",
			taskType: "task",
			spans:    visited(domain.TaskColumnInProgress, domain.TaskColumnDone),
			prev:     domain.TaskColumnDone,
			target:   domain.TaskColumnReleased,
		},
		{
			name:     "a stage whose latest visit was rejected has not been passed",
			taskType: "task",
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT,
				domain.TaskColumnNeedRevision).withVerdict(domain.TaskColumnCodeReview, domain.ReviewVerdictReject),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewStageRejected,
			wantSaid: []string{"code review"},
		},
		{
			name:     "an approved verdict is not a rejection",
			taskType: "task",
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT).
				withVerdict(domain.TaskColumnCodeReview, domain.ReviewVerdictApprove).
				withVerdict(domain.TaskColumnPMUAT, domain.ReviewVerdictApprove),
			target: domain.TaskColumnDone,
		},
		{
			name:     "analiz needs only analiz_review",
			taskType: "analiz",
			spans:    visited(domain.TaskColumnInProgress, domain.TaskColumnAnalizReview),
			target:   domain.TaskColumnDone,
		},
		{
			name:     "analiz without its review is refused",
			taskType: "analiz",
			spans:    visited(domain.TaskColumnInProgress),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"analiz review", "analiz_review"},
		},
		{
			name:     "analiz is never asked for QA or UAT",
			taskType: "analiz",
			spans:    visited(domain.TaskColumnAnalizReview),
			target:   domain.TaskColumnReleased,
		},
		{
			name:     "moves that are not done or released are never gated",
			taskType: "task",
			spans:    visited(domain.TaskColumnInProgress),
			target:   domain.TaskColumnNeedRevision,
		},
		{
			name:     "unreadable history fails closed",
			taskType: "task",
			spans:    &fakeStageEvidence{err: errors.New("boom: pool exhausted")},
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"could not be read"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{spans: tc.spans, workflows: workflowtest.Default().Reader()}
			repo := domain.Repository{ID: uuid.New()}
			task := domain.BoardTask{ID: uuid.New(), Key: "APP-7", TaskType: tc.taskType}

			err := svc.reviewChainGate(context.Background(), repo, task, tc.prev, tc.target)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("want the move allowed, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
			for _, want := range tc.wantSaid {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("want %q named in the block, got: %v", want, err)
				}
			}
		})
	}
}

func TestReviewChainGateWithoutSpanStoreFailsClosed(t *testing.T) {
	svc := &Service{}
	err := svc.reviewChainGate(context.Background(),
		domain.Repository{},
		domain.BoardTask{ID: uuid.New(), TaskType: "task"},
		domain.TaskColumnInProgress, domain.TaskColumnDone)
	if !errors.Is(err, domain.ErrReviewChainIncomplete) {
		t.Fatalf("want ErrReviewChainIncomplete, got %v", err)
	}
}

func TestReviewChainGateSkipsStagesTheBoardDoesNotHave(t *testing.T) {
	repo := domain.Repository{ID: uuid.New()}
	task := domain.BoardTask{ID: uuid.New(), Key: "APP-9", TaskType: "task"}

	noQA := &Service{
		spans:     visited(domain.TaskColumnCodeReview, domain.TaskColumnPMUAT),
		columns:   boardWith(domain.TaskColumnCodeReview, domain.TaskColumnPMUAT, domain.TaskColumnDone),
		workflows: workflowtest.Default().Reader(),
	}
	if err := noQA.reviewChainGate(context.Background(), repo, task, domain.TaskColumnPMUAT, domain.TaskColumnDone); err != nil {
		t.Fatalf("a board without in_qa must not be deadlocked by the QA stage: %v", err)
	}

	missingUAT := &Service{
		spans:     visited(domain.TaskColumnCodeReview),
		columns:   boardWith(domain.TaskColumnCodeReview, domain.TaskColumnPMUAT, domain.TaskColumnDone),
		workflows: workflowtest.Default().Reader(),
	}
	err := missingUAT.reviewChainGate(context.Background(), repo, task, domain.TaskColumnCodeReview, domain.TaskColumnDone)
	if !errors.Is(err, domain.ErrReviewChainIncomplete) {
		t.Fatalf("the stages the board DOES have must still be enforced, got %v", err)
	}
	if !strings.Contains(err.Error(), "pm_uat") {
		t.Fatalf("want pm_uat named, got: %v", err)
	}
}

func TestUpdateTaskEnforcesReviewChainForEveryActor(t *testing.T) {
	agentID := uuid.New()

	cases := []struct {
		name    string
		actor   domain.TaskActor
		from    domain.TaskColumn
		to      domain.TaskColumn
		spans   *fakeStageEvidence
		runs    []domain.TaskPipeline
		wantErr error
	}{
		{
			name:    "human cannot drag an unreviewed task to done",
			actor:   domain.TaskActorHuman,
			from:    domain.TaskColumnInProgress,
			to:      domain.TaskColumnDone,
			spans:   visited(domain.TaskColumnInProgress),
			wantErr: domain.ErrReviewChainIncomplete,
		},
		{
			name:    "agent cannot move an unreviewed task to done either",
			actor:   domain.TaskActorAgent,
			from:    domain.TaskColumnInProgress,
			to:      domain.TaskColumnDone,
			spans:   visited(domain.TaskColumnInProgress),
			wantErr: domain.ErrReviewChainIncomplete,
		},
		{
			name:  "a reviewed task reaches done",
			actor: domain.TaskActorHuman,
			from:  domain.TaskColumnHumanUAT,
			to:    domain.TaskColumnDone,
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA,
				domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT),
		},
		{
			// require_release_deploy is gone: released is no longer gated on
			// any deploy evidence, only on having passed the review chain.
			name:  "done to released needs no deploy evidence",
			actor: domain.TaskActorSystem,
			from:  domain.TaskColumnDone,
			to:    domain.TaskColumnReleased,
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA,
				domain.TaskColumnPMUAT, domain.TaskColumnDone),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoID, taskID := uuid.New(), uuid.New()
			tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
				ID:              taskID,
				RepositoryID:    repoID,
				Key:             "APP-42",
				TaskType:        "task",
				Column:          tc.from,
				AssigneeAgentID: &agentID,
			}}
			svc := &Service{
				repos:         &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
				tasks:         tasks,
				spans:         tc.spans,
				pipelineStore: &fakeDeployPipelines{runs: tc.runs},
				workflows:     workflowtest.Default().Reader(),
			}

			target := tc.to
			_, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
				Column:       &target,
				Actor:        tc.actor,
				ActorAgentID: &agentID,
			})

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				if tasks.updated.Column == target {
					t.Fatalf("a blocked move must not be persisted, task landed in %s", tasks.updated.Column)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tasks.updated.Column != target {
				t.Fatalf("want the task moved to %s, got %s", target, tasks.updated.Column)
			}
		})
	}
}

// TestReviewChainGateEnforcedWithNoRepoFlag proves the review chain is no
// longer an opt-in: a repository with every field at its zero value (the old
// require_review_chain default) still blocks done on a missing review stage.
func TestReviewChainGateEnforcedWithNoRepoFlag(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
		ID: taskID, RepositoryID: repoID, TaskType: "task", Column: domain.TaskColumnInProgress,
	}}
	svc := &Service{
		repos:         &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:         tasks,
		spans:         visited(domain.TaskColumnInProgress),
		pipelineStore: &fakeDeployPipelines{},
		workflows:     workflowtest.Default().Reader(),
	}

	col := domain.TaskColumnDone
	_, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
		Column: &col,
		Actor:  domain.TaskActorHuman,
	})
	if !errors.Is(err, domain.ErrReviewChainIncomplete) {
		t.Fatalf("want ErrReviewChainIncomplete even with no repo flag set, got %v", err)
	}
}
