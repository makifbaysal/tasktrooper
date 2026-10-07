package domain_test

import (
	"os"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fieldKeys(fields []domain.MCPConfigField) []string {
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		keys = append(keys, f.Key)
	}
	return keys
}

func TestMissingConfigFields_SecretWithoutStoredValue(t *testing.T) {
	template, ok := domain.MCPTemplateByID("gitlab")
	require.True(t, ok)

	server := domain.MCPServer{ID: "gitlab", Env: map[string]string{
		"GITLAB_API_URL": template.Env["GITLAB_API_URL"],
	}}

	missing := domain.MissingConfigFields(server, func(string, string) bool { return false })
	assert.Equal(t, []string{"GITLAB_PERSONAL_ACCESS_TOKEN"}, fieldKeys(missing))
}

func TestMissingConfigFields_SecretOnFileIsNotMissing(t *testing.T) {
	server := domain.MCPServer{ID: "gitlab", Env: map[string]string{
		"GITLAB_API_URL": "https://gitlab.example.com/api/v4",
	}}

	missing := domain.MissingConfigFields(server, func(location, key string) bool {
		return location == "env" && key == "GITLAB_PERSONAL_ACCESS_TOKEN"
	})
	assert.Empty(t, missing)
}

func TestMissingConfigFields_UnexpandedPlaceholderCountsAsEmpty(t *testing.T) {
	server := domain.MCPServer{ID: "gitlab", Env: map[string]string{
		"GITLAB_API_URL": "${GITLAB_API_URL}",
	}}

	missing := domain.MissingConfigFields(server, func(string, string) bool { return true })
	assert.Equal(t, []string{"GITLAB_API_URL"}, fieldKeys(missing))
}

func TestMissingConfigFields_ArgsIndexPointsAtTheEditableValue(t *testing.T) {
	template, ok := domain.MCPTemplateByID("filesystem")
	require.True(t, ok)

	filled := domain.MissingConfigFields(domain.MCPServer{ID: "filesystem", Args: template.Args}, nil)
	assert.Empty(t, filled)
	assert.Equal(t, os.TempDir(), template.Args[2], "a root that exists on every OS, not a literal /tmp")

	truncated := domain.MissingConfigFields(domain.MCPServer{ID: "filesystem", Args: template.Args[:2]}, nil)
	assert.Equal(t, []string{"2"}, fieldKeys(truncated))
}

func TestMissingConfigFields_ServerWithoutTemplateHasNone(t *testing.T) {
	assert.Empty(t, domain.MissingConfigFields(domain.MCPServer{ID: "custom-thing"}, nil))
}

func TestGitHubTemplateIsTheHostedServer(t *testing.T) {
	template, ok := domain.MCPTemplateByID("github")
	require.True(t, ok)

	assert.Equal(t, "http", template.Transport)
	assert.Equal(t, "https://api.githubcopilot.com/mcp/", template.URL)
	assert.Contains(t, template.Headers, "Authorization")
}
