package database_test

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

const lastMigrationBeforeRoleWorkflows = "142_role_kpis_v2"

// RoleWorkflowMigrationSuite covers migration 143: its seeded roles, task types
// and workflow stages must reproduce today's board behaviour (release-b-plan.md
// §3), and workflowtest.Default() must build the same shape for a fresh install.
type RoleWorkflowMigrationSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	boot   *pgxpool.Pool
	seq    atomic.Uint64
}

func TestRoleWorkflowMigrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(RoleWorkflowMigrationSuite))
}

func (s *RoleWorkflowMigrationSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg
	boot, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.boot = boot
}

func (s *RoleWorkflowMigrationSuite) TearDownSuite() {
	if s.boot != nil {
		s.boot.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *RoleWorkflowMigrationSuite) freshDatabase() *pgxpool.Pool {
	name := fmt.Sprintf("role_workflow_%d", s.seq.Add(1))
	_, err := s.boot.Exec(s.ctx, `CREATE DATABASE `+name)
	s.Require().NoError(err)
	s.T().Cleanup(func() {
		_, _ = s.boot.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	})

	u, err := url.Parse(s.pg.DSN())
	s.Require().NoError(err)
	u.Path = "/" + name
	pool, err := pgxpool.New(s.ctx, u.String())
	s.Require().NoError(err)
	s.T().Cleanup(pool.Close)
	return pool
}

// builtinAgentNames are the six names migration 143's role assignments key off,
// seeded minimally so the migration's agent lookups have something to match.
var builtinAgentNames = []string{
	"backend-developer", "frontend-developer", "mobile-developer",
	"system-architect", "qa-agent", "product-manager",
}

func (s *RoleWorkflowMigrationSuite) seedBuiltinAgents(pool *pgxpool.Pool) {
	for _, name := range builtinAgentNames {
		_, err := pool.Exec(s.ctx, `INSERT INTO agents (name) VALUES ($1)`, name)
		s.Require().NoError(err)
	}
}

func (s *RoleWorkflowMigrationSuite) TestAllDefaultAnalizSettings() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeRoleWorkflows))
	s.seedBuiltinAgents(pool)
	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	roles, err := postgres.NewRoleStore(postgres.NewDB(pool)).List(s.ctx)
	s.Require().NoError(err)
	analyst := findRole(roles, "analyst")
	s.Require().NotNil(analyst)
	s.Require().Len(analyst.Assignments, 1)
	s.Equal("system-architect", analyst.Assignments[0].AgentName)
	s.Nil(analyst.Assignments[0].Areas, "one agent covering all three areas collapses to areas=NULL")

	var count int
	s.Require().NoError(pool.QueryRow(s.ctx, `SELECT COUNT(*) FROM app_settings WHERE key LIKE 'analiz_assignee_%'`).Scan(&count))
	s.Zero(count, "analiz_assignee_* settings must be deleted")
}

func (s *RoleWorkflowMigrationSuite) TestBackendOverridden() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeRoleWorkflows))
	s.seedBuiltinAgents(pool)
	_, err := pool.Exec(s.ctx, `INSERT INTO agents (name) VALUES ('backend-analyst')`)
	s.Require().NoError(err)
	_, err = pool.Exec(s.ctx, `INSERT INTO app_settings (key, value) VALUES ('analiz_assignee_backend', 'backend-analyst')`)
	s.Require().NoError(err)
	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	roles, err := postgres.NewRoleStore(postgres.NewDB(pool)).List(s.ctx)
	s.Require().NoError(err)
	analyst := findRole(roles, "analyst")
	s.Require().NotNil(analyst)
	s.Require().Len(analyst.Assignments, 2)

	byAgent := map[string][]string{}
	for _, a := range analyst.Assignments {
		byAgent[a.AgentName] = a.Areas
	}
	s.Equal([]string{"backend"}, byAgent["backend-analyst"])
	s.ElementsMatch([]string{"frontend", "mobile"}, byAgent["system-architect"])
}

