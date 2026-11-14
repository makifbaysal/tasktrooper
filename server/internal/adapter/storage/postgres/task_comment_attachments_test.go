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

// TaskCommentAttachmentsSuite covers migration 206: linking an already
// uploaded attachment to a task comment and reading it back by comment id.
type TaskCommentAttachmentsSuite struct {
	suite.Suite
	ctx         context.Context
	cancel      context.CancelFunc
	pg          *database.Embedded
	pool        *pgxpool.Pool
	db          *postgres.DB
	tasks       *postgres.BoardTaskStore
	comments    *postgres.TaskCommentStore
	attachments *postgres.AttachmentStore
	repoID      uuid.UUID
}

func TestTaskCommentAttachmentsSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(TaskCommentAttachmentsSuite))
}

func (s *TaskCommentAttachmentsSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.comments = postgres.NewTaskCommentStore(s.db)
	s.attachments = postgres.NewAttachmentStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "task-comment-attachments-test", "", "/tmp/task-comment-attachments-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *TaskCommentAttachmentsSuite) TearDownSuite() {
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

func (s *TaskCommentAttachmentsSuite) newTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "task-comment-attachments-test",
		TaskType:     "task",
		Column:       domain.TaskColumnTodo,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task
}

func (s *TaskCommentAttachmentsSuite) TestLinkedAttachmentIsListedByCommentID() {
	task := s.newTask()
	comment, err := s.comments.Create(s.ctx, domain.TaskComment{
		TaskID: task.ID, AuthorType: "user", Content: "see attached",
	})
	s.Require().NoError(err)

	att, err := s.attachments.Create(s.ctx, domain.Attachment{
		RepositoryID: &s.repoID, Filename: "report.pdf", ContentType: "application/pdf",
		SizeBytes: 3, SHA256: "x", Data: []byte("pdf"), CreatedByType: "user",
	})
	s.Require().NoError(err)

	s.Require().NoError(s.attachments.LinkComment(s.ctx, comment.ID, att.ID))

	metas, err := s.attachments.ListMetaByCommentIDs(s.ctx, []uuid.UUID{comment.ID})
	s.Require().NoError(err)
	s.Require().Len(metas[comment.ID], 1)
	s.Equal("report.pdf", metas[comment.ID][0].Filename)
}

func (s *TaskCommentAttachmentsSuite) TestCommentWithoutAttachmentsListsEmpty() {
	task := s.newTask()
	comment, err := s.comments.Create(s.ctx, domain.TaskComment{
		TaskID: task.ID, AuthorType: "user", Content: "no files here",
	})
	s.Require().NoError(err)

	metas, err := s.attachments.ListMetaByCommentIDs(s.ctx, []uuid.UUID{comment.ID})
	s.Require().NoError(err)
	s.Empty(metas[comment.ID])
}
