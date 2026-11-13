package database_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/migrations"
)

const (
	lastTenantMigration = "132_drop_plan_concurrency"
	dropTenancy         = "133_drop_tenancy"
	localTenant         = "00000000-0000-0000-0000-000000000001"
	otherTenant         = "00000000-0000-0000-0000-000000000002"
	appRole             = "agent_app"
	appRolePassword     = "agent_app_test"
)

// DropTenancySuite applies migration 133 to databases holding rows, both as the
// embedded superuser and as an unprivileged role that owns the database — the
// harder case, subject to the FORCE ROW LEVEL SECURITY 133 removes.
type DropTenancySuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	boot   *pgxpool.Pool
	pools  []*pgxpool.Pool
	// fresh is a database migrated from empty by the unprivileged owner, read
	// through the superuser.
	fresh *pgxpool.Pool
}

func TestDropTenancySuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(DropTenancySuite))
}

func (s *DropTenancySuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 10*time.Minute)
	pg := sharedPostgres(s.T())
	s.pg = pg
	s.boot = s.connect(pg.DSN())
	// The cluster is shared with the other suites and outlives a -count=N
	// iteration, so the role may already be there.
	s.exec(s.boot, `DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '`+appRole+`') THEN `+
		`CREATE ROLE `+appRole+` LOGIN PASSWORD '`+appRolePassword+`' `+
		`NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT; END IF; END $$`)

	admin, runner := s.database("fresh_owner", true)
	s.Require().NoError(database.RunMigrations(s.ctx, runner),
		"every migration must apply as a NOSUPERUSER NOBYPASSRLS owner")
	s.fresh = admin
}

func (s *DropTenancySuite) TearDownSuite() {
	for _, p := range s.pools {
		p.Close()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// TestEveryMigrationApplied checks the ledger against the embedded files, so a
// new migration cannot leave this suite asserting an old schema.
func (s *DropTenancySuite) TestEveryMigrationApplied() {
	files, err := database.ListMigrationFiles(migrations.Up)
	s.Require().NoError(err)
	var applied int
	s.Require().NoError(s.fresh.QueryRow(s.ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied))
	s.Equal(len(files), applied)
}

func (s *DropTenancySuite) TestNoTenancyRemains() {
	for what, sql := range map[string]string{
		"tenant_id column": `
			SELECT c.relname FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
			WHERE c.relnamespace = 'public'::regnamespace AND a.attname = 'tenant_id' AND NOT a.attisdropped`,
		"policy": `
			SELECT c.relname || '.' || p.polname FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid`,
		"row-level security table": `
			SELECT relname FROM pg_class
			WHERE relnamespace = 'public'::regnamespace AND (relrowsecurity OR relforcerowsecurity)`,
		"default reading app.tenant_id": `
			SELECT c.relname FROM pg_attrdef d JOIN pg_class c ON c.oid = d.adrelid
			WHERE c.relnamespace = 'public'::regnamespace AND pg_get_expr(d.adbin, d.adrelid) LIKE '%app.tenant_id%'`,
		"function reading app.tenant_id": `
			SELECT proname FROM pg_proc
			WHERE pronamespace = 'public'::regnamespace AND prosrc LIKE '%app.tenant_id%'`,
		"tenant registry table": `
			SELECT relname FROM pg_class
			WHERE relnamespace = 'public'::regnamespace AND relname IN ('tenants', 'tenant_members')`,
		"key or index on tenant_id": `
			SELECT conname FROM pg_constraint
			WHERE connamespace = 'public'::regnamespace AND pg_get_constraintdef(oid) LIKE '%tenant_id%'
			UNION ALL
			SELECT ic.relname FROM pg_index i
			JOIN pg_class t ON t.oid = i.indrelid JOIN pg_class ic ON ic.oid = i.indexrelid
			WHERE t.relnamespace = 'public'::regnamespace AND pg_get_indexdef(i.indexrelid) LIKE '%tenant_id%'`,
	} {
		s.Emptyf(s.column(s.fresh, sql), "a %s survived the migrations", what)
	}
}

func (s *DropTenancySuite) TestPopulatedDatabaseAsSuperuser() {
	s.dropTenancyKeepsData(s.database("populated_superuser", false))
}

func (s *DropTenancySuite) TestPopulatedDatabaseAsOwner() {
	s.dropTenancyKeepsData(s.database("populated_owner", true))
}

// TestTwoTenantsRefuseToMerge: their natural keys would collapse into each
// other, so 133 must fail and, being one transaction, change nothing.
func (s *DropTenancySuite) TestTwoTenantsRefuseToMerge() {
	admin, runner := s.database("two_tenants", false)
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, runner, lastTenantMigration))
	for _, id := range []string{localTenant, otherTenant} {
		s.inTenant(runner, id, func(tx pgx.Tx) {
			_, err := tx.Exec(s.ctx, `INSERT INTO agents (name) VALUES ('Developer')`)
			s.Require().NoError(err)
		})
	}
	before := s.column(admin, schemaShapeSQL)

	err := database.RunMigrationsUpTo(s.ctx, runner, dropTenancy)
	s.Require().Error(err)
	s.Contains(err.Error(), "refusing to merge")

	s.Equal(before, s.column(admin, schemaShapeSQL), "a failed 133 left the schema changed")
	var recorded bool
	s.Require().NoError(admin.QueryRow(s.ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, dropTenancy).Scan(&recorded))
	s.False(recorded)
}

