package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Exercises the exact wiring runtime.go uses (repository.Service +
// SetAttachmentStore) against a freshly migrated, real embedded Postgres
// instance, instead of the fakes repository/add_comment_test.go uses: a
// regression guard for the QA report that AddComment/ListComments did not
// surface a linked attachment against a live server.
func TestAddCommentWithAttachmentThroughRealService(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pg, err := newTestDatabase(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop() }()
	pool, err := pgxpool.New(ctx, pg.DSN())
	require.NoError(t, err)
	defer pool.Close()
	db := postgres.NewDB(pool)

	repos := postgres.NewRepositoryStore(db)
	tasks := postgres.NewBoardTaskStore(db)
	comments := postgres.NewTaskCommentStore(db)
	attachments := postgres.NewAttachmentStore(db)

	svc := repository.NewService(repos, tasks, nil, nil, nil, comments, nil, nil, nil, nil)
	svc.SetAttachmentStore(attachments)

	repo, err := repos.Create(ctx, "comment-attachment-test", "", "/tmp/comment-attachment-test-"+uuid.New().String(), "", "")
	require.NoError(t, err)
	num, err := tasks.NextTaskNumber(ctx, "task")
	require.NoError(t, err)
	task, err := tasks.Create(ctx, domain.BoardTask{
		RepositoryID: repo.ID, TaskNumber: num, Title: "comment-attachment-test", TaskType: "task",
		Column: domain.TaskColumnTodo, Priority: domain.TaskPriorityMedium, CreatedBy: "test",
	})
	require.NoError(t, err)

	att, err := attachments.Create(ctx, domain.Attachment{
		RepositoryID: &repo.ID, Filename: "report.pdf", ContentType: "application/pdf",
		SizeBytes: 3, SHA256: "x", Data: []byte("pdf"), CreatedByType: "user",
	})
	require.NoError(t, err)

	created, err := svc.AddComment(ctx, repo.ID, task.ID, domain.CreateTaskCommentRequest{
		Content:       "see attached",
		AttachmentIDs: []string{att.ID.String()},
	})
	require.NoError(t, err)
	t.Logf("AddComment response attachments: %+v", created.Attachments)
	require.Len(t, created.Attachments, 1, "AddComment must return the linked attachment")

	listed, err := svc.ListComments(ctx, repo.ID, task.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	t.Logf("ListComments response attachments: %+v", listed[0].Attachments)
	require.Len(t, listed[0].Attachments, 1, "ListComments must return the linked attachment")
}
