package opsquery_test

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

func errorsBinding(component uuid.UUID) domain.SignalBindingView {
	return domain.SignalBindingView{
		SignalBinding: domain.SignalBinding{
			ID: uuid.New(), ComponentID: component, Environment: domain.EnvironmentProduction, Signal: domain.SignalErrorsIssues,
			DataSourceID: uuid.New(), RecipeID: "sentry-mobile", Params: map[string]string{"project": "ios-app"}, Status: domain.SignalBindingConfirmed,
		},
		DataSourceKind: domain.DataSourceKindSentry,
	}
}

func (s *OpsQuerySuite) TestErrorsAddsTheGroupsOfEachErrorsBindingBesideTheCloudsOwn() {
	cloudEnv := s.env(domain.EnvironmentProduction)
	mobile := domain.Component{ID: uuid.New(), RepositoryID: uuid.New(), Status: domain.ComponentStatusActive}
	other := domain.Component{ID: uuid.New(), RepositoryID: uuid.New(), Status: domain.ComponentStatusActive}
	s.seed([]domain.ComponentEnvironment{cloudEnv}, mobile, other)
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{mobile, other}, nil).Once()
	s.supportsErrors(cloudEnv)
	s.errorsFor(cloudEnv, domain.RuntimeErrorGroup{Fingerprint: "cloud", Message: "cloud boom", Count: 1, LastSeen: s.now.Add(-time.Hour)})

	reads, fails := errorsBinding(mobile.ID), errorsBinding(other.ID)
	bound := mocks.NewBoundErrorReader(s.T())
	bound.On("ErrorBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBindingView{reads, fails}, nil).Once()
	since := s.now.Add(-opsquery.DefaultErrorsWindow)
	bound.On("ReadErrors", mock.Anything, reads.SignalBinding, domain.ErrorsQuery{Since: since, Until: s.now, Limit: 100}).
		Return([]domain.RuntimeErrorGroup{{Fingerprint: "101", Message: "NSInvalidArgumentException", Count: 250, New: true, ExternalURL: "https://acme.sentry.io/issues/101/"}}, nil).Once()
	bound.On("ReadErrors", mock.Anything, fails.SignalBinding, mock.Anything).
		Return(nil, errors.New("Sentry list issues: rate limited (HTTP 429); try again in a minute")).Once()
	s.svc.SetBoundErrors(bound)

	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.Require().Len(out.Groups, 2)
	g := out.Groups[0]
	s.Equal("101", g.Fingerprint, "a new group sorts first")
	s.Equal(mobile.ID, g.ComponentID)
	s.Equal(mobile.RepositoryID, g.RepositoryID)
	s.Equal(uuid.Nil, g.EnvironmentID)
	s.Require().NotNil(g.DataSourceID)
	s.Equal(reads.DataSourceID, *g.DataSourceID)
	s.Equal(domain.CloudProviderKind("sentry"), g.Provider)
	s.Equal(cloudEnv.ID, out.Groups[1].EnvironmentID)
	s.Nil(out.Groups[1].DataSourceID)

	s.Require().Len(out.Unavailable, 1)
	u := out.Unavailable[0]
	s.Equal(other.ID, u.ComponentID)
	s.Equal(fails.DataSourceID, *u.DataSourceID)
	s.Equal(domain.OpsProviderError, u.Code)
	s.Contains(u.Message, "rate limited")
}

func (s *OpsQuerySuite) TestErrorsReadsOnlyTheErrorsBindingsOfTheAskedComponents() {
	wanted := uuid.New()
	s.seed(nil, domain.Component{ID: wanted, Status: domain.ComponentStatusActive})
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{{ID: wanted}}, nil).Once()
	reads := errorsBinding(wanted)
	bound := mocks.NewBoundErrorReader(s.T())
	bound.On("ErrorBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBindingView{reads, errorsBinding(uuid.New())}, nil).Once()
	bound.On("ReadErrors", mock.Anything, reads.SignalBinding, mock.Anything).Return([]domain.RuntimeErrorGroup{{Fingerprint: "1"}}, nil).Once()
	s.svc.SetBoundErrors(bound)

	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction, ComponentIDs: []uuid.UUID{wanted}})
	s.Require().NoError(err)
	s.Require().Len(out.Groups, 1)
	s.Equal(wanted, out.Groups[0].ComponentID)
}

func (s *OpsQuerySuite) TestErrorsStandsWhenTheErrorsBindingsCannotBeListed() {
	cloudEnv := s.env(domain.EnvironmentProduction)
	s.seed([]domain.ComponentEnvironment{cloudEnv})
	s.supportsErrors(cloudEnv)
	s.errorsFor(cloudEnv, groupFor(cloudEnv))
	bound := mocks.NewBoundErrorReader(s.T())
	bound.On("ErrorBindings", mock.Anything, domain.EnvironmentProduction).Return(nil, errors.New("store down")).Once()
	s.svc.SetBoundErrors(bound)

	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.Equal([]uuid.UUID{cloudEnv.ID}, groupEnvironments(out))
	s.Empty(out.Unavailable)
}

func (s *OpsQuerySuite) TestErrorsSaysTruncatedWhenABindingFillsItsCap() {
	mobile := domain.Component{ID: uuid.New(), RepositoryID: uuid.New(), Status: domain.ComponentStatusActive}
	s.seed(nil, mobile)
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{mobile}, nil).Once()
	reads := errorsBinding(mobile.ID)
	full := make([]domain.RuntimeErrorGroup, 100)
	for i := range full {
		full[i] = domain.RuntimeErrorGroup{Fingerprint: fmt.Sprint(i), Count: 1}
	}
	bound := mocks.NewBoundErrorReader(s.T())
	bound.On("ErrorBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBindingView{reads}, nil).Once()
	bound.On("ReadErrors", mock.Anything, reads.SignalBinding, mock.Anything).Return(full, nil).Once()
	s.svc.SetBoundErrors(bound)

	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.True(out.Truncated, "a binding answering its whole cap may hold more")
}
