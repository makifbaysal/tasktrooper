package postgres_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// BoardTaskParkSuite covers the two pieces of park SQL that only a real
// Postgres can answer for, both added so the board can SHOW a park:
//
//   - BlockOnResource hands back the column the card was parked out of, read
//     from a CTE snapshot inside the same statement that parks it. Nothing in
//     Go can check that the value is the pre-UPDATE one — RETURNING would
//     hand back the post-UPDATE 'blocked' and every Go-level test would still
//     pass.
//   - boardTaskSelect reaches onto the parked run for its recorded reset time
//     (the lateral join behind BoardTask.BlockedResumeAt), guarded so that only
//     a quota park pays for the lookup and only a quota park reports a time.
type BoardTaskParkSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	tasks  *postgres.BoardTaskStore
	runs   *postgres.TaskAgentRunStore
	agents *postgres.CatalogStore
	events *postgres.BoardEventStore
	repoID uuid.UUID
}

func TestBoardTaskParkSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(BoardTaskParkSuite))
}

func (s *BoardTaskParkSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	// Its own runtime path, like every other embedded suite here: a shared
	// binaries cache makes concurrent initdb runs fight.
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.runs = postgres.NewTaskAgentRunStore(s.db)
	s.agents = postgres.NewCatalogStore(s.db)
	s.events = postgres.NewBoardEventStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "board-task-park-test", "", "/tmp/board-task-park-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *BoardTaskParkSuite) TearDownSuite() {
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

func (s *BoardTaskParkSuite) newTask(column domain.TaskColumn) domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeTask)
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "board-task-park-test",
		TaskType:     domain.TaskTypeTask,
		Column:       column,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task
}

// parkedRun writes the run row a quota park leaves behind: the reset time the
// sweeper and the card both read.
func (s *BoardTaskParkSuite) parkedRun(taskID uuid.UUID, resumeAt time.Time) domain.TaskAgentRun {
	agent, err := s.agents.CreateAgent(s.ctx, domain.Agent{
		Name:         "board-task-park-test-" + uuid.New().String(),
		ProviderType: domain.LLMProviderAnthropic,
		Model:        "test-model",
	})
	s.Require().NoError(err)
	event, err := s.events.Create(s.ctx, domain.BoardEvent{
		RepositoryID: s.repoID,
		TaskID:       taskID,
		EventType:    domain.BoardEventTaskMoved,
		Payload:      json.RawMessage(`{}`),
	})
	s.Require().NoError(err)
	run, err := s.runs.Create(s.ctx, domain.TaskAgentRun{
		TaskID:       taskID,
		AgentID:      agent.ID,
		BoardEventID: event.ID,
		Status:       domain.TaskAgentRunStatusPending,
	})
	s.Require().NoError(err)
	run.Status = domain.TaskAgentRunStatusCompleted
	run.CLISessionID = "sess-" + uuid.New().String()
	run.QuotaResumeAt = &resumeAt
	updated, err := s.runs.Update(s.ctx, run)
	s.Require().NoError(err)
	return updated
}

// The from_column of the park event. Returning the post-UPDATE value here would
// make every park in board history read "blocked → blocked".
func (s *BoardTaskParkSuite) TestBlockOnResourceReturnsThePreParkColumn() {
	task := s.newTask(domain.TaskColumnInProgress)

	previous, err := s.tasks.BlockOnResource(s.ctx, s.repoID, task.ID,
		domain.ResourceClaudeCodeQuota, "usage limit reached")
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnInProgress, previous)

	parked, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnBlocked, parked.Column, "the park still lands")
	s.Equal(domain.ResourceClaudeCodeQuota, parked.BlockedResource)
	s.Equal(domain.TaskColumnInProgress, parked.BlockedOriginColumn, "the resume needs somewhere to go back to")
	s.Require().NotNil(parked.BlockedAt)
}

// A second park on an already-blocked card must not overwrite where the work
// came from — the origin column is the only route back. It reports `blocked` as
// the previous column, which is what makes the journal skip the no-op move.
func (s *BoardTaskParkSuite) TestReparkKeepsTheOriginColumn() {
	task := s.newTask(domain.TaskColumnInQA)

	_, err := s.tasks.BlockOnResource(s.ctx, s.repoID, task.ID, domain.ResourceMobileDevice, "device held")
	s.Require().NoError(err)

	previous, err := s.tasks.BlockOnResource(s.ctx, s.repoID, task.ID, domain.ResourceMobileDevice, "still held")
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnBlocked, previous, "nothing moved the second time")

	parked, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnInQA, parked.BlockedOriginColumn)
}

// A task deleted mid-run is not an error to park — it is nothing to park. The
// plain Exec this replaced affected no rows and returned nil; that stays.
func (s *BoardTaskParkSuite) TestBlockOnResourceOnAMissingTaskIsANoOp() {
	previous, err := s.tasks.BlockOnResource(s.ctx, s.repoID, uuid.New(),
		domain.ResourceClaudeCodeQuota, "usage limit reached")
	s.Require().NoError(err)
	s.Equal(domain.TaskColumn(""), previous)
}

