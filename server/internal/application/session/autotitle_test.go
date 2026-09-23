package session_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type autoTitleStore struct {
	port.SessionStore

	updateCalls int
	title       string
	autoTitled  bool
}

func (s *autoTitleStore) UpdateTitle(_ context.Context, _ uuid.UUID, title string, autoTitled bool) error {
	s.updateCalls++
	s.title = title
	s.autoTitled = autoTitled
	return nil
}

type stubTitleGenerator struct {
	calls int
	title string
	err   error
}

func (g *stubTitleGenerator) GenerateTitle(_ context.Context, _, _, _ string, _ domain.LLMProviderType) (string, error) {
	g.calls++
	return g.title, g.err
}

func agentSession() domain.Session {
	agentID := uuid.New()
	return domain.Session{ID: uuid.New(), AgentID: &agentID, Title: "chat with agent"}
}

func TestMaybeAutoTitleSetsTheGeneratedTitleOnceOnAFreshAgentChat(t *testing.T) {
	store := &autoTitleStore{}
	gen := &stubTitleGenerator{title: "Postgres migration rollback"}
	sess := agentSession()

	session.MaybeAutoTitleForTest(context.Background(), store, gen, sess, sess.ID, "how do I roll back a migration?", "run the .down.sql file", "test-model", domain.LLMProviderAnthropic)

	require.Equal(t, 1, store.updateCalls)
	assert.Equal(t, "Postgres migration rollback", store.title)
	assert.True(t, store.autoTitled)
	assert.Equal(t, 1, gen.calls)
}

func TestMaybeAutoTitleFallsBackToTheTruncatedUserMessageWhenGenerationFails(t *testing.T) {
	store := &autoTitleStore{}
	gen := &stubTitleGenerator{err: errors.New("provider unavailable")}
	sess := agentSession()

	session.MaybeAutoTitleForTest(context.Background(), store, gen, sess, sess.ID, "  merhaba dünya  ", "cevap", "test-model", domain.LLMProviderAnthropic)

	require.Equal(t, 1, store.updateCalls)
	assert.Equal(t, "merhaba dünya", store.title)
	assert.True(t, store.autoTitled)
}

func TestMaybeAutoTitleFallsBackWhenGenerationReturnsAnEmptyString(t *testing.T) {
	store := &autoTitleStore{}
	gen := &stubTitleGenerator{title: "   "}
	sess := agentSession()

	session.MaybeAutoTitleForTest(context.Background(), store, gen, sess, sess.ID, "kullanıcı mesajı", "cevap", "test-model", domain.LLMProviderAnthropic)

	require.Equal(t, 1, store.updateCalls)
	assert.Equal(t, "kullanıcı mesajı", store.title)
	assert.True(t, store.autoTitled)
}

func TestMaybeAutoTitleDoesNotRunTwiceOnceAlreadyAutoTitled(t *testing.T) {
	store := &autoTitleStore{}
	gen := &stubTitleGenerator{title: "should not be used"}
	sess := agentSession()
	sess.AutoTitled = true

	session.MaybeAutoTitleForTest(context.Background(), store, gen, sess, sess.ID, "another message", "another reply", "test-model", domain.LLMProviderAnthropic)

	assert.Equal(t, 0, gen.calls)
	assert.Equal(t, 0, store.updateCalls)
}

func TestMaybeAutoTitleNeverRunsOnATaskBoundChat(t *testing.T) {
	store := &autoTitleStore{}
	gen := &stubTitleGenerator{title: "should not be used"}
	sess := agentSession()
	taskID := uuid.New()
	sess.TaskID = &taskID

	session.MaybeAutoTitleForTest(context.Background(), store, gen, sess, sess.ID, "message", "reply", "test-model", domain.LLMProviderAnthropic)

	assert.Equal(t, 0, gen.calls)
	assert.Equal(t, 0, store.updateCalls)
}