var seededAt = time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)

type seededRows struct {
	agent, task, session string
}

func (s *DropTenancySuite) dropTenancyKeepsData(admin, runner *pgxpool.Pool) {
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, runner, lastTenantMigration))
	seeded := s.seedLocalTenant(runner)

	rowsBefore := s.checksums(admin)
	s.True(strings.HasPrefix(rowsBefore["agents"], "1|"), "the fixture wrote no agent: %q", rowsBefore["agents"])
	consBefore := s.pairs(admin, constraintsSQL)
	idxBefore := s.pairs(admin, indexesSQL)

	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, runner, dropTenancy))

	s.Equal(rowsBefore, s.checksums(admin), "row data changed")
	s.Equal(expectedConstraints(consBefore), s.pairs(admin, constraintsSQL))
	s.Equal(expectedIndexes(consBefore, idxBefore), s.pairs(admin, indexesSQL))

	var stateRows int
	var carried bool
	s.Require().NoError(admin.QueryRow(s.ctx,
		`SELECT count(*), bool_and(board_seeded_at = $1) FROM install_state`, seededAt).Scan(&stateRows, &carried))
	s.Equal(1, stateRows)
	s.True(carried, "install_state.board_seeded_at must carry tenants.bootstrapped_at")

	// Everything below runs as the migrating role with no app.tenant_id set.
	_, err := runner.Exec(s.ctx, `INSERT INTO agents (name) VALUES ('Developer')`)
	var pgErr *pgconn.PgError
	s.Require().Truef(errors.As(err, &pgErr), "a duplicate agent name must fail, got %v", err)
	s.Equal("23505", pgErr.Code)
	s.Equal("agents_name_key", pgErr.ConstraintName)

	_, err = runner.Exec(s.ctx, `
		INSERT INTO app_settings (key, value) VALUES ('workspace_root', '/moved')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`)
	s.Require().NoError(err)
	_, err = runner.Exec(s.ctx, `
		INSERT INTO gcloud_credentials (project_id, client_email, data) VALUES ('p2', 'e2', '\x02'::bytea)
		ON CONFLICT (id) DO UPDATE SET project_id = EXCLUDED.project_id`)
	s.Require().NoError(err)
	var credentials int
	var project string
	s.Require().NoError(runner.QueryRow(s.ctx,
		`SELECT count(*), max(project_id) FROM gcloud_credentials`).Scan(&credentials, &project))
	s.Equal(1, credentials, "gcloud_credentials must stay one row")
	s.Equal("p2", project)

	_, err = runner.Exec(s.ctx, `DELETE FROM agents WHERE id = $1`, seeded.agent)
	s.Require().NoError(err)
	var unassigned bool
	s.Require().NoError(runner.QueryRow(s.ctx,
		`SELECT assignee_agent_id IS NULL FROM board_tasks WHERE id = $1`, seeded.task).Scan(&unassigned))
	s.True(unassigned)

	_, err = runner.Exec(s.ctx, `DELETE FROM board_tasks WHERE id = $1`, seeded.task)
	s.Require().NoError(err)
	var detached bool
	s.Require().NoError(runner.QueryRow(s.ctx,
		`SELECT task_id IS NULL FROM sessions WHERE id = $1`, seeded.session).Scan(&detached))
	s.True(detached)
}

