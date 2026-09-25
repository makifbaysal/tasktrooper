package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/migrations"
)

// monorepoRootCatalog is the repo-root catalog the role agents live in
// now that they are no longer compiled into the binary.
const monorepoRootCatalog = "../../../../../catalog"

// RoleKPIsMigrationSuite checks that migrations/142_role_kpis_v2.up.sql stays
// in sync with the role agents' KPI definitions in the catalog. Rather than
// duplicating the target table a third time in Go, it takes a catalog sync's
// own output — the same SyncFromCatalog call a new install runs to create its
// agents and their KPIs — as the golden set, corrupts it to look like a
// pre-recalibration install, replays the migration's own SQL by hand (the
// schema_migrations ledger already marked 140 applied when the suite's shared
// database was first migrated), and asserts the corrupted rows come back
// exactly to golden. A hand-authored SQL VALUES table that drifted from the
// catalog would fail this without anyone having to remember to update it by
// hand.
type RoleKPIsMigrationSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	kpis   *postgres.KPIStore
}

func TestRoleKPIsMigrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(RoleKPIsMigrationSuite))
}

func (s *RoleKPIsMigrationSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	s.pool, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.db = postgres.NewDB(s.pool)
	s.kpis = postgres.NewKPIStore(s.db)
}

func (s *RoleKPIsMigrationSuite) TearDownSuite() {
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

func filterAgentsByName(agents []domain.Agent, names ...string) []domain.Agent {
	want := make(map[string]struct{}, len(names))
	for _, n := range names {
		want[n] = struct{}{}
	}
	out := make([]domain.Agent, 0, len(names))
	for _, a := range agents {
		if _, ok := want[a.Name]; ok {
			out = append(out, a)
		}
	}
	return out
}

type roleKPISnapshot struct {
	metricKey  string
	period     string
	targetFull float64
	targetHalf float64
	weight     float64
	enabled    bool
}

func (s *RoleKPIsMigrationSuite) TestMigrationReconcilesToCurrentDefaults() {
	agentsStore := postgres.NewCatalogStore(s.db)
	syncStore := postgres.NewCatalogSyncStore(s.db)
	svc := catalog.NewService(agentsStore, noopLLM{}, "")
	svc.SetKPIStore(s.kpis)
	reader := &catalogrepo.Reader{Source: monorepoRootCatalog}
	res, err := svc.SyncFromCatalog(s.ctx, reader, syncStore)
	s.Require().NoError(err)
	s.Require().GreaterOrEqual(res.Created, 6, "catalog sync must create the six role agents")

	allAgents, err := agentsStore.ListAgents(s.ctx)
	s.Require().NoError(err)
	// 142's hand-authored VALUES table only names the six original role agents;
	// an agent added to the catalog afterwards (release-engineer) syncs its KPIs
	// straight from its own catalog.yaml and is not part of this migration's golden set.
	agents := filterAgentsByName(allAgents,
		"system-architect", "backend-developer", "frontend-developer",
		"mobile-developer", "qa-agent", "product-manager")
	s.Require().Len(agents, 6)

	golden := make(map[string]map[string]roleKPISnapshot, 6) // agent name -> lower(trim(kpi name)) -> snapshot
	customKPIID := make(map[string]struct{})
	for _, agent := range agents {
		kpis, err := s.kpis.ListByAgent(s.ctx, agent.ID)
		s.Require().NoError(err)
		s.Require().NotEmpty(kpis, agent.Name)
		byName := make(map[string]roleKPISnapshot, len(kpis))
		for _, k := range kpis {
			byName[strings.ToLower(strings.TrimSpace(k.Name))] = roleKPISnapshot{
				metricKey: k.MetricKey, period: k.Period,
				targetFull: k.TargetFull, targetHalf: k.TargetHalf,
				weight: k.Weight, enabled: k.Enabled,
			}
		}
		golden[agent.Name] = byName

		// An admin-made KPI, under a name the seed would never use, must
		// survive the migration untouched.
		custom, err := s.kpis.CreateKPI(s.ctx, domain.AgentKPI{
			AgentID: agent.ID, MetricKey: "failed_runs", Name: "  Operator custom metric  ",
			Description: "hand-tuned by an operator", Period: domain.KPIPeriodDaily,
			TargetFull: 7, TargetHalf: 9, Weight: 3.25, Enabled: false,
		})
		s.Require().NoError(err)
		customKPIID[custom.ID.String()] = struct{}{}

		// Corrupt every seeded row to look like a stale pre-recalibration
		// install: wrong numbers, and the name re-cased/padded to prove the
		// migration matches case-insensitively and trims whitespace.
		for _, k := range kpis {
			// metric_key is left as seeded: every KPI on one agent already has a
			// distinct metric_key, and forcing them all to the same value here
			// would collide on agent_kpis' (agent_id, metric_key, period)
			// constraint before the migration ever runs. Period is safe to force
			// uniformly for the same reason it does not collide.
			corrupted := k
			corrupted.Name = "  " + strings.ToUpper(k.Name) + "  "
			corrupted.Period = domain.KPIPeriodMonthly
			corrupted.TargetFull = 999
			corrupted.TargetHalf = 999
			corrupted.Weight = 0.01
			corrupted.Enabled = false
			_, err := s.kpis.UpdateKPI(s.ctx, corrupted)
			s.Require().NoError(err)
		}
	}

	body, err := migrations.Up.ReadFile("142_role_kpis_v2.up.sql")
	s.Require().NoError(err)

	// Run it twice: once to reconcile the corrupted rows, once more to prove
	// it is a no-op the second time (idempotent, as an install that boots
	// more than once after the bump would run it — actually the runner only
	// ever applies a migration once, but the SQL itself must tolerate a
	// second run since nothing here relies on schema_migrations bookkeeping).
	for i := 0; i < 2; i++ {
		_, err = s.db.Exec(s.ctx, string(body))
		s.Require().NoError(err, "run %d", i)
	}

	for _, agent := range agents {
		kpis, err := s.kpis.ListByAgent(s.ctx, agent.ID)
		s.Require().NoError(err)

		want := golden[agent.Name]
		gotByName := make(map[string]domain.AgentKPI, len(kpis))
		for _, k := range kpis {
			if _, isCustom := customKPIID[k.ID.String()]; isCustom {
				s.Equal("failed_runs", k.MetricKey, "operator KPI must be untouched")
				s.Equal(domain.KPIPeriodDaily, k.Period)
				s.Equal(7.0, k.TargetFull)
				s.Equal(9.0, k.TargetHalf)
				s.Equal(3.25, k.Weight)
				s.False(k.Enabled)
				continue
			}
			gotByName[strings.ToLower(strings.TrimSpace(k.Name))] = k
		}

		s.Len(gotByName, len(want), "agent %s: no seeded KPI should be duplicated or dropped", agent.Name)
		for name, snap := range want {
			got, ok := gotByName[name]
			if !s.True(ok, "agent %s: %q missing after migration", agent.Name, name) {
				continue
			}
			s.Equal(snap.metricKey, got.MetricKey, "agent %s %q metric_key", agent.Name, name)
			s.Equal(snap.period, got.Period, "agent %s %q period", agent.Name, name)
			s.InDelta(snap.targetFull, got.TargetFull, 0.001, "agent %s %q target_full", agent.Name, name)
			s.InDelta(snap.targetHalf, got.TargetHalf, 0.001, "agent %s %q target_half", agent.Name, name)
			s.InDelta(snap.weight, got.Weight, 0.001, "agent %s %q weight", agent.Name, name)
			s.Equal(snap.enabled, got.Enabled, "agent %s %q enabled", agent.Name, name)
		}
	}
}

type noopLLM struct{}

func (noopLLM) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (noopLLM) ChatStream(context.Context, domain.AgentRequest, func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (noopLLM) Models(context.Context) ([]string, error) { return nil, nil }

func (noopLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }
