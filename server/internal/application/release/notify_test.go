package release

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type sentNotifications struct {
	mu   sync.Mutex
	sent []domain.Notification
}

func (s *sentNotifications) all() []domain.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.Notification(nil), s.sent...)
}

func withNotifier(t *testing.T, svc *Service) *sentNotifications {
	t.Helper()
	got := &sentNotifications{}
	channel := mocks.NewNotificationChannel(t)
	channel.On("Kind").Return(domain.NotificationChannelDesktop).Maybe()
	channel.On("Send", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		got.mu.Lock()
		defer got.mu.Unlock()
		got.sent = append(got.sent, args.Get(1).(domain.Notification))
	}).Return(nil).Maybe()
	notifier := notify.NewService()
	notifier.Register(channel)
	svc.notifier = notifier
	return got
}

func TestARollbackProposalNotifiesARollbackSuggestionWithAnApproveAction(t *testing.T) {
	f := newRollbackFixture()
	sent := withNotifier(t, f.svc)
	repositoryID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, uuid.New(), domain.DeliveryOnMerge, false), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "error rate up 2 points since the deploy")
	require.ErrorIs(t, err, domain.ErrRollbackNeedsHuman)

	got := sent.all()
	require.Len(t, got, 1)
	n := got[0]
	assert.Equal(t, domain.NotificationRollbackSuggested, n.Kind)
	assert.Equal(t, domain.NotificationSourceRelease, n.Source)
	assert.Equal(t, created.ID.String(), n.SourceID)
	assert.Equal(t, "/operations/changes?release="+created.ID.String(), n.Link)
	assert.Contains(t, n.Body, "error rate up 2 points")
	require.Len(t, n.Actions, 1)
	assert.Equal(t, domain.NotificationActionApproveRollback, n.Actions[0].ID)
	assert.Equal(t, approveRollbackLabel, n.Actions[0].Label)
}

func TestARepeatedProposalNotifiesOncePerCooldown(t *testing.T) {
	f := newRollbackFixture()
	sent := withNotifier(t, f.svc)
	repositoryID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, uuid.New(), domain.DeliveryOnMerge, false), []uuid.UUID{task.ID})
	require.NoError(t, err)

	for range 3 {
		_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackVerifyFailed, "smoke failed")
		require.ErrorIs(t, err, domain.ErrRollbackNeedsHuman)
	}
	assert.Len(t, sent.all(), 1)

	f.clock.Advance(31 * time.Minute)
	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackVerifyFailed, "smoke failed")
	require.ErrorIs(t, err, domain.ErrRollbackNeedsHuman)
	assert.Len(t, sent.all(), 2, "past the cooldown the proposal is told again")
}

func TestAnAutomaticRollbackNotifiesThatTheReleaseWasRolledBack(t *testing.T) {
	f := newRollbackFixture()
	sent := withNotifier(t, f.svc)
	repositoryID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, uuid.New(), domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "p95 latency doubled")
	require.NoError(t, err)
	require.Equal(t, domain.ReleaseRollingBack, updated.Status)

	got := sent.all()
	require.Len(t, got, 1)
	assert.Equal(t, domain.NotificationReleaseRolledBack, got[0].Kind)
	assert.Equal(t, domain.ReleaseLink(created.ID), got[0].Link)
	assert.Empty(t, got[0].Actions)
}

func TestARollbackAHumanStartedIsNotNotified(t *testing.T) {
	f := newRollbackFixture()
	sent := withNotifier(t, f.svc)
	repositoryID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, uuid.New(), domain.DeliveryOnMerge, false), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorHuman, domain.RollbackManual, "by hand")
	require.NoError(t, err)

	assert.Empty(t, sent.all())
}

func TestARollbackThatFailsIsNotToldAsRolledBack(t *testing.T) {
	f := newRollbackFixture()
	sent := withNotifier(t, f.svc)
	f.reverter.err = assert.AnError
	repositoryID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, uuid.New(), domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "errors")
	require.Error(t, err)

	assert.Empty(t, sent.all())
}
