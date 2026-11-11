package board

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeDeliverer struct {
	outcome   string
	calls     int
	interrupt bool
	summary   string
	author    uuid.UUID
}

func (f *fakeDeliverer) Deliver(_ context.Context, _, _, author uuid.UUID, summary string, interrupt bool) (board.ChangeDelivery, error) {
	f.calls++
	f.interrupt, f.summary, f.author = interrupt, summary, author
	runID := uuid.New()
	return board.ChangeDelivery{Outcome: f.outcome, RunID: &runID}, nil
}

func runNotify(t *testing.T, fake *fakeDeliverer, args string) (domain.ToolResult, uuid.UUID) {
	t.Helper()
	repoID, pm := uuid.New(), uuid.New()
	tool := newNotifyTaskAgentTool(&ToolKit{Tasks: &fakeTaskManager{taskRepoID: repoID}, ChangeRequests: fake})
	ctx := registry.ContextWithAgentID(registry.ContextWithRepositoryID(context.Background(), repoID), pm)
	return tool.Execute(ctx, args), pm
}

func TestNotifyTaskAgentTellsThePMWhatHappened(t *testing.T) {
	for outcome, want := range map[string]string{
		board.ChangeDeliveryNotified: "current pass ends",
		board.ChangeDeliveryStored:   "next run",
	} {
		fake := &fakeDeliverer{outcome: outcome}
		res, pm := runNotify(t, fake, `{"task_id":"`+uuid.NewString()+`","summary":"add an archive filter"}`)

		require.False(t, res.IsError, res.Content)
		var out notifyTaskAgentResult
		require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
		assert.Equal(t, outcome, out.Outcome)
		assert.Contains(t, out.Note, want)
		assert.False(t, fake.interrupt)
		assert.Equal(t, "add an archive filter", fake.summary)
		assert.Equal(t, pm, fake.author)
	}
}

func TestNotifyTaskAgentRefusesAnInterruptTheStakeholderDidNotApprove(t *testing.T) {
	fake := &fakeDeliverer{outcome: board.ChangeDeliveryInterrupted}

	res, _ := runNotify(t, fake, `{"task_id":"`+uuid.NewString()+`","summary":"stop and redo","interrupt":true}`)

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "user_approved")
	assert.Zero(t, fake.calls, "nothing is stopped without the yes")

	res, _ = runNotify(t, fake, `{"task_id":"`+uuid.NewString()+`","summary":"stop and redo","interrupt":true,"user_approved":true}`)
	require.False(t, res.IsError, res.Content)
	assert.True(t, fake.interrupt)
	assert.Contains(t, res.Content, "started again")
}

func TestNotifyTaskAgentWithoutTheServiceSaysSo(t *testing.T) {
	tool := newNotifyTaskAgentTool(&ToolKit{Tasks: &fakeTaskManager{}})
	res := tool.Execute(context.Background(), `{"task_id":"x","summary":"y"}`)
	assert.True(t, res.IsError)
}
