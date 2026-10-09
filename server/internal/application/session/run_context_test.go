package session_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The run context is built before the turn picks the agent loop or an agent
// CLI, and an agent CLI's MCP tool calls execute on it. Issue sync relies on
// both halves: a task create_board_task opens during an import's conversion
// chat is linked by the session found here, whatever the provider.
func TestRepositoryChatRunCarriesItsSessionAndRepository(t *testing.T) {
	repositoryID := uuid.New()
	sess := domain.Session{ID: uuid.New(), ProjectID: &repositoryID}

	_, runCtx, _, err := session.ResolveRunWorkspaceForTest(
		context.Background(), &workspaceStore{}, mirrorRepos{root: t.TempDir()}, &stubTaskWorkspaces{}, sess,
		domain.AppSettings{WorkspaceRoot: t.TempDir(), DefaultLanguage: "en"},
		[]domain.Message{{Role: domain.RoleUser, Content: "turn issue acme/widget#7 into tasks"}},
	)
	require.NoError(t, err)

	assert.Equal(t, sess.ID, registry.SessionIDFromContext(runCtx))
	assert.Equal(t, repositoryID, registry.RepositoryIDFromContext(runCtx))
}
