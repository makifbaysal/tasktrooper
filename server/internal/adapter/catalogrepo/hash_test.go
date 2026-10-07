package catalogrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	return dir
}

func etagOf(t *testing.T, dir string) string {
	t.Helper()
	files, err := collectFiles(dir)
	require.NoError(t, err)
	etag, err := hashFiles(dir, files)
	require.NoError(t, err)
	return etag
}

// "a0.md" sorts after "a/b.md" with slashes but before "a\b.md" with
// backslashes, so this tree also pins the sort order across hosts.
var etagFixture = map[string]string{
	"agent.yaml":             "name: Reviewer\n",
	"a/b.md":                 "nested\n",
	"a0.md":                  "sibling\n",
	"skills/review/SKILL.md": "Read the diff.\nReport findings.\n",
}

func TestCollectFilesListsSlashSeparatedPathsInSlashOrder(t *testing.T) {
	files, err := collectFiles(writeTree(t, etagFixture))
	require.NoError(t, err)
	assert.Equal(t, []string{"a/b.md", "a0.md", "agent.yaml", "skills/review/SKILL.md"}, files)
}

func TestEtagIsTheSameOnEveryHost(t *testing.T) {
	assert.Equal(t, "e2dfd07817d03ef18cd6fb09c0f3909a865c743a0509865de177c7e360a0b29a", etagOf(t, writeTree(t, etagFixture)))
}

func TestEtagIgnoresCRLFLineEndings(t *testing.T) {
	crlf := map[string]string{}
	for rel, body := range etagFixture {
		crlf[rel] = strings.ReplaceAll(body, "\n", "\r\n")
	}
	assert.Equal(t, etagOf(t, writeTree(t, etagFixture)), etagOf(t, writeTree(t, crlf)))
}

func TestEtagStillChangesWithContent(t *testing.T) {
	changed := map[string]string{}
	for rel, body := range etagFixture {
		changed[rel] = body
	}
	changed["a0.md"] = "sibling, edited\n"
	assert.NotEqual(t, etagOf(t, writeTree(t, etagFixture)), etagOf(t, writeTree(t, changed)))
}
