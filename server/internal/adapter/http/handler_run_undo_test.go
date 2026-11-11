package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeRunUndo struct {
	err     error
	gotReq  domain.RunUndoRequest
	preview domain.RunUndoPreview
}

func (f *fakeRunUndo) Preview(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (domain.RunUndoPreview, error) {
	return f.preview, f.err
}
func (f *fakeRunUndo) Undo(_ context.Context, _, _, runID uuid.UUID, req domain.RunUndoRequest) (domain.RunUndoResult, error) {
	f.gotReq = req
	return domain.RunUndoResult{RunID: runID, BackupRef: "tasktrooper/undo-backup/x"}, f.err
}
func (f *fakeRunUndo) Revert(_ context.Context, _, _, runID uuid.UUID, req domain.RunUndoRequest) (domain.RunRevertResult, error) {
	f.gotReq = req
	return domain.RunRevertResult{RunID: runID, Branch: "tasktrooper/revert-x"}, f.err
}

func (f *fakeRunUndo) ProposeRollback(_ context.Context, _, _, runID uuid.UUID, req domain.RunUndoRequest) (domain.RunRollbackProposalResult, error) {
	f.gotReq = req
	return domain.RunRollbackProposalResult{RunID: runID, Version: "v1.2.3"}, f.err
}

func undoApp(f *fakeRunUndo) *fiber.App {
	h := &Handler{runUndo: f}
	app := fiber.New()
	h.registerRunUndoRoutes(app)
	return app
}

func undoPath(suffix string) string {
	return "/v1/repositories/" + uuid.NewString() + "/tasks/" + uuid.NewString() + "/runs/" + uuid.NewString() + suffix
}

func postJSONBody(app *fiber.App, path, body string) (int, map[string]any) {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func TestRunUndoRoutes(t *testing.T) {
	f := &fakeRunUndo{preview: domain.RunUndoPreview{Supported: true, Mode: domain.UndoModeReset, BackupRef: "tasktrooper/undo-backup/x"}}
	app := undoApp(f)

	resp, err := app.Test(httptest.NewRequest("GET", undoPath("/undo/preview"), nil))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var body struct{ Preview domain.RunUndoPreview }
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.True(t, body.Preview.Supported)

	code, out := postJSONBody(app, undoPath("/undo"), `{"confirm":true,"force_push":true}`)
	require.Equal(t, 200, code)
	require.NotNil(t, out["result"])
	require.True(t, f.gotReq.Confirm)
	require.True(t, f.gotReq.ForcePush)

	code, out = postJSONBody(app, undoPath("/undo/revert"), `{"confirm":true}`)
	require.Equal(t, 200, code)
	require.NotNil(t, out["result"])

	code, out = postJSONBody(app, undoPath("/undo/rollback-proposal"), `{"confirm":true,"note":"x"}`)
	require.Equal(t, 200, code)
	require.Equal(t, "x", f.gotReq.Note)
	require.NotNil(t, out["result"])
}

func TestRunUndoMapsRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code int
	}{
		"no confirmation":      {domain.ErrUndoConfirmRequired, 400},
		"blocked":              {&domain.UndoBlockedError{Reason: domain.UndoBlockedLiveRun}, 409},
		"merged":               {domain.ErrUndoUseRevert, 409},
		"remote ahead":         {domain.ErrUndoRemoteAhead, 409},
		"no merged pr":         {domain.ErrUndoNotMerged, 409},
		"no release":           {domain.ErrUndoNoRelease, 409},
		"run missing":          {domain.ErrTaskAgentRunNotFound, 404},
		"anything else breaks": {context.DeadlineExceeded, 500},
	} {
		code, _ := postJSONBody(undoApp(&fakeRunUndo{err: tc.err}), undoPath("/undo"), `{"confirm":true}`)
		require.Equal(t, tc.code, code, name)
	}
}

func TestRunUndoIs503WithoutABoard(t *testing.T) {
	h := &Handler{}
	app := fiber.New()
	h.registerRunUndoRoutes(app)
	code, _ := postJSONBody(app, undoPath("/undo"), `{"confirm":true}`)
	require.Equal(t, 503, code)
}
