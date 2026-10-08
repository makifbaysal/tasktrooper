package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type recordingApprover struct{ tasks []uuid.UUID }

func (r *recordingApprover) ApproveForTask(_ context.Context, taskID uuid.UUID) []domain.DesignSystem {
	r.tasks = append(r.tasks, taskID)
	return nil
}

func TestApprovingADesignTaskApprovesItsDesignSystem(t *testing.T) {
	for _, tc := range []struct {
		name     string
		taskType domain.TaskType
		from, to domain.TaskColumn
		approves bool
	}{
		{"design approved in analiz_review", domain.TaskTypeDesign, domain.TaskColumnAnalizReview, domain.TaskColumnDone, true},
		{"design sent back", domain.TaskTypeDesign, domain.TaskColumnAnalizReview, domain.TaskColumnNeedRevision, false},
		{"analiz approval approves nothing", domain.TaskTypeAnaliz, domain.TaskColumnAnalizReview, domain.TaskColumnDone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoID, taskID, agentID := uuid.New(), uuid.New(), uuid.New()
			tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
				ID: taskID, RepositoryID: repoID, Key: "D-1", TaskType: tc.taskType, Column: tc.from, AssigneeAgentID: &agentID,
			}}
			approver := &recordingApprover{}
			svc := &Service{
				repos:         &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
				tasks:         tasks,
				spans:         visited(domain.TaskColumnInProgress, domain.TaskColumnAnalizReview),
				pipelineStore: &fakeDeployPipelines{},
				workflows:     workflowtest.Default().Reader(),
			}
			svc.SetDesignSystemApprover(approver)

			target := tc.to
			_, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
				Column: &target, Actor: domain.TaskActorHuman,
			})
			require.NoError(t, err)
			if tc.approves {
				assert.Equal(t, []uuid.UUID{taskID}, approver.tasks)
			} else {
				assert.Empty(t, approver.tasks)
			}
		})
	}
}
