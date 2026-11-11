package http

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
)

// The remote listener is a bare fiber.Use, not a router, so c.Params("id") is
// always empty there. The native chat routes must read the session id from the
// matched path or every phone chat dies with "invalid session id".
func TestNativeChatRoutesReadTheSessionIDFromThePath(t *testing.T) {
	rh := newRemoteHarness(t)
	rh.h.sessionSvc = &session.Service{}
	token, _ := rh.pair(remoteaccess.RoleOperator, 0)
	const id = "7c3e2a54-98f1-4a4e-b8d2-0b1f4c6a9d10"

	status, body, _ := rh.do("GET", "/remote/v1/sessions/"+id+"/turn", token, "")
	require.Equal(t, 200, status, body)
	assert.Contains(t, body, `"state":"idle"`)

	// Uppercase is what Swift's UUID().uuidString produces.
	status, body, _ = rh.do("GET", "/remote/v1/sessions/7C3E2A54-98F1-4A4E-B8D2-0B1F4C6A9D10/turn", token, "")
	require.Equal(t, 200, status, body)

	// An empty message is refused for being empty, which proves the id parsed.
	status, body, _ = rh.do("POST", "/remote/v1/sessions/"+id+"/messages", token, `{"content":""}`)
	assert.Equal(t, 400, status, body)
	assert.Contains(t, body, "content is required")
}

func TestRemoteSessionIDOfPath(t *testing.T) {
	id, ok := remoteSessionID("/remote/v1/sessions/7c3e2a54-98f1-4a4e-b8d2-0b1f4c6a9d10/turn")
	assert.True(t, ok)
	assert.Equal(t, "7c3e2a54-98f1-4a4e-b8d2-0b1f4c6a9d10", id.String())
	_, ok = remoteSessionID("/remote/v1/sessions/nope/turn")
	assert.False(t, ok)
	_, ok = remoteSessionID("/remote/v1/changes")
	assert.False(t, ok)
}

// Every request the phone's chat screen makes, in the order it makes them
// (create, send, then the poll loop), must pass the allowlist and the native
// routes must answer with the shape the app decodes. A refusal here would show
// on the phone as a failed send although the computer did the work.
func TestPhoneChatRequestSequenceIsAnswered(t *testing.T) {
	rh := newRemoteHarness(t)
	rh.h.sessionSvc = &session.Service{}
	token, _ := rh.pair(remoteaccess.RoleOperator, 0)
	const id = "7c3e2a54-98f1-4a4e-b8d2-0b1f4c6a9d10"
	const run = "0a1b2c3d-98f1-4a4e-b8d2-0b1f4c6a9d10"

	for _, step := range []struct{ method, path, body string }{
		{"POST", "/v1/sessions", `{"agent_id":"` + run + `","title":"PM"}`},
		{"GET", "/v1/sessions/" + id, ""},
		{"GET", "/v1/sessions/" + id + "/activity?limit=1", ""},
		{"GET", "/v1/runs/" + run + "/steps", ""},
		{"GET", "/v1/runs/" + run + "/steps?since=2026-11-10T10%3A00%3A00.123456Z", ""},
		{"GET", "/remote/v1/sessions/" + id + "/turn", ""},
		{"POST", "/v1/sessions/" + id + "/cancel", `{}`},
	} {
		status, body, _ := rh.do(step.method, step.path, token, step.body)
		assert.Equal(t, 200, status, "%s %s: %s", step.method, step.path, body)
	}

	rule, ok := matchRemoteRule("POST", "/remote/v1/sessions/"+id+"/messages")
	require.True(t, ok)
	assert.Nil(t, rule.Check([]byte(`{"content":"selam"}`)))
	assert.Nil(t, rule.Check([]byte(`{"content":"selam","role":"user","stream":false}`)))

	status, body, _ := rh.do("GET", "/remote/v1/sessions/"+id+"/turn", token, "")
	require.Equal(t, 200, status, body)
	assert.JSONEq(t, `{"state":"idle","started_at":null,"finished_at":null,"error":null}`, body)
}

// A turn that starts and ends between two polls reads as idle with a finish
// time and a null error, never as running forever and never with an error.
func TestRemoteTurnTrackerSuccessShape(t *testing.T) {
	tracker := &remoteTurnTracker{turns: map[uuid.UUID]*remoteTurn{}}
	id := uuid.New()
	require.True(t, tracker.begin(id))
	assert.Equal(t, "running", tracker.get(id).State)
	assert.False(t, tracker.begin(id), "a second turn is refused while one runs")
	tracker.finish(id, nil)
	got := tracker.get(id)
	assert.Equal(t, "idle", got.State)
	assert.NotNil(t, got.FinishedAt)
	assert.Nil(t, got.Error)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"error":null`)
	assert.True(t, tracker.begin(id), "the next turn can start")
}
