package claudecode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assistantLine(input, cacheWrite, cacheRead int, sidechain bool) string {
	return fmt.Sprintf(`{"type":"assistant","isSidechain":%t,"message":{"role":"assistant","usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":10}}}`,
		sidechain, input, cacheWrite, cacheRead)
}

const boundaryLine = `{"type":"system","subtype":"compact_boundary","isSidechain":false,"content":"Conversation compacted"}`

// The size that matters is what the session's LAST main-agent turn was sent:
// fresh, cache-written and cache-read input together. A subagent's turns run in
// their own context and must not stand in for it, and a compaction after the
// last turn means the history the next turn reads is already small.
func TestLastContextTokens(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  int
		seen  bool
	}{
		{"empty transcript", nil, 0, false},
		{"last main turn wins", []string{assistantLine(1, 2, 3, false), assistantLine(10, 20_000, 400_000, false)}, 420_010, true},
		{"subagent turns are ignored", []string{assistantLine(5, 0, 150_000, false), assistantLine(1, 0, 900_000, true)}, 150_005, true},
		{"a compaction resets it", []string{assistantLine(0, 0, 400_000, false), boundaryLine}, 0, true},
		{"turns after a compaction count", []string{assistantLine(0, 0, 400_000, false), boundaryLine, assistantLine(0, 1_000, 60_000, false)}, 61_000, true},
		{"junk lines are skipped", []string{`not json "usage"`, assistantLine(0, 0, 120_000, false)}, 120_000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, seen := lastContextTokens(strings.NewReader(strings.Join(tc.lines, "\n")))
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.seen, seen)
		})
	}
}

// writeTranscript puts a CLI transcript for sessionID where the real CLI keeps
// it, under a config dir the test points CLAUDE_CONFIG_DIR at.
func writeTranscript(t *testing.T, sessionID string, contextTokens int) {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	dir := filepath.Join(configDir, "projects", "-some-workspace")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := assistantLine(2, 1_000, 30_000, false) + "\n" + assistantLine(2, 2_000, contextTokens-2_002, false) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte(body), 0o600))
}


// A revision that resumes a session past the threshold compacts it first —
// /compact with the focus instructions, on the same session, without the MCP
// config a local command does not need — and then resumes it with the
// revision's own prompt.
func TestRevisionCompactsALargeSessionBeforeResumingIt(t *testing.T) {
	writeTranscript(t, "sess-big-1", 420_000)
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.ResumeSessionID = "sess-big-1"
	req.Prompt = "tt-42 came back to you for revision."
	req.CompactBeforeResume = true
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, "2", readFile(t, filepath.Join(workDir, "calls.txt")), "one compaction, then the revision")

	compact := strings.Split(strings.TrimRight(readFile(t, filepath.Join(workDir, "argv.1.txt")), "\x00"), "\x00")
	assert.Equal(t, []string{"-p", "--resume", "sess-big-1"}, compact[:3])
	assert.NotContains(t, compact, "--mcp-config")
	compactPrompt := readFile(t, filepath.Join(workDir, "stdin.1.txt"))
	assert.True(t, strings.HasPrefix(compactPrompt, "/compact "), compactPrompt)
	assert.Contains(t, compactPrompt, "Drop screenshots")

	revision := strings.Split(strings.TrimRight(readFile(t, filepath.Join(workDir, "argv.2.txt")), "\x00"), "\x00")
	assert.Equal(t, []string{"-p", "--resume", "sess-big-1"}, revision[:3])
	assert.Equal(t, "tt-42 came back to you for revision.", readFile(t, filepath.Join(workDir, "stdin.2.txt")))
}

// Below the threshold the session is cheap to re-read, and a compaction would
// only add a call, latency and a lossy summary.
func TestRevisionResumesASmallSessionWithoutCompacting(t *testing.T) {
	writeTranscript(t, "sess-small-1", compactBeforeResumeThreshold-1)
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.ResumeSessionID = "sess-small-1"
	req.CompactBeforeResume = true
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "1", readFile(t, filepath.Join(workDir, "calls.txt")))
}

// Only a revision asks for it: a quota-park wake-up or a follow-up step
// (criteria sweep, review verdict) continues mid-work and keeps its context.
func TestResumeWithoutTheFlagNeverCompacts(t *testing.T) {
	writeTranscript(t, "sess-big-2", 420_000)
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.ResumeSessionID = "sess-big-2"
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "1", readFile(t, filepath.Join(workDir, "calls.txt")))
}

// No transcript (another machine, pruned, a different config dir) means the
// size is unknown; the session is resumed as it is rather than compacted blind.
func TestRevisionWithoutATranscriptResumesAsIs(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.ResumeSessionID = "sess-unknown"
	req.CompactBeforeResume = true
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "1", readFile(t, filepath.Join(workDir, "calls.txt")))
}
