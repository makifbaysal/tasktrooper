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

type PipelineListByTaskSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.PipelineStore
	tasks  *postgres.BoardTaskStore
	repoID uuid.UUID
}

func TestPipelineListByTaskSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(PipelineListByTaskSuite))
}

func (s *PipelineListByTaskSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewPipelineStore(db)
	s.tasks = postgres.NewBoardTaskStore(db)

	repo, err := postgres.NewRepositoryStore(db).Create(s.ctx, "pipeline-list-test", "", "/tmp/pipeline-list-test-"+uuid.NewString(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *PipelineListByTaskSuite) TearDownSuite() {
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

func (s *PipelineListByTaskSuite) newTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "pipeline-list-test",
		TaskType:     "task",
		Column:       domain.TaskColumn("backlog"),
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task
}

// newPipeline pins created_at because the column defaults to now(), and
// back-to-back inserts are not guaranteed distinct timestamps.
func (s *PipelineListByTaskSuite) newPipeline(taskID uuid.UUID, createdAt time.Time) domain.TaskPipeline {
	p, err := s.store.Create(s.ctx, domain.TaskPipeline{
		TaskID:       taskID,
		RepositoryID: s.repoID,
		Trigger:      domain.PipelineTriggerManual,
		Status:       domain.PipelineStatusFailed,
	})
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `UPDATE task_pipelines SET created_at = $2 WHERE id = $1`, p.ID, createdAt)
	s.Require().NoError(err)
	return p
}

func (s *PipelineListByTaskSuite) newJob(pipelineID uuid.UUID, name string, position int) {
	_, err := s.store.CreateJob(s.ctx, domain.TaskPipelineJob{
		PipelineID: pipelineID,
		Name:       name,
		Command:    "make " + name,
		Status:     domain.PipelineJobStatusSuccess,
		Position:   position,
	})
	s.Require().NoError(err)
}

func pipelineJobNames(jobs []domain.TaskPipelineJob) []string {
	names := make([]string, len(jobs))
	for i, j := range jobs {
		names[i] = j.Name
	}
	return names
}

func (s *PipelineListByTaskSuite) TestListByTaskAttachesEachPipelinesOwnJobsInPositionOrderNewestFirst() {
	task := s.newTask()
	base := time.Now().Add(-time.Hour).UTC()
	jobless := s.newPipeline(task.ID, base)
	older := s.newPipeline(task.ID, base.Add(time.Minute))
	newer := s.newPipeline(task.ID, base.Add(2*time.Minute))

	s.newJob(newer.ID, "newer-2", 2)
	s.newJob(older.ID, "older-1", 1)
	s.newJob(newer.ID, "newer-0", 0)
	s.newJob(older.ID, "older-0", 0)
	s.newJob(newer.ID, "newer-1", 1)
	s.newJob(older.ID, "older-2", 2)

	other := s.newTask()
	otherPipeline := s.newPipeline(other.ID, base.Add(3*time.Minute))
	s.newJob(otherPipeline.ID, "other-0", 0)

	got, err := s.store.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(got, 3)
	s.Equal([]uuid.UUID{newer.ID, older.ID, jobless.ID}, []uuid.UUID{got[0].ID, got[1].ID, got[2].ID})

	s.Equal([]string{"newer-0", "newer-1", "newer-2"}, pipelineJobNames(got[0].Jobs))
	s.Equal([]string{"older-0", "older-1", "older-2"}, pipelineJobNames(got[1].Jobs))
	s.Nil(got[2].Jobs, "a pipeline with no jobs must keep the nil slice ListJobs returns")

	for _, p := range got {
		perPipeline, err := s.store.ListJobs(s.ctx, p.ID)
		s.Require().NoError(err)
		s.Equal(perPipeline, p.Jobs, "pipeline %s", p.ID)
	}
}

func (s *PipelineListByTaskSuite) TestListByTaskReturnsOnlyTheNewestLimitPipelines() {
	task := s.newTask()
	base := time.Now().Add(-24 * time.Hour).UTC()
	total := postgres.TaskPipelineHistoryLimit + 2
	created := make([]domain.TaskPipeline, total)
	for i := range created {
		created[i] = s.newPipeline(task.ID, base.Add(time.Duration(i)*time.Second))
	}
	oldest, newest := created[0], created[total-1]
	s.newJob(oldest.ID, "oldest-0", 0)
	s.newJob(newest.ID, "newest-0", 0)

	got, err := s.store.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(got, postgres.TaskPipelineHistoryLimit)
	s.Equal(newest.ID, got[0].ID)
	s.Equal([]string{"newest-0"}, pipelineJobNames(got[0].Jobs))
	s.Equal(created[total-postgres.TaskPipelineHistoryLimit].ID, got[len(got)-1].ID)

	returned := make(map[uuid.UUID]bool, len(got))
	for _, p := range got {
		returned[p.ID] = true
	}
	s.False(returned[oldest.ID])
	s.False(returned[created[1].ID])
}

func (s *PipelineListByTaskSuite) TestListByTaskWithNoPipelinesReturnsNil() {
	got, err := s.store.ListByTask(s.ctx, s.newTask().ID)
	s.Require().NoError(err)
	s.Nil(got)
}
