package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanEnvExamplesReadsNamesValuesAndWhereTheyLive(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	write(".env.example", "# admin\nADMIN_PASSWORD_HASH=\nexport GITHUB_REPO=\"o/r\"\nGITHUB_BRANCH='master'\nGITHUB_REPO=dup\n")
	write("apps/web/.env.sample", "API_URL=https://api.example.com\n")
	write("node_modules/pkg/.env.example", "IGNORED=1\n")

	got := scanEnvExamples(root)

	byName := map[string]string{}
	paths := map[string]string{}
	for _, ex := range got {
		byName[ex.Name] = ex.Value
		paths[ex.Name] = ex.Path
	}
	assert.Equal(t, map[string]string{
		"ADMIN_PASSWORD_HASH": "",
		"GITHUB_REPO":         "o/r",
		"GITHUB_BRANCH":       "master",
		"API_URL":             "https://api.example.com",
	}, byName)
	assert.Equal(t, filepath.Join("apps", "web", ".env.sample"), paths["API_URL"])
}
