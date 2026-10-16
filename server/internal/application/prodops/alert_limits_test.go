package prodops_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type AlertLimitSuite struct {
	suite.Suite
	ctx       context.Context
	store     *mocks.AlertIngestTokenStore
	incidents *fakeIncidents
	clock     time.Time
	tok       domain.AlertIngestToken
	plaintext string
}

func TestAlertLimitSuite(t *testing.T) { suite.Run(t, new(AlertLimitSuite)) }

func (s *AlertLimitSuite) SetupTest() {
	s.ctx = context.Background()
	s.store = mocks.NewAlertIngestTokenStore(s.T())
	s.incidents = newFakeIncidents()
	s.clock = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	s.tok = domain.AlertIngestToken{ID: uuid.New(), RepositoryID: uuid.New(), DefaultEnv: domain.DeployEnvProd}
	s.plaintext = "tt_alert_" + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
}

func (s *AlertLimitSuite) service(limits domain.AlertIngestLimits) *prodops.Service {
	svc := prodops.NewService(prodops.Deps{Incidents: s.incidents, AlertTokens: s.store, AlertLimits: limits})
	prodops.SetClock(svc, func() time.Time { return s.clock })
	return svc
}

func (s *AlertLimitSuite) alert(title string) domain.IncidentInput {
	return domain.IncidentInput{Title: title, Severity: domain.IncidentSeverityMedium}
}

func (s *AlertLimitSuite) TestAdmissionSpendsTheTokensRequestBudget() {
	svc := s.service(domain.AlertIngestLimits{RequestsPerMinute: 60, Burst: 3})
	s.store.On("FindByHash", mock.Anything, sha256Hex(s.plaintext)).Return(s.tok, nil)

	for i := range 3 {
		_, err := svc.AdmitAlertToken(s.ctx, s.plaintext)
		s.Require().NoError(err, "request %d is inside the burst", i+1)
	}
	_, err := svc.AdmitAlertToken(s.ctx, s.plaintext)
	s.ErrorIs(err, prodops.ErrAlertRateLimited)

	s.clock = s.clock.Add(time.Second)
	_, err = svc.AdmitAlertToken(s.ctx, s.plaintext)
	s.NoError(err, "60 a minute refills one a second")
}

func (s *AlertLimitSuite) TestZeroLimitsFallBackToTheDefaultsNotToUnlimited() {
	svc := s.service(domain.AlertIngestLimits{})
	s.store.On("FindByHash", mock.Anything, sha256Hex(s.plaintext)).Return(s.tok, nil)

	for range prodops.DefaultAlertBurst {
		_, err := svc.AdmitAlertToken(s.ctx, s.plaintext)
		s.Require().NoError(err)
	}
	_, err := svc.AdmitAlertToken(s.ctx, s.plaintext)
	s.ErrorIs(err, prodops.ErrAlertRateLimited)
}

func (s *AlertLimitSuite) TestNewIncidentsAreCappedButOpenOnesStillCount() {
	svc := s.service(domain.AlertIngestLimits{NewIncidentsPerHour: 2})

	first, err := svc.IngestWithAlertToken(s.ctx, s.tok, s.alert("disk full"))
	s.Require().NoError(err)
	_, err = svc.IngestWithAlertToken(s.ctx, s.tok, s.alert("cpu hot"))
	s.Require().NoError(err)

	_, err = svc.IngestWithAlertToken(s.ctx, s.tok, s.alert("a third, new alert"))
	s.ErrorIs(err, prodops.ErrAlertRateLimited)

	again, err := svc.IngestWithAlertToken(s.ctx, s.tok, s.alert("disk full"))
	s.Require().NoError(err, "a recurrence of an open incident is not a new incident")
	s.Equal(first.ID, again.ID)
	s.Equal(2, again.Occurrences)

	recovery := s.alert("cpu hot")
	recovery.Resolved = true
	resolved, err := svc.IngestWithAlertToken(s.ctx, s.tok, recovery)
	s.Require().NoError(err, "a recovery is never refused")
	s.Equal(domain.IncidentStatusResolved, resolved.Status)

	s.Len(s.incidents.incidents, 2)
}

func (s *AlertLimitSuite) TestTheNewIncidentWindowRollsAndIsPerToken() {
	svc := s.service(domain.AlertIngestLimits{NewIncidentsPerHour: 1})
	other := domain.AlertIngestToken{ID: uuid.New(), RepositoryID: s.tok.RepositoryID}

	_, err := svc.IngestWithAlertToken(s.ctx, s.tok, s.alert("one"))
	s.Require().NoError(err)
	_, err = svc.IngestWithAlertToken(s.ctx, s.tok, s.alert("two"))
	s.ErrorIs(err, prodops.ErrAlertRateLimited)

	_, err = svc.IngestWithAlertToken(s.ctx, other, s.alert("two"))
	s.NoError(err, "another token has its own allowance")

	s.clock = s.clock.Add(61 * time.Minute)
	_, err = svc.IngestWithAlertToken(s.ctx, s.tok, s.alert("three"))
	s.NoError(err, "an hour later the first one has left the window")
}

func (s *AlertLimitSuite) TestARefusedNewIncidentDoesNotSpendTheAllowance() {
	svc := s.service(domain.AlertIngestLimits{NewIncidentsPerHour: 3})
	for i := range 3 {
		_, err := svc.IngestWithAlertToken(s.ctx, s.tok, s.alert(fmt.Sprintf("alert %d", i)))
		s.Require().NoError(err)
	}
	for range 5 {
		_, err := svc.IngestWithAlertToken(s.ctx, s.tok, s.alert("over the cap"))
		s.ErrorIs(err, prodops.ErrAlertRateLimited)
	}

	s.clock = s.clock.Add(61 * time.Minute)
	for i := range 3 {
		_, err := svc.IngestWithAlertToken(s.ctx, s.tok, s.alert(fmt.Sprintf("next hour %d", i)))
		s.Require().NoError(err)
	}
}