// seedLocalTenant writes rows as the pre-133 app did: inside a transaction
// scoped to the local tenant, with tenant_id coming off the column default.
func (s *DropTenancySuite) seedLocalTenant(runner *pgxpool.Pool) seededRows {
	var out seededRows
	s.inTenant(runner, localTenant, func(tx pgx.Tx) {
		exec := func(sql string, args ...any) {
			_, err := tx.Exec(s.ctx, sql, args...)
			s.Require().NoError(err, sql)
		}
		scan := func(dest *string, sql string, args ...any) {
			s.Require().NoError(tx.QueryRow(s.ctx, sql, args...).Scan(dest), sql)
		}
		exec(`INSERT INTO tenants (id, bootstrapped_at) VALUES ($1, $2)`, localTenant, seededAt)
		var repo, event string
		scan(&repo, `INSERT INTO repositories (name, root_path) VALUES ('app', '/w/app') RETURNING id::text`)
		scan(&out.agent, `INSERT INTO agents (name) VALUES ('Developer') RETURNING id::text`)
		scan(&out.task, `
			INSERT INTO board_tasks (repository_id, title, task_number, assignee_agent_id)
			VALUES ($1, 'first', 1, $2) RETURNING id::text`, repo, out.agent)
		scan(&out.session, `INSERT INTO sessions (title, task_id) VALUES ('chat', $1) RETURNING id::text`, out.task)
		scan(&event, `
			INSERT INTO board_events (repository_id, task_id, event_type)
			VALUES ($1, $2, 'task.moved') RETURNING id::text`, repo, out.task)
		exec(`INSERT INTO task_agent_runs (task_id, agent_id, board_event_id, status) VALUES ($1, $2, $3, 'pending')`,
			out.task, out.agent, event)
		exec(`INSERT INTO app_settings (key, value) VALUES ('workspace_root', '/w')`)
		exec(`INSERT INTO board_settings (id) VALUES (1)`)
		exec(`INSERT INTO llm_provider_configs (provider_type, base_url, default_model)
			VALUES ('openai', 'https://api.openai.com/v1', 'gpt-4o')`)
		exec(`INSERT INTO mcp_servers (id) VALUES ('github')`)
		exec(`INSERT INTO gcloud_credentials (project_id, client_email, data) VALUES ('p', 'e', '\x01'::bytea)`)
	})
	return out
}

func (s *DropTenancySuite) inTenant(pool *pgxpool.Pool, id string, fn func(pgx.Tx)) {
	tx, err := pool.Begin(s.ctx)
	s.Require().NoError(err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(s.ctx)) }()
	_, err = tx.Exec(s.ctx, `SELECT set_config('app.tenant_id', $1, true)`, id)
	s.Require().NoError(err)
	fn(tx)
	s.Require().NoError(tx.Commit(s.ctx))
}

func (s *DropTenancySuite) checksums(admin *pgxpool.Pool) map[string]string {
	out := map[string]string{}
	for _, table := range s.column(admin, `
		SELECT relname FROM pg_class
		WHERE relnamespace = 'public'::regnamespace AND relkind = 'r'
		  AND relname NOT IN ('tenants', 'tenant_members', 'schema_migrations', 'install_state')
		ORDER BY 1`) {
		drop := `- 'tenant_id' - 'assignee_user_id' - 'owner_user_id'`
		if table == "gcloud_credentials" {
			drop += ` - 'id'`
		}
		var n int64
		var sum string
		s.Require().NoError(admin.QueryRow(s.ctx, fmt.Sprintf(`
			SELECT count(*), md5(coalesce(string_agg(h, '' ORDER BY h), ''))
			FROM (SELECT md5((to_jsonb(x) %s)::text) AS h FROM public.%s x) rows`,
			drop, pgx.Identifier{table}.Sanitize())).Scan(&n, &sum))
		out[table] = fmt.Sprintf("%d|%s", n, sum)
	}
	return out
}

const constraintsSQL = `
	SELECT cls.relname || '.' || con.conname, pg_get_constraintdef(con.oid)
	FROM pg_constraint con JOIN pg_class cls ON cls.oid = con.conrelid
	WHERE con.connamespace = 'public'::regnamespace`

const indexesSQL = `
	SELECT ic.relname, t.relname || '|' || pg_get_indexdef(i.indexrelid)
	FROM pg_index i JOIN pg_class t ON t.oid = i.indrelid JOIN pg_class ic ON ic.oid = i.indexrelid
	WHERE t.relnamespace = 'public'::regnamespace`

const schemaShapeSQL = `
	SELECT 'table ' || relname FROM pg_class
	WHERE relnamespace = 'public'::regnamespace AND relkind = 'r'
	UNION ALL
	SELECT 'column ' || c.relname || '.' || a.attname FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
	WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND a.attnum > 0 AND NOT a.attisdropped
	UNION ALL
	SELECT 'policy ' || c.relname || '.' || p.polname FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid
	UNION ALL
	SELECT 'constraint ' || c.relname || '.' || con.conname || ' ' || pg_get_constraintdef(con.oid)
	FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
	WHERE con.connamespace = 'public'::regnamespace
	UNION ALL
	SELECT 'index ' || pg_get_indexdef(i.indexrelid) FROM pg_index i JOIN pg_class t ON t.oid = i.indrelid
	WHERE t.relnamespace = 'public'::regnamespace
	ORDER BY 1`