func (s *RoleWorkflowMigrationSuite) TestMissingAgentIsSkipped() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeRoleWorkflows))
	s.seedBuiltinAgents(pool)
	_, err := pool.Exec(s.ctx, `INSERT INTO app_settings (key, value) VALUES ('analiz_assignee_backend', 'ghost-agent')`)
	s.Require().NoError(err)
	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	roles, err := postgres.NewRoleStore(postgres.NewDB(pool)).List(s.ctx)
	s.Require().NoError(err)
	analyst := findRole(roles, "analyst")
	s.Require().NotNil(analyst)
	s.Require().Len(analyst.Assignments, 1)
	s.Equal("system-architect", analyst.Assignments[0].AgentName)
	s.ElementsMatch([]string{"frontend", "mobile"}, analyst.Assignments[0].Areas)
}

func (s *RoleWorkflowMigrationSuite) TestTaskTypeForeignKey() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeRoleWorkflows))
	s.seedBuiltinAgents(pool)
	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	var repoID string
	s.Require().NoError(pool.QueryRow(s.ctx,
		`INSERT INTO repositories (name, root_path) VALUES ('r', '/tmp/r') RETURNING id`).Scan(&repoID))

	_, err := pool.Exec(s.ctx, `
		INSERT INTO board_tasks (repository_id, task_number, title, task_type, board_column)
		VALUES ($1, 1, 't', 'not_a_real_type', 'backlog')
	`, repoID)
	s.Error(err, "an unknown task_type must be refused by the FK")

	_, err = pool.Exec(s.ctx, `
		INSERT INTO board_tasks (repository_id, task_number, title, task_type, board_column)
		VALUES ($1, 2, 't', 'technical', 'backlog')
	`, repoID)
	s.NoError(err, "a real task_types row must be accepted")
}

