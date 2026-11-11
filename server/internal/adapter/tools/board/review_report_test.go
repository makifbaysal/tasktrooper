package board

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type reportingTaskManager struct {
	*documentTaskManager
	agent  uuid.UUID
	report domain.ReviewReportInput
}

func (r *reportingTaskManager) RecordReviewReport(_ context.Context, _, _, agentID uuid.UUID, report domain.ReviewReportInput) (int, error) {
	if err := report.Validate(); err != nil {
		return 0, err
	}
	r.agent, r.report = agentID, report
	return len(report.Findings) + len(report.NeedsHumanReview), nil
}

func reportKit(t *testing.T) (*ToolKit, *reportingTaskManager, uuid.UUID) {
	kit, tasks, taskID := newDocumentKit("task")
	mgr := &reportingTaskManager{documentTaskManager: tasks}
	kit.Tasks = mgr
	return kit, mgr, taskID
}

func TestRecordReviewFindingsPassesTheReportAndTheCallingAgentOn(t *testing.T) {
	kit, mgr, taskID := reportKit(t)
	agent := uuid.New()
	ctx := registry.ContextWithAgentID(context.Background(), agent)
	res := newRecordReviewFindingsTool(kit).Execute(ctx, `{"task_id":"`+taskID.String()+`",
		"findings":[{"key":"ARCH-1","file":"a.go","line":4,"severity":"high","category":"correctness","message":"nil deref","confidence":"low","human_review_reason":"could not run it"}],
		"needs_human_review":[{"file":"pay.go","reason":"touches payment logic I could not run","question":"does a retry double charge?"}],
		"not_reviewed":[{"scope":"web/","reason":"no browser"}],"reviewed_aspects":["security"]}`)
	require.False(t, res.IsError, res.Content)
	assert.Equal(t, agent, mgr.agent)
	require.Len(t, mgr.report.Findings, 1)
	assert.Equal(t, "ARCH-1", mgr.report.Findings[0].Key)
	assert.Equal(t, "web/", mgr.report.NotReviewed[0].Scope)
	assert.Contains(t, res.Content, `"recorded":2`)
}

func TestRecordReviewFindingsRefusesToFlagEverything(t *testing.T) {
	kit, _, taskID := reportKit(t)
	ctx := registry.ContextWithAgentID(context.Background(), uuid.New())
	flags := ""
	for i := 0; i < 6; i++ {
		flags += `{"reason":"unsure"},`
	}
	res := newRecordReviewFindingsTool(kit).Execute(ctx, `{"task_id":"`+taskID.String()+`","needs_human_review":[`+flags[:len(flags)-1]+`]}`)
	require.True(t, res.IsError)
	assert.Contains(t, res.Content, "limit is 5")
}

func TestRecordReviewFindingsNeedsAnAgentAndAReport(t *testing.T) {
	kit, _, taskID := reportKit(t)
	res := newRecordReviewFindingsTool(kit).Execute(context.Background(), `{"task_id":"`+taskID.String()+`","findings":[{"message":"x"}]}`)
	assert.True(t, res.IsError)
	ctx := registry.ContextWithAgentID(context.Background(), uuid.New())
	res = newRecordReviewFindingsTool(kit).Execute(ctx, `{"task_id":"`+taskID.String()+`"}`)
	assert.True(t, res.IsError)
}
