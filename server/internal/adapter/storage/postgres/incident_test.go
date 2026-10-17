package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// IncidentStoreSuite covers the dedupe contract, which lives entirely in SQL:
// a partial unique index plus an ON CONFLICT that must fold recurrences into
// the live incident while letting a resolved one recur as a new row.
type IncidentStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.IncidentStore
	repoID uuid.UUID
}

func TestIncidentStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(IncidentStoreSuite))
}

func (s *IncidentStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	// Its own runtime path: this suite runs alongside the platform/database
	// one, and a shared binaries cache makes both initdb runs fight.
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewIncidentStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "incident-test", "", "/tmp/incident-test", "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *IncidentStoreSuite) TearDownSuite() {
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

func (s *IncidentStoreSuite) input(fingerprint string, severity domain.IncidentSeverity) domain.IncidentInput {
	return domain.IncidentInput{
		RepositoryID: s.repoID,
		Env:          domain.DeployEnvProd,
		Source:       domain.IncidentSourceWebhook,
		Fingerprint:  fingerprint,
		Title:        "api 5xx",
		Detail:       "burst of 503s",
		Severity:     severity,
		Payload:      map[string]any{"alert": "HighErrorRate"},
	}
}

func (s *IncidentStoreSuite) TestRecurrenceFoldsIntoTheLiveIncident() {
	first, created, err := s.store.Upsert(s.ctx, s.input("fp-fold", domain.IncidentSeverityHigh))
	s.Require().NoError(err)
	s.True(created)
	s.Equal(1, first.Occurrences)

	second, created, err := s.store.Upsert(s.ctx, s.input("fp-fold", domain.IncidentSeverityHigh))
	s.Require().NoError(err)
	s.False(created, "the same fingerprint must not open a second incident")
	s.Equal(first.ID, second.ID)
	s.Equal(2, second.Occurrences)
}

// An escalating recurrence (warning → page) must raise the severity, not be
// absorbed at the level the first alert happened to carry.
func (s *IncidentStoreSuite) TestSeverityEscalatesButNeverDowngrades() {
	_, _, err := s.store.Upsert(s.ctx, s.input("fp-escalate", domain.IncidentSeverityMedium))
	s.Require().NoError(err)

	escalated, _, err := s.store.Upsert(s.ctx, s.input("fp-escalate", domain.IncidentSeverityCritical))
	s.Require().NoError(err)
	s.Equal(domain.IncidentSeverityCritical, escalated.Severity)

	stillCritical, _, err := s.store.Upsert(s.ctx, s.input("fp-escalate", domain.IncidentSeverityLow))
	s.Require().NoError(err)
	s.Equal(domain.IncidentSeverityCritical, stillCritical.Severity)
}

func (s *IncidentStoreSuite) TestResolvedIncidentDoesNotAbsorbAFreshOutage() {
	first, _, err := s.store.Upsert(s.ctx, s.input("fp-recur", domain.IncidentSeverityHigh))
	s.Require().NoError(err)
	_, err = s.store.UpdateRemedy(s.ctx, first.ID, "raised the pool size", domain.RemedyKindConfig,
		domain.RemedyAuthorHuman, 70)
	s.Require().NoError(err)
	_, err = s.store.UpdateStatus(s.ctx, first.ID, domain.IncidentStatusResolved)
	s.Require().NoError(err)

	second, created, err := s.store.Upsert(s.ctx, s.input("fp-recur", domain.IncidentSeverityHigh))
	s.Require().NoError(err)
	s.True(created, "a new outage after a resolve is a new incident")
	s.NotEqual(first.ID, second.ID)

	history, err := s.store.History(s.ctx, s.repoID, "fp-recur", 5)
	s.Require().NoError(err)
	s.Require().Len(history, 1, "only the resolved one is history")
	s.Equal("raised the pool size", history[0].Remedy)
}

// Who wrote the remedy is what the ingest guard keys off, so the column has to
// round-trip: unknown on a fresh row (nothing has been written yet — and rows
// predating the column are NULL, never backfilled), then the author of every
// write that follows.
func (s *IncidentStoreSuite) TestUpdateRemedyRecordsItsAuthor() {
	incident, _, err := s.store.Upsert(s.ctx, s.input("fp-author", domain.IncidentSeverityHigh))
	s.Require().NoError(err)
	s.Empty(incident.RemedyAuthor, "nothing has written a remedy yet")

	triaged, err := s.store.UpdateRemedy(s.ctx, incident.ID, "restart the deployment",
		domain.RemedyKindRollback, domain.RemedyAuthorAutoTriage, 40)
	s.Require().NoError(err)
	s.Equal(domain.RemedyAuthorAutoTriage, triaged.RemedyAuthor)

	proposed, err := s.store.UpdateRemedy(s.ctx, incident.ID, "connection pool exhausted; raise max_conns",
		domain.RemedyKindConfig, domain.RemedyAuthorAgent, 85)
	s.Require().NoError(err)
	s.Equal(domain.RemedyAuthorAgent, proposed.RemedyAuthor, "a real diagnosis replaces the machine's")
	s.Equal("connection pool exhausted; raise max_conns", proposed.Remedy)

	fetched, err := s.store.Get(s.ctx, incident.ID)
	s.Require().NoError(err)
	s.Equal(domain.RemedyAuthorAgent, fetched.RemedyAuthor, "authorship survives a re-read")

	listed, err := s.store.List(s.ctx, domain.IncidentFilter{RepositoryID: &s.repoID})
	s.Require().NoError(err)
	for _, inc := range listed {
		if inc.ID == incident.ID {
			s.Equal(domain.RemedyAuthorAgent, inc.RemedyAuthor, "listings carry the author too")
		}
	}
}

func (s *IncidentStoreSuite) TestFindLiveAndEventsAndTaskLookup() {
	incident, _, err := s.store.Upsert(s.ctx, s.input("fp-live", domain.IncidentSeverityCritical))
	s.Require().NoError(err)

	live, err := s.store.FindLive(s.ctx, s.repoID, domain.DeployEnvProd, "fp-live")
	s.Require().NoError(err)
	s.Equal(incident.ID, live.ID)

	s.Require().NoError(s.store.AppendEvent(s.ctx, incident.ID, domain.IncidentEventDetected, "probe failed twice"))
	fetched, err := s.store.Get(s.ctx, incident.ID)
	s.Require().NoError(err)
	s.Require().Len(fetched.Events, 1)
	s.Equal("probe failed twice", fetched.Events[0].Message)
	s.Equal("HighErrorRate", fetched.Payload["alert"])

	_, err = s.store.UpdateStatus(s.ctx, incident.ID, domain.IncidentStatusIgnored)
	s.Require().NoError(err)
	_, err = s.store.FindLive(s.ctx, s.repoID, domain.DeployEnvProd, "fp-live")
	s.ErrorIs(err, domain.ErrIncidentNotFound, "an ignored incident is not live")
}

func (s *IncidentStoreSuite) TestListFiltersByStatus() {
	_, _, err := s.store.Upsert(s.ctx, s.input("fp-list", domain.IncidentSeverityHigh))
	s.Require().NoError(err)

	open, err := s.store.List(s.ctx, domain.IncidentFilter{
		RepositoryID: &s.repoID,
		Statuses:     []domain.IncidentStatus{domain.IncidentStatusOpen},
	})
	s.Require().NoError(err)
	s.NotEmpty(open)
	for _, inc := range open {
		s.Equal(domain.IncidentStatusOpen, inc.Status)
	}

	all, err := s.store.List(s.ctx, domain.IncidentFilter{RepositoryID: &s.repoID})
	s.Require().NoError(err)
	s.GreaterOrEqual(len(all), len(open))
}

func (s *IncidentStoreSuite) TestListFiltersBySource() {
	webhook, _, err := s.store.Upsert(s.ctx, s.input("fp-source-webhook", domain.IncidentSeverityHigh))
	s.Require().NoError(err)
	in := s.input("fp-source-rule", domain.IncidentSeverityHigh)
	in.Source = domain.IncidentSourceAlertRule
	rule, _, err := s.store.Upsert(s.ctx, in)
	s.Require().NoError(err)

	got, err := s.store.List(s.ctx, domain.IncidentFilter{RepositoryID: &s.repoID, Source: domain.IncidentSourceAlertRule, Limit: 500})
	s.Require().NoError(err)
	ids := map[uuid.UUID]bool{}
	for _, inc := range got {
		s.Equal(domain.IncidentSourceAlertRule, inc.Source)
		ids[inc.ID] = true
	}
	s.True(ids[rule.ID])
	s.False(ids[webhook.ID])
}
