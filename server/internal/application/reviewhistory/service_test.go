package reviewhistory_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/reviewhistory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type memFindings struct {
	mu   sync.Mutex
	rows []domain.TaskReviewFinding
	acks map[string]domain.ReviewFlagAck
}

func (m *memFindings) Insert(_ context.Context, rows []domain.TaskReviewFinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, rows...)
	return nil
}
func (m *memFindings) ListByTask(_ context.Context, id uuid.UUID) ([]domain.TaskReviewFinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.TaskReviewFinding
	for _, r := range m.rows {
		if r.TaskID == id {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memFindings) ListAcks(_ context.Context, id uuid.UUID) ([]domain.ReviewFlagAck, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.ReviewFlagAck
	for _, a := range m.acks {
		if a.TaskID == id {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *memFindings) Ack(_ context.Context, a domain.ReviewFlagAck) (domain.ReviewFlagAck, error) {
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

type oneTask struct{ task domain.BoardTask }

func (o oneTask) Get(_ context.Context, _, id uuid.UUID) (domain.BoardTask, error) {
	if id != o.task.ID {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return o.task, nil
}

func newService(t *testing.T) (*reviewhistory.Service, *memFindings, domain.BoardTask, uuid.UUID) {
	agent := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Column: domain.TaskColumnCodeReview}
	store := &memFindings{}
	svc := &reviewhistory.Service{
		Tasks: oneTask{task}, Findings: store, Rules: rules(t),
		AgentNames: func(context.Context) map[uuid.UUID]string { return map[uuid.UUID]string{agent: "architect"} },
		HeadSHA:    func(context.Context, uuid.UUID) string { return "abc123" },
		Now:        func() time.Time { return t0 },
	}
	return svc, store, task, agent
}

func TestARecordedReportCarriesTheGateTheReviewerAndTheCommit(t *testing.T) {
	svc, store, task, agent := newService(t)
	n, err := svc.RecordReport(context.Background(), task.RepositoryID, task.ID, agent, domain.ReviewReportInput{
		Findings:         []domain.ReviewFindingInput{{Key: "A-1", Message: "bug", File: "a.go"}},
		NeedsHumanReview: []domain.HumanReviewInput{{File: "p.go", Reason: "payments"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	require.Len(t, store.rows, 2)
	for _, r := range store.rows {
		assert.Equal(t, "code_review", r.Gate)
		assert.Equal(t, "architect", r.AgentName)
		assert.Equal(t, "abc123", r.HeadSHA)
		assert.Equal(t, task.ID, r.TaskID)
	}
	_, err = svc.RecordReport(context.Background(), task.RepositoryID, task.ID, agent, domain.ReviewReportInput{})
	assert.ErrorIs(t, err, domain.ErrReviewReportInvalid)
	_, err = svc.RecordReport(context.Background(), task.RepositoryID, uuid.New(), agent, domain.ReviewReportInput{NotReviewed: []domain.NotReviewedInput{{Scope: "x", Reason: "y"}}})
	assert.ErrorIs(t, err, domain.ErrBoardTaskNotFound)
}

func TestMarkingAFlagReviewedKeepsTheFirstReviewerAndCountsDown(t *testing.T) {
	svc, _, task, agent := newService(t)
	ctx := context.Background()
	_, err := svc.RecordReport(ctx, task.RepositoryID, task.ID, agent, domain.ReviewReportInput{
		NeedsHumanReview: []domain.HumanReviewInput{{File: "p.go", Reason: "touches payment logic I could not run", Question: "retry?"}},
	})
	require.NoError(t, err)
	h, err := svc.History(ctx, task.RepositoryID, task.ID)
	require.NoError(t, err)
	require.Len(t, h.Flags, 1)
	assert.Equal(t, 1, h.Summary.OpenFlags)
	assert.Equal(t, 1, svc.OpenFlagCount(ctx, task.RepositoryID, task.ID))

	flag, err := svc.MarkReviewed(ctx, task.RepositoryID, task.ID, h.Flags[0].ID, "ayse")
	require.NoError(t, err)
	assert.Equal(t, "ayse", flag.ReviewedBy)
	again, err := svc.MarkReviewed(ctx, task.RepositoryID, task.ID, h.Flags[0].ID, "mehmet")
	require.NoError(t, err)
	assert.Equal(t, "ayse", again.ReviewedBy)

	h, _ = svc.History(ctx, task.RepositoryID, task.ID)
	assert.Zero(t, h.Summary.OpenFlags)
	assert.True(t, h.Flags[0].Reviewed())

	_, err = svc.MarkReviewed(ctx, task.RepositoryID, task.ID, "agent-nothing", "ayse")
	assert.ErrorIs(t, err, domain.ErrReviewFlagNotFound)
}

func TestAQAVerdictIsKeptAsItWasGivenAndFeedsTheHistory(t *testing.T) {
	svc, store, task, agent := newService(t)
	task.Column = domain.TaskColumnInQA
	crit := domain.AcceptanceCriterion{ID: uuid.New(), TaskID: task.ID, Text: "user can log in"}
	svc.RecordCriterionCheck(context.Background(), task, crit, check(agent, false, "500 on POST /login"))
	svc.RecordCriterionCheck(context.Background(), task, crit, check(agent, true, ""))
	require.Len(t, store.rows, 2)
	assert.Equal(t, domain.ReviewFindingOpen, store.rows[0].Status)
	assert.Equal(t, "500 on POST /login", store.rows[0].Message)
	assert.Equal(t, domain.ReviewFindingPassed, store.rows[1].Status)
	assert.Equal(t, store.rows[0].Fingerprint, store.rows[1].Fingerprint)
	assert.Equal(t, "in_qa", store.rows[0].Gate)
}

func TestAServiceThatIsNotWiredReadsAsEmptyAndRefusesWrites(t *testing.T) {
	var svc *reviewhistory.Service
	h, err := svc.History(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	assert.Empty(t, h.Rounds)
	_, err = svc.RecordReport(context.Background(), uuid.New(), uuid.New(), uuid.New(), domain.ReviewReportInput{})
	assert.Error(t, err)
	svc.RecordCriterionCheck(context.Background(), domain.BoardTask{}, domain.AcceptanceCriterion{}, domain.CriterionCheck{})
}

func check(agent uuid.UUID, approved bool, note string) domain.CriterionCheck {
	return domain.CriterionCheck{AgentID: &agent, Approved: approved, Note: note, Role: domain.CriterionReviewRoleQA}
}
