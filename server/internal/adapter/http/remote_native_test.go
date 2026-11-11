package http

import (
	"testing"

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
