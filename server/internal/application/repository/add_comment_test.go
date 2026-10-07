package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestAddCommentStampsActorUserID(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	repos := &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}}
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{ID: taskID, RepositoryID: repoID}}

	t.Run("actor uid in context is stamped on the comment", func(t *testing.T) {
		comments := &fakeReleaseComments{}
		svc := &Service{repos: repos, tasks: tasks, comments: comments}
		ctx := registry.ContextWithActorUserID(context.Background(), "firebase-uid-123")

		created, err := svc.AddComment(ctx, repoID, taskID, domain.CreateTaskCommentRequest{Content: "looks good"})
		if err != nil {
			t.Fatalf("AddComment: %v", err)
		}
		if created.ActorUserID == nil || *created.ActorUserID != "firebase-uid-123" {
			t.Fatalf("want actor_user_id %q, got %v", "firebase-uid-123", created.ActorUserID)
		}
		if len(comments.comments) != 1 || comments.comments[0].ActorUserID == nil || *comments.comments[0].ActorUserID != "firebase-uid-123" {
			t.Fatalf("comment store did not receive actor_user_id: %+v", comments.comments)
		}
	})

	t.Run("no actor in context leaves actor_user_id nil", func(t *testing.T) {
		comments := &fakeReleaseComments{}
		svc := &Service{repos: repos, tasks: tasks, comments: comments}

		created, err := svc.AddComment(context.Background(), repoID, taskID, domain.CreateTaskCommentRequest{Content: "still works"})
		if err != nil {
			t.Fatalf("AddComment: %v", err)
		}
		if created.ActorUserID != nil {
			t.Fatalf("want nil actor_user_id, got %v", *created.ActorUserID)
		}
	})
}

type commentEventStore struct {
	events []domain.BoardEvent
}

func (s *commentEventStore) Create(_ context.Context, event domain.BoardEvent) (domain.BoardEvent, error) {
	event.ID = uuid.New()
	s.events = append(s.events, event)
	return event, nil
}

func (s *commentEventStore) ListRecent(context.Context, int) ([]domain.BoardEvent, error) {
	return s.events, nil
}

func (s *commentEventStore) ListByTask(context.Context, uuid.UUID, int) ([]domain.BoardEvent, error) {
	return s.events, nil
}

type commentEnqueuer struct{ jobs []board.RunJob }

func (e *commentEnqueuer) Enqueue(job board.RunJob) { e.jobs = append(e.jobs, job) }

// The dispatcher is built without a board config or run store: an
// informational comment must be recorded and stop before either is consulted.
func TestAnInformationalCommentIsRecordedWithoutDispatching(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	events, runner := &commentEventStore{}, &commentEnqueuer{}
	svc := &Service{
		repos:      &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:      &fakeReleaseTaskStore{task: domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnCodeReview}},
		comments:   &fakeReleaseComments{},
		dispatcher: board.NewDispatcher(nil, events, nil, runner, true),
	}

	_, err := svc.AddComment(context.Background(), repoID, taskID, domain.CreateTaskCommentRequest{
		AuthorType: "system", Content: "[advisory checks] coverage 50%", Informational: true,
	})

	require.NoError(t, err)
	require.Len(t, events.events, 1)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(events.events[0].Payload, &payload))
	require.Equal(t, true, payload[domain.EventPayloadInformational])
	require.Empty(t, runner.jobs)
}
