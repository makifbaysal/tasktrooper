package github

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenScopesReadsAClassicTokensScopes(t *testing.T) {
	withAPIBase(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "repo, admin:repo_hook, read:org")
		_, _ = w.Write([]byte(`{"login":"akif"}`))
	})

	scopes, classic, err := TokenScopes(context.Background(), "ghp_x")

	require.NoError(t, err)
	assert.True(t, classic)
	assert.Equal(t, []string{"repo", "admin:repo_hook", "read:org"}, scopes)
	assert.Equal(t, []string{"workflow"}, MissingScopes(scopes, []string{"repo", "workflow"}))
}

func TestTokenScopesCannotReadAFineGrainedToken(t *testing.T) {
	withAPIBase(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"login":"akif"}`))
	})

	scopes, classic, err := TokenScopes(context.Background(), "github_pat_x")

	require.NoError(t, err)
	assert.False(t, classic)
	assert.Empty(t, scopes)
}
