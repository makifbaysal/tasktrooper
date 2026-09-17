package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// quotaParkStore records what parkTurnOnQuota does to the store: the park
// itself, and the transcript notice it leaves behind.
type quotaParkStore struct {
	port.SessionStore
	parkErr error

	parkedSessionID uuid.UUID
	parkedReq       domain.SessionMessageRequest
	parkedPolicy    domain.ToolPolicy
	parkedResumeAt  time.Time
	parkCalls       int

	appendedContent string
	appendCalls     int
}

func (s *quotaParkStore) ParkPendingTurn(_ context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest, policy domain.ToolPolicy, resumeAt time.Time) error {
	s.parkCalls++
	if s.parkErr != nil {
		return s.parkErr
	}
	s.parkedSessionID = sessionID
	s.parkedReq = req
	s.parkedPolicy = policy
	s.parkedResumeAt = resumeAt
	return nil
}

func (s *quotaParkStore) AppendMessage(_ context.Context, _ uuid.UUID, _ domain.Role, content string, _ []byte, _ []byte) (domain.SessionMessage, error) {
	s.appendCalls++
	s.appendedContent = content
	return domain.SessionMessage{}, nil
}

func TestParkTurnOnQuotaParksAndLeavesAQueuedNotice(t *testing.T) {
	store := &quotaParkStore{}
	sessionID := uuid.New()
	resumeAt := time.Now().Add(20 * time.Minute)
	block := &domain.QuotaBlock{ResumeAt: resumeAt, CLISessionID: "cli-sess-1"}
	req := domain.SessionMessageRequest{Role: domain.RoleUser, Content: "keep working on the migration"}
	policy := domain.ToolPolicy{AllowTools: []string{"run_terminal"}}

	err := session.ParkTurnOnQuotaForTest(context.Background(), store, sessionID, req, policy, block, "en")
	require.Error(t, err, "the return value is always the *domain.QuotaNotice — see parkTurnOnQuota's own comment")

	assert.Equal(t, 1, store.parkCalls)
	assert.Equal(t, sessionID, store.parkedSessionID)
	assert.Equal(t, req.Content, store.parkedReq.Content)
	assert.Equal(t, policy.AllowTools, store.parkedPolicy.AllowTools)
	assert.WithinDuration(t, resumeAt, store.parkedResumeAt, time.Second)

	require.Equal(t, 1, store.appendCalls)
	assert.Contains(t, store.appendedContent, domain.QuotaQueuedNoticePrefix,
		"a queued turn must not read as a rate-limit failure to a client keying styling off the prefix")
	assert.NotContains(t, store.appendedContent, domain.RateLimitNoticePrefix)

	// The returned error is itself the queued sentence, so a caller that
	// returns it straight through (SendMessage, SendMessageStream) hands its
	// own caller the right wording without re-deriving it.
	assert.Contains(t, err.Error(), "will send automatically")
}

func TestParkTurnOnQuotaFallsBackToAnErrorNoticeWhenTheParkItselfFails(t *testing.T) {
	store := &quotaParkStore{parkErr: errors.New("db unavailable")}
	block := &domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour)}

	err := session.ParkTurnOnQuotaForTest(context.Background(), store, uuid.New(), domain.SessionMessageRequest{}, domain.ToolPolicy{}, block, "en")
	require.Error(t, err)

	require.Equal(t, 1, store.appendCalls, "the user must still see something in the transcript, not silence")
	assert.NotContains(t, store.appendedContent, domain.QuotaQueuedNoticePrefix,
		"a park that was never written must not claim to be queued")
}