// TestParityWithWorkflowtestDefault checks migration 143's promise: what it
// seeds for a fresh install and what workflowtest.Default() builds must be the
// identical shape (release-b-plan.md §Goal).
func (s *RoleWorkflowMigrationSuite) TestParityWithWorkflowtestDefault() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeRoleWorkflows))
	s.seedBuiltinAgents(pool)
	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	db := postgres.NewDB(pool)
	migratedRoles, err := postgres.NewRoleStore(db).List(s.ctx)
	s.Require().NoError(err)
	migratedWorkflows, err := postgres.NewWorkflowStore(db).LoadAll(s.ctx)
	s.Require().NoError(err)

	want := workflowtest.Default()

	// Compare by key and by agent name, not by id: the migration's ids are
	// real, the fixture's are deterministic fakes.
	s.Require().Len(migratedRoles, len(want.Roles))
	for _, wantRole := range want.Roles {
		got := findRole(migratedRoles, wantRole.Key)
		if !s.NotNil(got, "role %q missing from migration", wantRole.Key) {
			continue
		}
		s.Equal(wantRole.Name, got.Name, "role %q name", wantRole.Key)
		s.ElementsMatch(orEmpty(wantRole.RequiredTools), orEmpty(got.RequiredTools), "role %q required_tools", wantRole.Key)
		s.ElementsMatch(normalizeAssignments(wantRole.Assignments), normalizeAssignments(got.Assignments), "role %q assignments", wantRole.Key)
	}

	migratedPurposes, err := postgres.NewRoleStore(db).ListPurposes(s.ctx)
	s.Require().NoError(err)
	roleKeyByID := map[string]string{}
	for _, r := range migratedRoles {
		roleKeyByID[r.ID.String()] = r.Key
	}
	gotPurposeRole := map[string]string{}
	for _, p := range migratedPurposes {
		if p.RoleID != nil {
			gotPurposeRole[string(p.Purpose)] = roleKeyByID[p.RoleID.String()]
		}
	}
	wantPurposeRole := map[string]string{}
	for _, p := range want.Purposes {
		if p.RoleID != nil {
			for _, r := range want.Roles {
				if r.ID == *p.RoleID {
					wantPurposeRole[string(p.Purpose)] = r.Key
				}
			}
		}
	}
	s.Equal(wantPurposeRole, gotPurposeRole)

	s.Require().Len(migratedWorkflows, len(want.Workflows))
	roleKeyOf := func(id *uuid.UUID, roles []domain.AgentRole) string {
		if id == nil {
			return ""
		}
		for _, r := range roles {
			if r.ID == *id {
				return r.Key
			}
		}
		return ""
	}
	for _, wf := range migratedWorkflows {
		wantWF, ok := want.Workflows[wf.Type.Key]
		if !s.True(ok, "task type %q missing from workflowtest.Default()", wf.Type.Key) {
			continue
		}
		s.Equal(wantWF.Type.Label, wf.Type.Label, "type %q label", wf.Type.Key)
		s.Equal(wantWF.Type.KeyPrefix, wf.Type.KeyPrefix, "type %q key_prefix", wf.Type.Key)
		s.Equal(wantWF.Type.IsDefault, wf.Type.IsDefault, "type %q is_default", wf.Type.Key)
		s.Equal(wantWF.Type.IsDefect, wf.Type.IsDefect, "type %q is_defect", wf.Type.Key)
		s.Equal(string(wantWF.Type.AssigneeMode), string(wf.Type.AssigneeMode), "type %q assignee_mode", wf.Type.Key)
		s.Equal(roleKeyOf(wantWF.Type.AssigneeRoleID, want.Roles), roleKeyOf(wf.Type.AssigneeRoleID, migratedRoles), "type %q assignee role", wf.Type.Key)
		s.ElementsMatch(sortedBehaviours(wantWF.Type.Behaviours), sortedBehaviours(wf.Type.Behaviours), "type %q behaviours", wf.Type.Key)

		s.Require().Len(wf.Stages, len(wantWF.Stages), "type %q stage count", wf.Type.Key)
		wantByCol := map[domain.TaskColumn]domain.WorkflowStage{}
		for _, st := range wantWF.Stages {
			wantByCol[st.Column] = st
		}
		for _, st := range wf.Stages {
			wst, ok := wantByCol[st.Column]
			if !s.True(ok, "type %q column %q missing from fixture", wf.Type.Key, st.Column) {
				continue
			}
			label := fmt.Sprintf("%s/%s", wf.Type.Key, st.Column)
			s.Equal(string(wst.Kind), string(st.Kind), "%s kind", label)
			s.Equal(wst.OnPath, st.OnPath, "%s on_path", label)
			s.Equal(wst.Instructions, st.Instructions, "%s instructions", label)
			s.ElementsMatch(sortedBehaviours(wst.Behaviours), sortedBehaviours(st.Behaviours), "%s behaviours", label)
			s.ElementsMatch(normalizeParticipants(wst.Participants, want.Roles), normalizeParticipants(st.Participants, migratedRoles), "%s participants", label)
		}
	}
}

func findRole(roles []domain.AgentRole, key string) *domain.AgentRole {
	for i := range roles {
		if roles[i].Key == key {
			return &roles[i]
		}
	}
	return nil
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type comparableAssignment struct {
	AgentName string
	Areas     string
	Priority  int
}

func normalizeAssignments(in []domain.RoleAssignment) []comparableAssignment {
	out := make([]comparableAssignment, 0, len(in))
	for _, a := range in {
		areas := append([]string(nil), a.Areas...)
		sort.Strings(areas)
		out = append(out, comparableAssignment{AgentName: a.AgentName, Areas: fmt.Sprint(areas), Priority: a.Priority})
	}
	return out
}

func sortedBehaviours(in []domain.BehaviourRef) []string {
	out := make([]string, 0, len(in))
	for _, b := range in {
		out = append(out, fmt.Sprintf("%s:%v", b.Key, b.Params))
	}
	sort.Strings(out)
	return out
}

type comparableParticipant struct {
	RoleKey      string
	Mode         string
	Instructions string
	Position     int
}

func normalizeParticipants(in []domain.StageParticipant, roles []domain.AgentRole) []comparableParticipant {
	out := make([]comparableParticipant, 0, len(in))
	for _, p := range in {
		key := ""
		for _, r := range roles {
			if r.ID == p.RoleID {
				key = r.Key
			}
		}
		out = append(out, comparableParticipant{RoleKey: key, Mode: string(p.Mode), Instructions: p.Instructions, Position: p.Position})
	}
	return out
}
