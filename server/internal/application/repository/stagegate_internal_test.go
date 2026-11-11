package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// emptyWorkflowReader answers every task type with a workflow that has no
// stages at all — the "nobody has curated this type yet" case, which must
// stay unscoped/legacy-open rather than becoming a hard lock-out.
type emptyWorkflowReader struct{}

func (emptyWorkflowReader) Workflow(context.Context, domain.TaskType) (domain.Workflow, error) {
	return domain.Workflow{}, nil
}
func (emptyWorkflowReader) DefaultTaskType(context.Context) (domain.TaskType, error) { return "", nil }
func (emptyWorkflowReader) DefectTaskType(context.Context) (domain.TaskType, error)  { return "", nil }
func (emptyWorkflowReader) TaskTypeExists(context.Context, domain.TaskType) (bool, error) {
	return true, nil
}
func (emptyWorkflowReader) KeyPrefix(context.Context, domain.TaskType) (string, error) {
	return "", nil
}

func TestValidateStageConfiguredAllowsAColumnTheTypeHasAStageFor(t *testing.T) {
	svc := &Service{workflows: workflowtest.Default().Reader()}

	require.NoError(t, svc.validateStageConfigured(context.Background(), "task", domain.TaskColumnCodeReview))
}

func TestValidateStageConfiguredRefusesAColumnRemovedForAnaliz(t *testing.T) {
	svc := &Service{workflows: workflowtest.Default().Reader()}

	err := svc.validateStageConfigured(context.Background(), "analiz", domain.TaskColumnCodeReview)

	require.Error(t, err)
	require.ErrorContains(t, err, "don't use the code_review column")
	var gateErr *domain.StageNotOnWorkflowError
	require.True(t, errors.As(err, &gateErr), "the HTTP layer needs a typed error to render stage_not_on_workflow")
	require.Equal(t, domain.TaskType("analiz"), gateErr.TaskType)
	require.Equal(t, domain.TaskColumnCodeReview, gateErr.Target)
}

func TestValidateStageConfiguredAllowsAColumnAnalizStillHas(t *testing.T) {
	svc := &Service{workflows: workflowtest.Default().Reader()}

	require.NoError(t, svc.validateStageConfigured(context.Background(), "analiz", domain.TaskColumnAnalizReview))
	require.NoError(t, svc.validateStageConfigured(context.Background(), "analiz", domain.TaskColumnNeedRevision))
}

func TestValidateStageConfiguredAllowsEveryColumnForAnUncuratedType(t *testing.T) {
	svc := &Service{workflows: emptyWorkflowReader{}}

	require.NoError(t, svc.validateStageConfigured(context.Background(), "custom_type", domain.TaskColumnPMUAT))
}
