package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type recordingStore struct {
	steps     []string
	runStatus string
}

func (s *recordingStore) CreateRun(_ context.Context, _ *uuid.UUID, _, _ string) (domain.SessionRun, error) {
	return domain.SessionRun{ID: uuid.New()}, nil
}

func (s *recordingStore) CompleteRun(ctx context.Context, _ uuid.UUID, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.runStatus = status
	return nil
}

func (s *recordingStore) CancelRun(ctx context.Context, _ uuid.UUID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.runStatus != "" {
		return false, nil
	}
	s.runStatus = domain.TaskAgentRunStatusCancelled
	return true, nil
}

func (s *recordingStore) RunStatus(context.Context, uuid.UUID) (string, error) { return "", nil }

func (s *recordingStore) AppendStep(ctx context.Context, _ uuid.UUID, stepType string, _ []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.steps = append(s.steps, stepType)
	return nil
}

func (s *recordingStore) ListRunsBySession(context.Context, uuid.UUID, int) ([]domain.SessionRun, error) {
	return nil, nil
}

func (s *recordingStore) ListStepsByRun(context.Context, uuid.UUID) ([]domain.SessionStep, error) {
	return nil, nil
}

func (s *recordingStore) ListActiveRuns(context.Context) ([]domain.SessionRun, error) {
	return nil, nil
}

func TestRecorderPersistsTerminalStateAfterContextCancelled(t *testing.T) {
	store := &recordingStore{}
	ctx, cancel := context.WithCancel(context.Background())
	runCtx, rec, err := activity.StartRun(ctx, store, nil, "req-1", "model")
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, runCtx)

	cancel()

	rec.Step("subtask_failed", map[string]string{"task_key": "t1"})
	rec.Complete("failed")

	assert.Equal(t, []string{"subtask_failed"}, store.steps)
	assert.Equal(t, "failed", store.runStatus)
}

func (s *recordingStore) ListStepsByRunSince(ctx context.Context, runID uuid.UUID, _ time.Time) ([]domain.SessionStep, error) {
	return s.ListStepsByRun(ctx, runID)
}

type payloadStore struct {
	recordingStore
	payloads []string
}

func (s *payloadStore) AppendStep(ctx context.Context, runID uuid.UUID, stepType string, payload []byte) error {
	s.payloads = append(s.payloads, string(payload))
	return s.recordingStore.AppendStep(ctx, runID, stepType, payload)
}

func TestWithStepTagMergesIntoObjectPayloads(t *testing.T) {
	store := &payloadStore{}
	ctx, _, err := activity.StartRun(context.Background(), store, nil, "req", "m")
	require.NoError(t, err)

	tagged := activity.WithStepTag(ctx, "task_key", "t1")
	activity.FromContext(tagged).Step("a", map[string]string{"x": "1"})
	activity.FromContext(tagged).Step("b", map[string]string{"task_key": "own"})
	activity.FromContext(tagged).Step("c", []string{"x"})
	activity.FromContext(tagged).Step("d", nil)
	activity.FromContext(ctx).Step("e", map[string]string{"x": "1"})

	assert.JSONEq(t, `{"x":"1","task_key":"t1"}`, store.payloads[0])
	assert.JSONEq(t, `{"task_key":"own"}`, store.payloads[1], "a key the caller set is never overwritten")
	assert.JSONEq(t, `["x"]`, store.payloads[2], "non-object payloads are left alone")
	assert.JSONEq(t, `{"task_key":"t1"}`, store.payloads[3])
	assert.JSONEq(t, `{"x":"1"}`, store.payloads[4], "the parent ctx is not tagged by a child")
}

func TestWithStepTagNestsAndKeepsRunID(t *testing.T) {
	store := &payloadStore{}
	ctx, rec, err := activity.StartRun(context.Background(), store, nil, "req", "m")
	require.NoError(t, err)

	nested := activity.WithStepTag(activity.WithStepTag(ctx, "task_key", "t1"), "wave", "2")
	activity.FromContext(nested).Step("a", map[string]string{})

	assert.JSONEq(t, `{"task_key":"t1","wave":"2"}`, store.payloads[0])
	assert.Equal(t, rec.RunID(), activity.FromContext(nested).RunID())
}

func TestWithStepTagWithoutRecorderIsANoop(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, ctx, activity.WithStepTag(ctx, "k", "v"))
}
