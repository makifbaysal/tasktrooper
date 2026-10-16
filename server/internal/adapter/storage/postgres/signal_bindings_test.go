package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type SignalBindingStoreSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	pg        *database.Embedded
	pool      *pgxpool.Pool
	store     *postgres.SignalBindingStore
	sources   *postgres.DataSourceStore
	models    *postgres.ProjectModelStore
	repos     *postgres.RepositoryStore
	component domain.Component
	source    domain.DataSource
}

func TestSignalBindingStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(SignalBindingStoreSuite))
}

func (s *SignalBindingStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewSignalBindingStore(db)
	s.sources = postgres.NewDataSourceStore(db)
	s.models = postgres.NewProjectModelStore(db)
	s.repos = postgres.NewRepositoryStore(db)
}

func (s *SignalBindingStoreSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *SignalBindingStoreSuite) SetupTest() {
	suffix := uuid.NewString()
	repo, err := s.repos.Create(s.ctx, "signals-repo-"+suffix, "", "/tmp/signals-repo-"+suffix, "", "")
	s.Require().NoError(err)
	s.component, err = s.models.SaveComponent(s.ctx, domain.Component{RepositoryID: repo.ID, Path: "api"})
	s.Require().NoError(err)
	s.source = s.newSource()
}

func (s *SignalBindingStoreSuite) newSource() domain.DataSource {
	ds, err := s.sources.CreateDataSource(s.ctx, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Name: "prom", Config: map[string]string{"url": "http://localhost:9090"},
	}, nil)
	s.Require().NoError(err)
	return ds
}

func (s *SignalBindingStoreSuite) suggestion(signal string, params map[string]string) domain.SignalBinding {
	return domain.SignalBinding{Signal: signal, DataSourceID: s.source.ID, RecipeID: "otel-http", Params: params}
}

func (s *SignalBindingStoreSuite) suggest(env domain.DeployEnvironment, list ...domain.SignalBinding) []domain.SignalBinding {
	saved, err := s.store.ReplaceDetectedSuggestions(s.ctx, s.component.ID, env, []uuid.UUID{s.source.ID}, list)
	s.Require().NoError(err)
	return saved
}

