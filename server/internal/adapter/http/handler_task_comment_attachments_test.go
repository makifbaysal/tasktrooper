package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type tcaAttachmentStore struct {
	metas map[uuid.UUID]domain.AttachmentMeta
	links map[uuid.UUID][]uuid.UUID
}

func (f *tcaAttachmentStore) Create(context.Context, domain.Attachment) (domain.AttachmentMeta, error) {
	return domain.AttachmentMeta{}, nil
}
func (f *tcaAttachmentStore) Get(context.Context, uuid.UUID) (domain.Attachment, error) {
	return domain.Attachment{}, nil
}
func (f *tcaAttachmentStore) GetMeta(_ context.Context, id uuid.UUID) (domain.AttachmentMeta, error) {
	meta, ok := f.metas[id]
	if !ok {
		return domain.AttachmentMeta{}, fmt.Errorf("attachment not found")
	}
	return meta, nil
}
func (f *tcaAttachmentStore) ListMetaByTask(context.Context, uuid.UUID) ([]domain.AttachmentMeta, error) {
	return nil, nil
}
func (f *tcaAttachmentStore) ListMetaByMessageIDs(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error) {
	return nil, nil
}
func (f *tcaAttachmentStore) LinkTask(context.Context, uuid.UUID, uuid.UUID, int) error   { return nil }
func (f *tcaAttachmentStore) UnlinkTask(context.Context, uuid.UUID, uuid.UUID) error      { return nil }
func (f *tcaAttachmentStore) LinkMessage(context.Context, uuid.UUID, uuid.UUID) error     { return nil }
func (f *tcaAttachmentStore) ListMetaByCommentIDs(_ context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error) {
	out := make(map[uuid.UUID][]domain.AttachmentMeta)
	for _, cID := range commentIDs {
		for _, aID := range f.links[cID] {
			out[cID] = append(out[cID], f.metas[aID])
		}
	}
	return out, nil
}
func (f *tcaAttachmentStore) LinkComment(_ context.Context, commentID, attachmentID uuid.UUID) error {
	if f.links == nil {
		f.links = map[uuid.UUID][]uuid.UUID{}
	}
	f.links[commentID] = append(f.links[commentID], attachmentID)
	return nil
}
func (f *tcaAttachmentStore) Delete(context.Context, uuid.UUID) error { return nil }

var _ port.AttachmentStore = (*tcaAttachmentStore)(nil)

// Reproduces the QA report over the real Fiber routes (not the service
// directly): POST a comment with attachment_ids, then GET the comment list,
// exactly the sequence curl/the MCP http client used.
func TestCreateAndListTaskComment_AttachmentSurfacesOverHTTP(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	tasks := &annTaskStore{tasks: map[uuid.UUID]domain.BoardTask{taskID: {
		ID: taskID, RepositoryID: repoID, Key: "T-1", TaskType: "task", Column: domain.TaskColumnTodo,
	}}}
	comments := &annCommentStore{}
	attachmentID := uuid.New()
	attachments := &tcaAttachmentStore{
		metas: map[uuid.UUID]domain.AttachmentMeta{attachmentID: {ID: attachmentID, Filename: "report.pdf"}},
	}
	repos := newFakeDeployOpsRepositoryStore([]domain.Repository{{ID: repoID, Name: "tasktrooper"}})
	svc := repository.NewService(repos, tasks, nil, nil, nil, comments, nil, nil, nil, nil)
	svc.SetAttachmentStore(attachments)

	h := &Handler{repositorySvc: svc}
	app := fiber.New()
	h.registerRepositoryRoutes(app)

	base := "/v1/repositories/" + repoID.String() + "/tasks/" + taskID.String()
	doReq := func(method, path string, body any) (int, []byte) {
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			reader = bytes.NewReader(raw)
		}
		req := httptest.NewRequest(method, path, reader)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := app.Test(req, 5000)
		require.NoError(t, err)
		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, out
	}

	status, raw := doReq("POST", base+"/comments", map[string]any{
		"content": "see attached", "attachment_ids": []string{attachmentID.String()},
	})
	require.Equal(t, fiber.StatusCreated, status, string(raw))
	var created domain.TaskComment
	require.NoError(t, json.Unmarshal(raw, &created))
	require.Len(t, created.Attachments, 1, "POST response: %s", raw)
	require.Equal(t, "report.pdf", created.Attachments[0].Filename)

	status, raw = doReq("GET", base+"/comments", nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var listed struct {
		Comments []domain.TaskComment `json:"comments"`
	}
	require.NoError(t, json.Unmarshal(raw, &listed))
	require.Len(t, listed.Comments, 1)
	require.Len(t, listed.Comments[0].Attachments, 1, "GET response: %s", raw)
}
