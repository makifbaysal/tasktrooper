package opencode

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestParseSessionExportSumsAssistantMessages(t *testing.T) {
	raw, err := os.ReadFile("testdata/export_child.json")
	require.NoError(t, err)

	u, err := parseSessionExport(raw)
	require.NoError(t, err)
	assert.Equal(t, domain.Usage{
		PromptTokens: 22720, CompletionTokens: 5085, TotalTokens: 27805,
		CacheReadTokens: 118373,
	}, u)
}

func TestParseSessionExportRejectsGarbage(t *testing.T) {
	_, err := parseSessionExport([]byte("not json"))
	assert.Error(t, err)
}

func TestChildUsageSkipsFailingSessions(t *testing.T) {
	u := childUsage(t.Context(), "/nonexistent/opencode", []string{"ses_a", "ses_b"})
	assert.Equal(t, domain.Usage{}, u)
}