func dropLeadingTenant(def string) string { return strings.ReplaceAll(def, "(tenant_id, ", "(") }

func onDroppedTable(qualified string) bool {
	table, _, _ := strings.Cut(qualified, ".")
	return table == "tenants" || table == "tenant_members"
}

// expectedConstraints: the pre-133 set with tenant_id dropped from its columns,
// minus the UNIQUE (tenant_id, id) composite key, plus the three 133 adds.
func expectedConstraints(before map[string]string) map[string]string {
	want := map[string]string{}
	for name, def := range before {
		switch {
		case onDroppedTable(name), def == "UNIQUE (tenant_id, id)":
		case name == "gcloud_credentials.gcloud_credentials_pkey":
			want[name] = "PRIMARY KEY (id)"
		default:
			want[name] = dropLeadingTenant(def)
		}
	}
	want["install_state.install_state_pkey"] = "PRIMARY KEY (id)"
	want["install_state.install_state_id_check"] = "CHECK ((id = 1))"
	want["gcloud_credentials.gcloud_credentials_id_check"] = "CHECK ((id = 1))"
	return want
}

func expectedIndexes(consBefore, before map[string]string) map[string]string {
	gone := map[string]bool{
		"idx_board_tasks_assignee_user":              true,
		"idx_agents_owner":                           true,
		"idx_agent_memories_owner":                   true,
		"idx_repository_gcloud_resources_repository": true,
	}
	for name, def := range consBefore {
		if def == "UNIQUE (tenant_id, id)" {
			_, index, _ := strings.Cut(name, ".")
			gone[index] = true
		}
	}
	want := map[string]string{}
	for name, def := range before {
		table, _, _ := strings.Cut(def, "|")
		switch {
		case gone[name], table == "tenants", table == "tenant_members":
		case name == "gcloud_credentials_pkey":
			want[name] = "gcloud_credentials|CREATE UNIQUE INDEX gcloud_credentials_pkey ON public.gcloud_credentials USING btree (id)"
		default:
			want[name] = dropLeadingTenant(def)
		}
	}
	want["install_state_pkey"] = "install_state|CREATE UNIQUE INDEX install_state_pkey ON public.install_state USING btree (id)"
	return want
}

// database creates an empty database; as the owner variant it belongs to the
// unprivileged role, with the extensions a superuser has to install first.
func (s *DropTenancySuite) database(name string, asOwner bool) (admin, runner *pgxpool.Pool) {
	s.exec(s.boot, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	create := `CREATE DATABASE ` + name
	if asOwner {
		create += ` OWNER ` + appRole
	}
	s.exec(s.boot, create)
	admin = s.connect(dsnFor(s.pg.DSN(), nil, name))
	if !asOwner {
		return admin, admin
	}
	s.exec(admin, `REVOKE ALL ON DATABASE `+name+` FROM PUBLIC`)
	s.exec(admin, `CREATE EXTENSION IF NOT EXISTS pgcrypto`)
	s.exec(admin, `CREATE EXTENSION IF NOT EXISTS pg_trgm`)
	return admin, s.connect(dsnFor(s.pg.DSN(), url.UserPassword(appRole, appRolePassword), name))
}

func dsnFor(base string, user *url.Userinfo, db string) string {
	u, err := url.Parse(base)
	if err != nil {
		panic(err)
	}
	if user != nil {
		u.User = user
	}
	u.Path = "/" + db
	return u.String()
}

func (s *DropTenancySuite) connect(dsn string) *pgxpool.Pool {
	pool, err := pgxpool.New(s.ctx, dsn)
	s.Require().NoError(err)
	s.pools = append(s.pools, pool)
	return pool
}

func (s *DropTenancySuite) exec(pool *pgxpool.Pool, sql string) {
	_, err := pool.Exec(s.ctx, sql)
	s.Require().NoError(err, sql)
}

func (s *DropTenancySuite) column(pool *pgxpool.Pool, sql string) []string {
	rows, err := pool.Query(s.ctx, sql)
	s.Require().NoError(err)
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		s.Require().NoError(rows.Scan(&v))
		out = append(out, v)
	}
	s.Require().NoError(rows.Err())
	return out
}

func (s *DropTenancySuite) pairs(pool *pgxpool.Pool, sql string) map[string]string {
	rows, err := pool.Query(s.ctx, sql)
	s.Require().NoError(err)
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		s.Require().NoError(rows.Scan(&k, &v))
		out[k] = v
	}
	s.Require().NoError(rows.Err())
	return out
}