// The card says when it comes back. The time lives on the RUN, so the task read
// has to reach for it — and it must be the LATEST parked run's, or a task parked
// twice shows the first park's expiry while the second is still in force.
func (s *BoardTaskParkSuite) TestQuotaParkedTaskCarriesItsResumeTime() {
	task := s.newTask(domain.TaskColumnInProgress)
	first := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	latest := time.Now().Add(4 * time.Hour).UTC().Truncate(time.Second)
	s.parkedRun(task.ID, first)
	s.parkedRun(task.ID, latest)

	_, err := s.tasks.BlockOnResource(s.ctx, s.repoID, task.ID,
		domain.ResourceClaudeCodeQuota, "usage limit reached")
	s.Require().NoError(err)

	parked, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Require().NotNil(parked.BlockedResumeAt, "a quota park with no time renders as an open-ended block")
	s.True(latest.Equal(parked.BlockedResumeAt.UTC()), "want %s, got %s", latest, parked.BlockedResumeAt.UTC())

	// The list endpoints are where the board actually reads this, and they run
	// the same projection — so the badge cannot be present on one path only.
	listed, err := s.tasks.ListByRepository(s.ctx, s.repoID)
	s.Require().NoError(err)
	var found bool
	for _, t := range listed {
		if t.ID != task.ID {
			continue
		}
		found = true
		s.Require().NotNil(t.BlockedResumeAt)
		s.True(latest.Equal(t.BlockedResumeAt.UTC()))
	}
	s.True(found, "the parked task must appear in the repository listing")
}

// A device park has no predictable end, so it reports none — even for a task
// whose history happens to contain an old quota park.
func (s *BoardTaskParkSuite) TestNonQuotaParkReportsNoResumeTime() {
	task := s.newTask(domain.TaskColumnInQA)
	s.parkedRun(task.ID, time.Now().Add(time.Hour))

	_, err := s.tasks.BlockOnResource(s.ctx, s.repoID, task.ID, domain.ResourceMobileDevice, "device held")
	s.Require().NoError(err)

	parked, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.ResourceMobileDevice, parked.BlockedResource)
	s.Nil(parked.BlockedResumeAt, "a phone frees when it frees; a countdown here would be a lie")
}

// An unparked task never reports a resume time, whatever its run history says.
// This is also what keeps the lateral join off the hot path for the boards that
// are entirely unparked tasks.
func (s *BoardTaskParkSuite) TestUnparkedTaskReportsNoResumeTime() {
	task := s.newTask(domain.TaskColumnInProgress)
	s.parkedRun(task.ID, time.Now().Add(time.Hour))

	fresh, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal("", fresh.BlockedResource)
	s.Nil(fresh.BlockedResumeAt)
}

// The sweeper's whole job: a task whose recorded reset time has passed is
// claimed back off the blocked column and its park markers are cleared, so a
// resumed run starts from a card that looks exactly like it never parked.
func (s *BoardTaskParkSuite) TestTakeQuotaResumableClaimsDueTaskAndRestoresColumn() {
	task := s.newTask(domain.TaskColumnInProgress)
	s.parkedRun(task.ID, time.Now().Add(-time.Minute))

	previous, err := s.tasks.BlockOnResource(s.ctx, s.repoID, task.ID,
		domain.ResourceClaudeCodeQuota, "usage limit reached")
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnInProgress, previous)

	taken, ok, err := s.tasks.TakeQuotaResumable(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(task.ID, taken.ID)
	s.Equal(domain.TaskColumnInProgress, taken.Column, "the task goes back to the column it was parked out of")

	restored, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnInProgress, restored.Column)
	s.Equal("", restored.BlockedResource)
	s.Nil(restored.BlockedAt)
	s.Equal(domain.TaskColumn(""), restored.BlockedOriginColumn)
}

// A park whose reset time is still in the future must not be claimed: the
// sweeper would otherwise resume a session before the quota it is waiting on
// has actually reset.
func (s *BoardTaskParkSuite) TestTakeQuotaResumableLeavesAFutureParkBlocked() {
	task := s.newTask(domain.TaskColumnInProgress)
	s.parkedRun(task.ID, time.Now().Add(time.Hour))

	_, err := s.tasks.BlockOnResource(s.ctx, s.repoID, task.ID,
		domain.ResourceClaudeCodeQuota, "usage limit reached")
	s.Require().NoError(err)

	_, ok, err := s.tasks.TakeQuotaResumable(s.ctx, time.Now())
	s.Require().NoError(err)
	s.False(ok, "the reset time has not arrived yet")

	still, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnBlocked, still.Column)
	s.Equal(domain.ResourceClaudeCodeQuota, still.BlockedResource)
}
