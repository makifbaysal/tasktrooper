package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

type DesignSystemStoreSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	pg        *database.Embedded
	pool      *pgxpool.Pool
	db        *postgres.DB
	store     *postgres.DesignSystemStore
	projectID uuid.UUID
	repoID    uuid.UUID
}

func TestDesignSystemStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(DesignSystemStoreSuite))
}

func (s *DesignSystemStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewDesignSystemStore(s.db)
}

func (s *DesignSystemStoreSuite) SetupTest() {
	name := "design-system-" + uuid.NewString()[:8]
	repo, err := postgres.NewRepositoryStore(s.db).Create(s.ctx, name, "", "/tmp/"+name, "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
	project, err := postgres.NewInitiativeProjectStore(s.db).Create(s.ctx, name, "")
	s.Require().NoError(err)
	s.projectID = project.ID
}

func (s *DesignSystemStoreSuite) TearDownSuite() {
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

func (s *DesignSystemStoreSuite) base(md string) domain.DesignSystem {
	return domain.DesignSystem{
		Scope:     domain.DesignSystemScopeProject,
		ProjectID: &s.projectID,
		Status:    domain.DesignSystemInReview,
		DesignMD:  md,
		Tokens:    json.RawMessage(`{"color":{"primary":{"$value":"#15214b"}}}`),
		CreatedBy: "ui-designer",
	}
}

func (s *DesignSystemStoreSuite) TestVersionsApproveAndSupersede() {
	first, err := s.store.Create(s.ctx, s.base("v1"))
	s.Require().NoError(err)
	s.Equal(1, first.Version)
	s.JSONEq(`{"color":{"primary":{"$value":"#15214b"}}}`, string(first.Tokens))

	second, err := s.store.Create(s.ctx, s.base("v2"))
	s.Require().NoError(err)
	s.Equal(2, second.Version)

	approved, err := s.store.Approve(s.ctx, first.ID)
	s.Require().NoError(err)
	s.Equal(domain.DesignSystemApproved, approved.Status)
	s.NotNil(approved.ApprovedAt)

	_, err = s.store.Approve(s.ctx, second.ID)
	s.Require().NoError(err)

	list, err := s.store.ListForProject(s.ctx, s.projectID)
	s.Require().NoError(err)
	s.Require().Len(list, 2)
	s.Equal(2, list[0].Version)
	s.Equal(domain.DesignSystemApproved, list[0].Status)
	s.Equal(domain.DesignSystemSuperseded, list[1].Status)
}

func (s *DesignSystemStoreSuite) TestRepositoryLayerNumbersOnItsOwn() {
	_, err := s.store.Create(s.ctx, s.base("base"))
	s.Require().NoError(err)
	layer, err := s.store.Create(s.ctx, domain.DesignSystem{
		Scope:        domain.DesignSystemScopeRepository,
		RepositoryID: &s.repoID,
		Status:       domain.DesignSystemInReview,
		Tokens:       json.RawMessage(`{"color":{"accent":{"$value":"#f0b86e"}}}`),
		Rationale:    "marketing site identity",
	})
	s.Require().NoError(err)
	s.Equal(1, layer.Version)

	layer.DesignMD = "site layer"
	updated, err := s.store.UpdateContent(s.ctx, layer)
	s.Require().NoError(err)
	s.Equal("site layer", updated.DesignMD)

	list, err := s.store.ListForRepository(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(list, 1)
}

func (s *DesignSystemStoreSuite) TestRepositoryBaseProjectAndRequests() {
	got, err := s.store.RepositoryBaseProject(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Nil(got)

	s.Require().NoError(s.store.SetRepositoryBaseProject(s.ctx, s.repoID, &s.projectID))
	got, err = s.store.RepositoryBaseProject(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().NotNil(got)
	s.Equal(s.projectID, *got)

	s.Require().NoError(s.store.SetRepositoryBaseProject(s.ctx, s.repoID, nil))
	got, err = s.store.RepositoryBaseProject(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Nil(got)

	none, err := s.store.LatestRequestForProject(s.ctx, s.projectID)
	s.Require().NoError(err)
	s.Nil(none)
}

func (s *DesignSystemStoreSuite) TestDesignTaskTypeIsSeeded() {
	var prefix, mode string
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT key_prefix, assignee_mode FROM task_types WHERE key = 'design'`).Scan(&prefix, &mode))
	s.Equal("D", prefix)
	s.Equal("override", mode)

	var stages int
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT count(*) FROM workflow_stages WHERE task_type = 'design'`).Scan(&stages))
	s.Equal(8, stages)

	var approves int
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT count(*) FROM workflow_stages
		WHERE task_type = 'design' AND column_slug = 'done' AND behaviours @> '[{"key":"approve_design_system_on_enter"}]'`).Scan(&approves))
	s.Equal(1, approves)
}

func (s *DesignSystemStoreSuite) newTask(taskType domain.TaskType, column domain.TaskColumn) domain.BoardTask {
	tasks := postgres.NewBoardTaskStore(s.db)
	num, err := tasks.NextTaskNumber(s.ctx, taskType)
	s.Require().NoError(err)
	task, err := tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID, TaskNumber: num, Title: "design blocker test", TaskType: taskType,
		Column: column, Priority: domain.TaskPriorityMedium, CreatedBy: "test",
	})
	s.Require().NoError(err)
	return task
}

func (s *DesignSystemStoreSuite) setColumn(id uuid.UUID, column domain.TaskColumn) {
	_, err := s.pool.Exec(s.ctx, `UPDATE board_tasks SET board_column = $2 WHERE id = $1`, id, column)
	s.Require().NoError(err)
}

func (s *DesignSystemStoreSuite) TestADesignTaskHoldsItsDependentsUntilReleased() {
	relations := postgres.NewTaskRelationStore(s.db)
	design := s.newTask(domain.TaskTypeDesign, domain.TaskColumnAnalizReview)
	plain := s.newTask(domain.TaskTypeTask, domain.TaskColumnInProgress)
	impl := s.newTask(domain.TaskTypeTask, domain.TaskColumnTodo)
	_, err := relations.AddBlockers(s.ctx, impl.ID, []uuid.UUID{design.ID, plain.ID})
	s.Require().NoError(err)

	blocking := func() []uuid.UUID {
		tasks, err := relations.ListBlockingSources(s.ctx, impl.ID)
		s.Require().NoError(err)
		ids := []uuid.UUID{}
		for _, t := range tasks {
			ids = append(ids, t.ID)
		}
		return ids
	}
	unfinished := func() []uuid.UUID {
		rels, err := relations.ListUnfinishedBlockers(s.ctx)
		s.Require().NoError(err)
		ids := []uuid.UUID{}
		for _, r := range rels {
			if r.TargetTaskID == impl.ID {
				ids = append(ids, r.SourceTaskID)
			}
		}
		return ids
	}

	s.ElementsMatch([]uuid.UUID{design.ID, plain.ID}, blocking())

	s.setColumn(design.ID, domain.TaskColumnDone)
	s.setColumn(plain.ID, domain.TaskColumnDone)
	s.ElementsMatch([]uuid.UUID{design.ID}, blocking(), "an approved design still holds: its hand-off is written in done")
	s.ElementsMatch([]uuid.UUID{design.ID}, unfinished())

	s.setColumn(design.ID, domain.TaskColumnReleased)
	s.Empty(blocking())
	s.Empty(unfinished())
}