func (s *SignalBindingStoreSuite) TestSuggestionsRoundTripAndReplaceOnlyPendingDetectedRows() {
	first := s.suggest(domain.EnvironmentProduction,
		s.suggestion("http.requests", map[string]string{"job": "api"}),
		s.suggestion("http.requests", map[string]string{"job": "api-v2"}),
		s.suggestion("http.latency", nil),
	)
	s.Require().Len(first, 3)
	s.Equal(domain.SignalBindingSuggested, first[0].Status)
	s.Equal(domain.SignalBindingDetected, first[0].Source)
	s.Equal(map[string]string{"job": "api"}, first[0].Params)
	s.Equal(map[string]string{}, first[2].Params)
	s.Equal(domain.EnvironmentProduction, first[0].Environment)
	s.Equal(s.component.ID, first[0].ComponentID)

	_, err := s.store.DismissSignalBinding(s.ctx, first[1].ID)
	s.Require().NoError(err)
	confirmed, err := s.store.ConfirmSignalBinding(s.ctx, first[2].ID)
	s.Require().NoError(err)
	s.Equal(domain.SignalBindingConfirmed, confirmed.Status)
	s.suggest(domain.EnvironmentStaging, s.suggestion("http.requests", map[string]string{"job": "api-staging"}))

	s.suggest(domain.EnvironmentProduction, s.suggestion("http.error_ratio", map[string]string{"job": "api"}))

	prod, err := s.store.ListSignalBindings(s.ctx, s.component.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	byStatus := map[domain.SignalBindingStatus][]string{}
	for _, b := range prod {
		byStatus[b.Status] = append(byStatus[b.Status], b.Signal+"/"+b.Params["job"])
	}
	s.Equal(map[domain.SignalBindingStatus][]string{
		domain.SignalBindingDismissed: {"http.requests/api-v2"},
		domain.SignalBindingConfirmed: {"http.latency/"},
		domain.SignalBindingSuggested: {"http.error_ratio/api"},
	}, byStatus)

	staging, err := s.store.ListSignalBindings(s.ctx, s.component.ID, domain.EnvironmentStaging)
	s.Require().NoError(err)
	s.Len(staging, 1)
}

func (s *SignalBindingStoreSuite) TestPutReplacesTheConfirmedBindingInPlaceAndDropsPendingSuggestions() {
	saved := s.suggest(domain.EnvironmentProduction,
		s.suggestion("http.requests", map[string]string{"job": "api"}),
		s.suggestion("http.latency", map[string]string{"job": "api"}),
	)
	first, err := s.store.PutConfirmedSignalBinding(s.ctx, domain.SignalBinding{
		ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.requests",
		DataSourceID: s.source.ID, Query: "sum(rate(x[5m]))", Source: domain.SignalBindingHuman,
	})
	s.Require().NoError(err)
	s.Equal(domain.SignalBindingConfirmed, first.Status)
	s.Equal(domain.SignalBindingHuman, first.Source)

	other := s.newSource()
	second, err := s.store.PutConfirmedSignalBinding(s.ctx, domain.SignalBinding{
		ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.requests",
		DataSourceID: other.ID, RecipeID: "otel-http", Params: map[string]string{"job": "edge"}, Source: domain.SignalBindingHuman,
	})
	s.Require().NoError(err)
	s.Equal(first.ID, second.ID)
	s.True(first.CreatedAt.Equal(second.CreatedAt))
	s.Equal(other.ID, second.DataSourceID)
	s.Equal("", second.Query)
	s.Equal(map[string]string{"job": "edge"}, second.Params)

	all, err := s.store.ListSignalBindings(s.ctx, s.component.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Require().Len(all, 2)
	ids := []uuid.UUID{all[0].ID, all[1].ID}
	s.Contains(ids, saved[1].ID)
	s.NotContains(ids, saved[0].ID)
}

func (s *SignalBindingStoreSuite) TestConfirmReplacesTheSlotsConfirmedBindingAndIsIdempotent() {
	human, err := s.store.PutConfirmedSignalBinding(s.ctx, domain.SignalBinding{
		ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.requests",
		DataSourceID: s.source.ID, Query: "up", Source: domain.SignalBindingHuman,
	})
	s.Require().NoError(err)
	saved := s.suggest(domain.EnvironmentProduction,
		s.suggestion("http.requests", map[string]string{"job": "a"}),
		s.suggestion("http.requests", map[string]string{"job": "b"}),
	)

	confirmed, err := s.store.ConfirmSignalBinding(s.ctx, saved[0].ID)
	s.Require().NoError(err)
	s.Equal(saved[0].ID, confirmed.ID)
	s.Equal(domain.SignalBindingDetected, confirmed.Source)
	again, err := s.store.ConfirmSignalBinding(s.ctx, saved[0].ID)
	s.Require().NoError(err)
	s.Equal(confirmed.ID, again.ID)

	_, err = s.store.GetSignalBinding(s.ctx, human.ID)
	s.ErrorIs(err, port.ErrNotFound)
	_, err = s.store.GetSignalBinding(s.ctx, saved[1].ID)
	s.ErrorIs(err, port.ErrNotFound)

	listed, err := s.store.ListConfirmedSignalBindings(s.ctx, domain.EnvironmentProduction)
	s.Require().NoError(err)
	var mine []domain.SignalBinding
	for _, b := range listed {
		if b.ComponentID == s.component.ID {
			mine = append(mine, b)
		}
	}
	s.Require().Len(mine, 1)
	s.Equal(saved[0].ID, mine[0].ID)
}

func (s *SignalBindingStoreSuite) TestUnknownIDsAnswerNotFound() {
	_, err := s.store.GetSignalBinding(s.ctx, uuid.New())
	s.ErrorIs(err, port.ErrNotFound)
	_, err = s.store.ConfirmSignalBinding(s.ctx, uuid.New())
	s.ErrorIs(err, port.ErrNotFound)
	_, err = s.store.DismissSignalBinding(s.ctx, uuid.New())
	s.ErrorIs(err, port.ErrNotFound)
	s.ErrorIs(s.store.DeleteSignalBinding(s.ctx, uuid.New()), port.ErrNotFound)
}

func (s *SignalBindingStoreSuite) TestDeletingTheDataSourceDeletesItsBindings() {
	saved := s.suggest(domain.EnvironmentProduction, s.suggestion("http.requests", nil))
	s.Require().NoError(s.sources.DeleteDataSource(s.ctx, s.source.ID))
	_, err := s.store.GetSignalBinding(s.ctx, saved[0].ID)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *SignalBindingStoreSuite) TestDeleteRemovesOneBinding() {
	saved := s.suggest(domain.EnvironmentProduction, s.suggestion("http.requests", nil), s.suggestion("http.latency", nil))
	s.Require().NoError(s.store.DeleteSignalBinding(s.ctx, saved[0].ID))
	left, err := s.store.ListSignalBindings(s.ctx, s.component.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Require().Len(left, 1)
	s.Equal(saved[1].ID, left[0].ID)
}

func (s *SignalBindingStoreSuite) pendingIDs(env domain.DeployEnvironment) map[string]uuid.UUID {
	all, err := s.store.ListSignalBindings(s.ctx, s.component.ID, env)
	s.Require().NoError(err)
	out := map[string]uuid.UUID{}
	for _, b := range all {
		if b.Status == domain.SignalBindingSuggested {
			out[b.Signal+"/"+b.Params["job"]] = b.ID
		}
	}
	return out
}

func (s *SignalBindingStoreSuite) TestReplaceKeepsUnchangedSuggestionsAndTheRowsOfSourcesThatDidNotAnswer() {
	other := s.newSource()
	fromOther := domain.SignalBinding{Signal: "http.requests", DataSourceID: other.ID, RecipeID: "otel-http", Params: map[string]string{"job": "edge"}}
	_, err := s.store.ReplaceDetectedSuggestions(s.ctx, s.component.ID, domain.EnvironmentProduction, []uuid.UUID{s.source.ID, other.ID}, []domain.SignalBinding{
		s.suggestion("http.requests", map[string]string{"job": "api"}),
		fromOther,
		s.suggestion("http.latency", map[string]string{"job": "api"}),
	})
	s.Require().NoError(err)
	before := s.pendingIDs(domain.EnvironmentProduction)
	s.Require().Len(before, 3)

	saved, err := s.store.ReplaceDetectedSuggestions(s.ctx, s.component.ID, domain.EnvironmentProduction, []uuid.UUID{s.source.ID}, []domain.SignalBinding{
		s.suggestion("http.requests", map[string]string{"job": "api"}),
		s.suggestion("http.error_ratio", map[string]string{"job": "api"}),
	})
	s.Require().NoError(err)

	after := s.pendingIDs(domain.EnvironmentProduction)
	s.Equal(before["http.requests/api"], after["http.requests/api"])
	s.Equal(before["http.requests/edge"], after["http.requests/edge"])
	s.NotContains(after, "http.latency/api")
	s.Contains(after, "http.error_ratio/api")
	s.Len(after, 3)
	s.Len(saved, 3)
}

func (s *SignalBindingStoreSuite) TestDismissTouchesOnlyASuggestion() {
	saved := s.suggest(domain.EnvironmentProduction, s.suggestion("http.requests", nil))
	confirmed, err := s.store.PutConfirmedSignalBinding(s.ctx, domain.SignalBinding{
		ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.latency",
		DataSourceID: s.source.ID, Query: "up", Source: domain.SignalBindingHuman,
	})
	s.Require().NoError(err)

	got, err := s.store.DismissSignalBinding(s.ctx, confirmed.ID)
	s.Require().NoError(err)
	s.Equal(domain.SignalBindingConfirmed, got.Status)
	still, err := s.store.GetSignalBinding(s.ctx, confirmed.ID)
	s.Require().NoError(err)
	s.Equal(domain.SignalBindingConfirmed, still.Status)

	got, err = s.store.DismissSignalBinding(s.ctx, saved[0].ID)
	s.Require().NoError(err)
	s.Equal(domain.SignalBindingDismissed, got.Status)
	got, err = s.store.DismissSignalBinding(s.ctx, saved[0].ID)
	s.Require().NoError(err)
	s.Equal(domain.SignalBindingDismissed, got.Status)
}

func (s *SignalBindingStoreSuite) TestConcurrentConfirmsAndPutsOfOneSlotLeaveOneConfirmedBinding() {
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()
	put := domain.SignalBinding{
		ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.requests",
		DataSourceID: s.source.ID, Query: "up", Source: domain.SignalBindingHuman,
	}
	for round := range 8 {
		var list []domain.SignalBinding
		for job := range 6 {
			list = append(list, s.suggestion("http.requests", map[string]string{"job": fmt.Sprintf("%d-%d", round, job)}))
		}
		saved := s.suggest(domain.EnvironmentProduction, list...)

		start := make(chan struct{})
		errs := make(chan error, len(saved)+2)
		var wg sync.WaitGroup
		for _, b := range saved {
			wg.Add(1)
			go func(id uuid.UUID) {
				defer wg.Done()
				<-start
				if _, err := s.store.ConfirmSignalBinding(ctx, id); err != nil && !errors.Is(err, port.ErrNotFound) {
					errs <- err
				}
			}(b.ID)
		}
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := s.store.PutConfirmedSignalBinding(ctx, put); err != nil {
					errs <- err
				}
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			s.NoError(err)
		}
	}

	all, err := s.store.ListSignalBindings(s.ctx, s.component.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	confirmed := 0
	for _, b := range all {
		s.NotEqual(domain.SignalBindingSuggested, b.Status)
		if b.Status == domain.SignalBindingConfirmed {
			confirmed++
		}
	}
	s.Equal(1, confirmed)
}
