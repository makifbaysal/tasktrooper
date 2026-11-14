package repository

import (
	"context"
	"encoding/json"
	"fmt"
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

type fakeCommentAttachmentStore struct {
	metas map[uuid.UUID]domain.AttachmentMeta
	links map[uuid.UUID][]uuid.UUID
}

func (f *fakeCommentAttachmentStore) Create(context.Context, domain.Attachment) (domain.AttachmentMeta, error) {
	return domain.AttachmentMeta{}, nil
}

func (f *fakeCommentAttachmentStore) Get(context.Context, uuid.UUID) (domain.Attachment, error) {
	return domain.Attachment{}, nil
}

func (f *fakeCommentAttachmentStore) GetMeta(_ context.Context, id uuid.UUID) (domain.AttachmentMeta, error) {
	meta, ok := f.metas[id]
	if !ok {
		return domain.AttachmentMeta{}, fmt.Errorf("attachment not found")
	}
	return meta, nil
}

func (f *fakeCommentAttachmentStore) ListMetaByTask(context.Context, uuid.UUID) ([]domain.AttachmentMeta, error) {
	return nil, nil
}

func (f *fakeCommentAttachmentStore) ListMetaByMessageIDs(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error) {
	return nil, nil
}

func (f *fakeCommentAttachmentStore) LinkTask(context.Context, uuid.UUID, uuid.UUID, int) error { return nil }

func (f *fakeCommentAttachmentStore) UnlinkTask(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f *fakeCommentAttachmentStore) LinkMessage(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f *fakeCommentAttachmentStore) ListMetaByCommentIDs(_ context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error) {
	out := make(map[uuid.UUID][]domain.AttachmentMeta)
	for _, cID := range commentIDs {
		for _, aID := range f.links[cID] {
			out[cID] = append(out[cID], f.metas[aID])
		}
	}
	return out, nil
}

func (f *fakeCommentAttachmentStore) LinkComment(_ context.Context, commentID, attachmentID uuid.UUID) error {
	if f.links == nil {
		f.links = map[uuid.UUID][]uuid.UUID{}
	}
	f.links[commentID] = append(f.links[commentID], attachmentID)
	return nil
}

func (f *fakeCommentAttachmentStore) Delete(context.Context, uuid.UUID) error { return nil }

func TestAddComment_LinksProvidedAttachments(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	repos := &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}}
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{ID: taskID, RepositoryID: repoID}}
	comments := &fakeReleaseComments{}
	attachmentID := uuid.New()
	attachments := &fakeCommentAttachmentStore{
		metas: map[uuid.UUID]domain.AttachmentMeta{attachmentID: {ID: attachmentID, Filename: "report.pdf"}},
	}
	svc := &Service{repos: repos, tasks: tasks, comments: comments, attachments: attachments}

	created, err := svc.AddComment(context.Background(), repoID, taskID, domain.CreateTaskCommentRequest{
		Content:       "see attached",
		AttachmentIDs: []string{attachmentID.String()},
	})
	require.NoError(t, err)
	require.Len(t, created.Attachments, 1)
	require.Equal(t, "report.pdf", created.Attachments[0].Filename)
	require.Equal(t, []uuid.UUID{attachmentID}, attachments.links[created.ID])
}

func TestListComments_PopulatesAttachments(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	commentID, attachmentID := uuid.New(), uuid.New()
	repos := &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}}
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{ID: taskID, RepositoryID: repoID}}
	comments := &fakeReleaseComments{comments: []domain.TaskComment{{ID: commentID, TaskID: taskID}}}
	attachments := &fakeCommentAttachmentStore{
		metas: map[uuid.UUID]domain.AttachmentMeta{attachmentID: {ID: attachmentID, Filename: "report.pdf"}},
		links: map[uuid.UUID][]uuid.UUID{commentID: {attachmentID}},
	}
	svc := &Service{repos: repos, tasks: tasks, comments: comments, attachments: attachments}

	got, err := svc.ListComments(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Len(t, got[0].Attachments, 1)
	require.Equal(t, "report.pdf", got[0].Attachments[0].Filename)
}

// A comment created before migration 206 (or with no attachment store wired)
// must still list cleanly, with no attachments rather than an error.
func TestListComments_WithoutAttachmentStoreIsBackwardCompatible(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	commentID := uuid.New()
	repos := &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}}
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{ID: taskID, RepositoryID: repoID}}
	comments := &fakeReleaseComments{comments: []domain.TaskComment{{ID: commentID, TaskID: taskID}}}
	svc := &Service{repos: repos, tasks: tasks, comments: comments}

	got, err := svc.ListComments(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Empty(t, got[0].Attachments)
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
