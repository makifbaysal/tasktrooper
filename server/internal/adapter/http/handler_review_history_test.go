package http

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/application/reviewhistory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type rhFindings struct {
	mu   sync.Mutex
	rows []domain.TaskReviewFinding
	acks map[string]domain.ReviewFlagAck
}

func (m *rhFindings) Insert(_ context.Context, rows []domain.TaskReviewFinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, rows...)
	return nil
}
func (m *rhFindings) ListByTask(context.Context, uuid.UUID) ([]domain.TaskReviewFinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.TaskReviewFinding(nil), m.rows...), nil
}
func (m *rhFindings) ListAcks(context.Context, uuid.UUID) ([]domain.ReviewFlagAck, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.ReviewFlagAck
	for _, a := range m.acks {
		out = append(out, a)
	}
	return out, nil
}
func (m *rhFindings) Ack(_ context.Context, a domain.ReviewFlagAck) (domain.ReviewFlagAck, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.acks == nil {
		m.acks = map[string]domain.ReviewFlagAck{}
	}
	if prev, ok := m.acks[a.FlagKey]; ok {
		return prev, nil
	}
	m.acks[a.FlagKey] = a
	return a, nil
}

func newReviewHistoryApp(t *testing.T) (*qFixture, *rhFindings) {
	t.Helper()
	repoID, taskID := uuid.New(), uuid.New()
	f := newQuestionTestApp(t, domain.BoardTask{ID: taskID, RepositoryID: repoID, Key: "T-1", TaskType: "task", Column: domain.TaskColumnHumanUAT})
	store := &rhFindings{}
	rules, err := reviewhistory.LoadRules(catalog.ReviewFS())
	require.NoError(t, err)
	repos := newFakeDeployOpsRepositoryStore([]domain.Repository{{ID: repoID, Name: "tasktrooper"}})
	svc := repository.NewService(repos, f.tasks, nil, nil, nil, &annCommentStore{}, nil, nil, nil, nil)
	svc.SetReviewHistory(&reviewhistory.Service{Tasks: f.tasks, Findings: store, Rules: rules})
	h := &Handler{repositorySvc: svc}
	f.app = fiber.New()
	h.registerRepositoryRoutes(f.app)
	return f, store
}

func TestReviewHistoryOfAFreshTaskIsEmptyArraysNotNull(t *testing.T) {
	f, _ := newReviewHistoryApp(t)
	status, raw := f.do(t, "GET", f.base()+"/review-history", nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &body))
	for _, key := range []string{"rounds", "findings", "flags"} {
		assert.JSONEq(t, `[]`, string(body[key]), key)
	}
	assert.Contains(t, string(body["summary"]), `"open_flags":0`)
}

func TestReviewHistoryOfAnUnknownTaskIs404(t *testing.T) {
	f, _ := newReviewHistoryApp(t)
	status, _ := f.do(t, "GET", "/v1/repositories/"+f.repoID.String()+"/tasks/"+uuid.NewString()+"/review-history", nil)
	assert.Equal(t, fiber.StatusNotFound, status)
}

func TestMarkingAFlagReviewedRecordsWhoAndShowsInTheHistory(t *testing.T) {
	f, store := newReviewHistoryApp(t)
	agent := uuid.New()
	require.NoError(t, store.Insert(context.Background(), (domain.ReviewReportInput{
		NeedsHumanReview: []domain.HumanReviewInput{{File: "pay.go", Line: 9, Reason: "touches payment logic I could not run", Question: "retry?"}},
	}).Rows(f.taskID, "code_review", &agent, "architect", "sha", time.Now())))

	status, raw := f.do(t, "GET", f.base()+"/review-history", nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var h domain.ReviewHistory
	require.NoError(t, json.Unmarshal(raw, &h))
	require.Len(t, h.Flags, 1)
	assert.Equal(t, 1, h.Summary.OpenFlags)
	assert.Equal(t, "retry?", h.Flags[0].Question)

	status, raw = f.do(t, "POST", f.base()+"/review-flags/"+h.Flags[0].ID+"/reviewed", nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var flag domain.ReviewFlag
	require.NoError(t, json.Unmarshal(raw, &flag))
	assert.Equal(t, "local", flag.ReviewedBy)
	assert.NotNil(t, flag.ReviewedAt)

	_, raw = f.do(t, "GET", f.base()+"/review-history", nil)
	require.NoError(t, json.Unmarshal(raw, &h))
	assert.Zero(t, h.Summary.OpenFlags)
	assert.True(t, h.Flags[0].Reviewed())

	status, _ = f.do(t, "POST", f.base()+"/review-flags/agent-never-raised/reviewed", nil)
	assert.Equal(t, fiber.StatusNotFound, status)
}
