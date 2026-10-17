package opsquery_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

func (s *OpsQuerySuite) withBoundLogs() *mocks.BoundLogReader {
	r := mocks.NewBoundLogReader(s.T())
	s.svc.SetBoundLogs(r)
	return r
}

func logsBinding(component uuid.UUID) domain.SignalBinding {
	return domain.SignalBinding{
		ID: uuid.New(), ComponentID: component, Environment: domain.EnvironmentProduction, Signal: domain.SignalLogsLines,
		DataSourceID: uuid.New(), RecipeID: "loki-service", Params: map[string]string{"service": "api"}, Status: domain.SignalBindingConfirmed,
	}
}

func (s *OpsQuerySuite) TestLogsReadsABoundComponentThroughItsBindingInsteadOfItsEnvironments() {
	viaLoki, cloudOnly := s.env(domain.EnvironmentProduction), s.env(domain.EnvironmentProduction)
	s.seed([]domain.ComponentEnvironment{viaLoki, cloudOnly})
	r := s.withBoundLogs()
	b := logsBinding(viaLoki.ComponentID)
	since, until := s.now.Add(-time.Hour), s.now
	r.On("LogBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBinding{b}, nil).Once()
	r.On("ReadLogs", mock.Anything, b, domain.LogQuery{Since: since, Until: until, MinSeverity: domain.LogError, Text: "boom", Limit: 50}).
		Return(domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{s.entry(1, "from loki")}}, nil).Once()
	s.runtime.On("Logs", mock.Anything, cloudOnly.ID, domain.RuntimeLogQuery{Since: since, Until: until, MinSeverity: domain.LogError, Text: "boom", Limit: 50}).
		Return(domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{s.entry(2, "from the cloud")}}, nil).Once()

	out, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{
		Environment: domain.EnvironmentProduction, Since: since, Until: until, MinSeverity: domain.LogError, Text: "boom", Limit: 50,
	})

	s.Require().NoError(err)
	s.Empty(out.Unavailable)
	s.Require().Len(out.Entries, 2)
	loki, cloud := out.Entries[0], out.Entries[1]
	s.Equal("from loki", loki.Message)
	s.Equal(viaLoki.ComponentID, loki.ComponentID)
	s.Equal(uuid.Nil, loki.EnvironmentID)
	s.Equal(b.DataSourceID, *loki.DataSourceID)
	s.Equal(cloudOnly.ID, cloud.EnvironmentID)
	s.Nil(cloud.DataSourceID)
}

func (s *OpsQuerySuite) TestLogsReportsAFailedBindingWithItsSource() {
	tests := []struct {
		name string
		err  error
		want domain.OpsUnavailableCode
	}{
		{name: "source deleted", err: fmt.Errorf("gone: %w", port.ErrNotFound), want: domain.OpsNotConnected},
		{name: "timed out", err: context.DeadlineExceeded, want: domain.OpsTimeout},
		{name: "source failed", err: errors.New("Loki logs: HTTP 503"), want: domain.OpsProviderError},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.reset()
			s.seed(nil)
			r := s.withBoundLogs()
			b := logsBinding(uuid.New())
			r.On("LogBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBinding{b}, nil).Once()
			r.On("ReadLogs", mock.Anything, b, mock.Anything).Return(domain.RuntimeLogPage{}, tt.err).Once()

			out, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{Environment: domain.EnvironmentProduction})

			s.Require().NoError(err)
			s.Require().Len(out.Unavailable, 1)
			u := out.Unavailable[0]
			s.Equal(tt.want, u.Code)
			s.Equal(b.ComponentID, u.ComponentID)
			s.Equal(uuid.Nil, u.EnvironmentID)
			s.Equal(b.DataSourceID, *u.DataSourceID)
		})
	}
}

func (s *OpsQuerySuite) TestLogsCountsBindingsTowardTheEnvironmentLimit() {
	envs := make([]domain.ComponentEnvironment, opsquery.MaxLogEnvironments)
	for i := range envs {
		envs[i] = s.env(domain.EnvironmentProduction)
	}
	s.seed(envs)
	r := s.withBoundLogs()
	r.On("LogBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBinding{logsBinding(uuid.New())}, nil).Once()

	_, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{Environment: domain.EnvironmentProduction})

	var tooMany *opsquery.TooManyEnvironmentsError
	s.Require().ErrorAs(err, &tooMany)
	s.Equal(opsquery.MaxLogEnvironments+1, tooMany.Matched)
}

func (s *OpsQuerySuite) TestLogsReadsOnlyTheBindingsOfTheComponentsAskedFor() {
	s.seed(nil)
	r := s.withBoundLogs()
	wanted, other := logsBinding(uuid.New()), logsBinding(uuid.New())
	r.On("LogBindings", mock.Anything, domain.EnvironmentStaging).Return([]domain.SignalBinding{wanted, other}, nil).Once()
	r.On("ReadLogs", mock.Anything, wanted, mock.Anything).Return(domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{s.entry(1, "x")}}, nil).Once()

	out, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{Environment: domain.EnvironmentStaging, ComponentIDs: []uuid.UUID{wanted.ComponentID}})

	s.Require().NoError(err)
	s.Require().Len(out.Entries, 1)
	s.Equal(wanted.ComponentID, out.Entries[0].ComponentID)
}
