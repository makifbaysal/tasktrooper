package prodops_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type IncidentNotifySuite struct {
	suite.Suite
	ctx       context.Context
	incidents *fakeIncidents
	svc       *prodops.Service
	repoID    uuid.UUID

	mu   sync.Mutex
	sent []domain.Notification
}

func TestIncidentNotifySuite(t *testing.T) { suite.Run(t, new(IncidentNotifySuite)) }

func (s *IncidentNotifySuite) SetupTest() {
	s.ctx = context.Background()
	s.incidents = newFakeIncidents()
	s.repoID = uuid.New()
	s.sent = nil

	channel := mocks.NewNotificationChannel(s.T())
	channel.On("Kind").Return(domain.NotificationChannelDesktop).Maybe()
	channel.On("Send", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.sent = append(s.sent, args.Get(1).(domain.Notification))
	}).Return(nil).Maybe()
	notifier := notify.NewService()
	notifier.Register(channel)

	s.svc = prodops.NewService(prodops.Deps{Incidents: s.incidents, Notifier: notifier})
}

func (s *IncidentNotifySuite) notifications() []domain.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.Notification(nil), s.sent...)
}

func (s *IncidentNotifySuite) alert(severity domain.IncidentSeverity) domain.IncidentInput {
	return domain.IncidentInput{
		RepositoryID: s.repoID,
		Env:          domain.DeployEnvProd,
		Source:       domain.IncidentSourceWebhook,
		Fingerprint:  "fp-notify",
		Title:        "api down",
		Severity:     severity,
	}
}

func (s *IncidentNotifySuite) TestANewHighIncidentNotifiesWithALinkToIt() {
	incident, err := s.svc.Ingest(s.ctx, s.alert(domain.IncidentSeverityHigh))
	s.Require().NoError(err)

	sent := s.notifications()
	s.Require().Len(sent, 1)
	s.Equal("PROD · api down", sent[0].Title)
	s.NotEmpty(sent[0].Body)
	s.Equal(domain.IncidentSeverityHigh, sent[0].Severity)
	s.Equal(domain.NotificationSourceIncident, sent[0].Source)
	s.Equal(incident.ID.String(), sent[0].SourceID)
	s.Equal("/operations/issues?id="+incident.ID.String(), sent[0].Link)

	stored, err := s.incidents.Get(s.ctx, incident.ID)
	s.Require().NoError(err)
	kinds := make([]string, 0, len(stored.Events))
	for _, e := range stored.Events {
		kinds = append(kinds, e.Kind)
	}
	s.Contains(kinds, domain.IncidentEventNotified)
}

func (s *IncidentNotifySuite) TestAnIncidentBelowTheFloorIsNotNotified() {
	_, err := s.svc.Ingest(s.ctx, s.alert(domain.IncidentSeverityMedium))
	s.Require().NoError(err)

	s.Empty(s.notifications())
}

func (s *IncidentNotifySuite) TestACriticalRecurrenceInsideTheCooldownNotifiesOnce() {
	for range 3 {
		_, err := s.svc.Ingest(s.ctx, s.alert(domain.IncidentSeverityCritical))
		s.Require().NoError(err)
	}

	s.Len(s.notifications(), 1)
}

func (s *IncidentNotifySuite) TestRecoveryOfANotifiedIncidentNotifies() {
	_, err := s.svc.Ingest(s.ctx, s.alert(domain.IncidentSeverityHigh))
	s.Require().NoError(err)

	recovery := s.alert(domain.IncidentSeverityHigh)
	recovery.Resolved = true
	resolved, err := s.svc.Ingest(s.ctx, recovery)
	s.Require().NoError(err)

	sent := s.notifications()
	s.Require().Len(sent, 2)
	s.Equal("Recovered · prod", sent[1].Title)
	s.Equal(resolved.ID.String(), sent[1].SourceID)
}

func (s *IncidentNotifySuite) TestAFlappingAlertNotifiesOnceAnOutageAndOnceARecoveryPerCooldown() {
	clock := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	prodops.SetClock(s.svc, func() time.Time { return clock })
	flap := func() {
		opened, err := s.svc.Ingest(s.ctx, s.alert(domain.IncidentSeverityHigh))
		s.Require().NoError(err)
		recovery := s.alert(domain.IncidentSeverityHigh)
		recovery.Resolved = true
		resolved, err := s.svc.Ingest(s.ctx, recovery)
		s.Require().NoError(err)
		s.Require().Equal(opened.ID, resolved.ID)
		clock = clock.Add(5 * time.Minute)
	}

	for range 4 {
		flap()
	}
	sent := s.notifications()
	s.Require().Len(sent, 2, "four flaps in twenty minutes: one outage, one recovery")
	s.Equal("PROD · api down", sent[0].Title)
	s.Equal("Recovered · prod", sent[1].Title)
	s.Len(s.incidents.incidents, 4, "every flap is its own incident")

	clock = clock.Add(31 * time.Minute)
	flap()
	s.Len(s.notifications(), 4, "past the cooldown the next flap is told again")
}

func (s *IncidentNotifySuite) TestOnlyAProposalSomeoneElseWroteIsNotified() {
	incident, err := s.svc.Ingest(s.ctx, s.alert(domain.IncidentSeverityHigh))
	s.Require().NoError(err)
	remedy := domain.Remedy{Kind: domain.RemedyKindConfig, Summary: "raise max_conns to 200", Confidence: 80}

	_, err = s.svc.ProposeRemedy(s.ctx, incident.ID, remedy, domain.RemedyAuthorHuman)
	s.Require().NoError(err)
	s.Len(s.notifications(), 1)

	_, err = s.svc.ProposeRemedy(s.ctx, incident.ID, remedy, domain.RemedyAuthorAgent)
	s.Require().NoError(err)
	sent := s.notifications()
	s.Require().Len(sent, 2)
	s.Equal("Fix proposed · prod", sent[1].Title)
	s.Equal("raise max_conns to 200", sent[1].Body)
}
