package runtrace_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace/runtracetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func newRecorder(t *testing.T, dir string, cap int) (*runtrace.Recorder, *runtracetest.MemStore) {
	t.Helper()
	store := runtracetest.NewMemStore()
	rec := runtrace.Start(context.Background(), store, runtrace.Meta{
		RunID: uuid.New(), TaskID: uuid.New(), AgentID: uuid.New(), WorkDir: dir, WorkspacePath: dir, BaseSHA: "base", Cap: cap,
	})
	require.NotNil(t, rec)
	return rec, store
}

func detail(t *testing.T, ev domain.RunTraceEvent) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(ev.Detail, &m))
	return m
}

func TestNilRecorderIsANoOp(t *testing.T) {
	var rec *runtrace.Recorder
	call := rec.Begin(runtrace.CallStart{Tool: "read_file"})
	call.End(runtrace.CallResult{})
	rec.Open(runtrace.CallStart{CallID: "x"})
	rec.Close("x", runtrace.CallResult{})
	rec.Abandon(context.Background())
	require.Nil(t, runtrace.Start(context.Background(), nil, runtrace.Meta{RunID: uuid.New()}))
	require.Nil(t, runtrace.FromContext(context.Background()))
}

func TestReadRecordsPathAndRangeNotContents(t *testing.T) {
	rec, store := newRecorder(t, t.TempDir(), 0)
	call := rec.Begin(runtrace.CallStart{Tool: "read_file", Arguments: `{"path":"src/a.go","offset":10,"limit":40}`})
	call.End(runtrace.CallResult{Content: "SECRET FILE BODY"})
	ev := store.BySeq(1)
	require.Equal(t, domain.TraceCategoryRead, ev.Category)
	require.Equal(t, "src/a.go", ev.Target)
	d := detail(t, ev)
	require.EqualValues(t, 10, d["offset"])
	require.EqualValues(t, 40, d["limit"])
	require.NotContains(t, string(ev.Detail), "SECRET FILE BODY")
}

func TestChangeRecordsHashesAndSizeDelta(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello"), 0o644))
	rec, store := newRecorder(t, dir, 0)

	call := rec.Begin(runtrace.CallStart{Tool: "write_file", Arguments: `{"path":"f.txt","content":"hello world and more"}`})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello world"), 0o644))
	call.End(runtrace.CallResult{Content: "ok"})

	ev := store.BySeq(1)
	d := detail(t, ev)
	require.Equal(t, "write", d["op"])
	require.NotEmpty(t, d["hash_before"])
	require.NotEmpty(t, d["hash_after"])
	require.NotEqual(t, d["hash_before"], d["hash_after"])
	require.EqualValues(t, 6, d["size_delta"])
	require.Equal(t, true, d["changed"])
	require.NotContains(t, string(ev.Detail), "and more")
}

func TestChangeDeleteAndCreate(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("12345"), 0o644))
	rec, store := newRecorder(t, dir, 0)
	del := rec.Begin(runtrace.CallStart{Tool: "delete_file", Arguments: `{"path":"gone.txt"}`})
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.txt")))
	del.End(runtrace.CallResult{})
	d := detail(t, store.BySeq(1))
	require.EqualValues(t, -5, d["size_delta"])
	require.Equal(t, true, d["existed_before"])

	cr := rec.Begin(runtrace.CallStart{Tool: "write_file", Arguments: `{"path":"new.txt"}`})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("abc"), 0o644))
	cr.End(runtrace.CallResult{})
	d = detail(t, store.BySeq(2))
	require.Equal(t, false, d["existed_before"])
	require.EqualValues(t, 3, d["size_delta"])
}

func TestPathsOutsideTheWorkspaceAreNeverHashed(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(outside, []byte("zzz"), 0o644))
	rec, store := newRecorder(t, dir, 0)
	call := rec.Begin(runtrace.CallStart{Tool: "edit_file", Arguments: `{"path":"` + outside + `"}`})
	call.End(runtrace.CallResult{})
	require.NotContains(t, string(store.BySeq(1).Detail), "hash_before")
}

func TestExecScrubsCommandAndOutputAndKeepsEdges(t *testing.T) {
	rec, store := newRecorder(t, t.TempDir(), 0)
	var out []string
	for i := 0; i < 100; i++ {
		out = append(out, "line "+string(rune('a'+i%26)))
	}
	out[3] = "token: ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	call := rec.Begin(runtrace.CallStart{Tool: "run_terminal",
		Arguments: `{"command":"curl -H 'Authorization: Bearer abcdef1234567890' https://u:pw@example.com/x --password hunter2xyz"}`})
	call.End(runtrace.CallResult{Content: strings.Join(out, "\n")})

	ev := store.BySeq(1)
	require.Equal(t, domain.TraceCategoryExec, ev.Category)
	raw := string(ev.Detail)
	for _, leak := range []string{"abcdef1234567890", "pw@", "hunter2xyz", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"} {
		require.NotContains(t, raw, leak)
	}
	require.Contains(t, raw, "[redacted]")
	d := detail(t, ev)
	require.Len(t, d["output_head"], 20)
	require.Len(t, d["output_tail"], 20)
	require.EqualValues(t, 100, d["output_lines"])
	require.EqualValues(t, 0, d["exit_code"])
	require.Equal(t, ".", d["cwd"])
}

func TestExecFailureRecordsExitCodeAndError(t *testing.T) {
	rec, store := newRecorder(t, t.TempDir(), 0)
	call := rec.Begin(runtrace.CallStart{Tool: "run_terminal", Arguments: `{"command":"go test ./..."}`})
	call.End(runtrace.CallResult{IsError: true, Content: "exit error: exit status 2\noutput:\nFAIL pkg"})
	ev := store.BySeq(1)
	require.Equal(t, domain.TraceStatusError, ev.Status)
	require.NotEmpty(t, ev.Error)
	d := detail(t, ev)
	require.EqualValues(t, 2, d["exit_code"])
	require.Equal(t, []any{"FAIL pkg"}, d["output_head"])
}

func TestShellWithExternalEffectIsFlagged(t *testing.T) {
	rec, store := newRecorder(t, t.TempDir(), 0)
	rec.Begin(runtrace.CallStart{Tool: "run_terminal", Arguments: `{"command":"git status && git push origin HEAD"}`}).End(runtrace.CallResult{})
	rec.Begin(runtrace.CallStart{Tool: "run_terminal", Arguments: `{"command":"go build ./..."}`}).End(runtrace.CallResult{})
	require.Equal(t, runtrace.EffectShellOuter, detail(t, store.BySeq(1))["effect"])
	require.Nil(t, detail(t, store.BySeq(2))["effect"])
}

func TestExternalKeepsHostNotBodyOrCredentials(t *testing.T) {
	rec, store := newRecorder(t, t.TempDir(), 0)
	rec.Begin(runtrace.CallStart{Tool: "http_request", Arguments: `{"method":"POST","url":"https://user:pw@api.example.com/v1/x?token=abc","headers":{"Authorization":"Bearer zzzzzzzzzz"},"body":"{\"a\":1}"}`}).End(runtrace.CallResult{})
	rec.Begin(runtrace.CallStart{Tool: "http_request", Arguments: `{"url":"https://docs.example.com/"}`}).End(runtrace.CallResult{})
	ev := store.BySeq(1)
	require.Equal(t, domain.TraceCategoryExternal, ev.Category)
	require.Equal(t, "api.example.com", ev.Target)
	raw := string(ev.Detail)
	for _, leak := range []string{"zzzzzzzzzz", "pw", "token=abc", `{\"a\":1}`} {
		require.NotContains(t, raw, leak)
	}
	require.Equal(t, runtrace.EffectHTTP, detail(t, ev)["effect"])
	require.Nil(t, detail(t, store.BySeq(2))["effect"])
}

func TestMCPAndBrowserAndBoard(t *testing.T) {
	rec, store := newRecorder(t, t.TempDir(), 0)
	rec.Open(runtrace.CallStart{CallID: "1", Tool: "mcp__github__create_issue", Native: "mcp__github__create_issue", Arguments: `{"title":"x"}`, Source: domain.TraceSourceCLI})
	rec.Close("1", runtrace.CallResult{})
	rec.Begin(runtrace.CallStart{Tool: "browser_click", Arguments: `{"selector":"#a"}`}).End(runtrace.CallResult{})
	rec.Begin(runtrace.CallStart{Tool: "create_board_task", Arguments: `{"title":"secret body","repository_id":"r1"}`}).End(runtrace.CallResult{Content: `{"id":"t-9","key":"TT-9"}`})
	rec.Begin(runtrace.CallStart{Tool: "add_task_comment", Arguments: `{"task_id":"t-9","content":"long comment"}`}).End(runtrace.CallResult{})

	require.Equal(t, runtrace.EffectMCP, detail(t, store.BySeq(1))["effect"])
	require.Equal(t, "github", detail(t, store.BySeq(1))["mcp_server"])
	require.Equal(t, runtrace.EffectBrowser, detail(t, store.BySeq(2))["effect"])
	board := store.BySeq(3)
	require.Equal(t, domain.TraceCategoryBoard, board.Category)
	require.NotContains(t, string(board.Detail), "secret body")
	require.Contains(t, string(board.Detail), "TT-9")
	require.Equal(t, runtrace.EffectBoardNote, detail(t, store.BySeq(4))["effect"])
	require.NotContains(t, string(store.BySeq(4).Detail), "long comment")
}

func TestEventCapDropsAndCounts(t *testing.T) {
	rec, store := newRecorder(t, t.TempDir(), 3)
	for i := 0; i < 7; i++ {
		rec.Begin(runtrace.CallStart{Tool: "read_file", Arguments: `{"path":"a"}`}).End(runtrace.CallResult{})
	}
	events, err := store.List(context.Background(), rec.Meta().RunID, port.RunTraceFilter{})
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, 4, rec.Dropped())
}

func TestResumedRunContinuesTheSequence(t *testing.T) {
	store := runtracetest.NewMemStore()
	meta := runtrace.Meta{RunID: uuid.New(), TaskID: uuid.New(), AgentID: uuid.New(), WorkDir: t.TempDir(), BaseSHA: "first"}
	first := runtrace.Start(context.Background(), store, meta)
	first.Begin(runtrace.CallStart{Tool: "read_file", Arguments: `{"path":"a"}`}).End(runtrace.CallResult{})
	meta.BaseSHA = "second"
	second := runtrace.Start(context.Background(), store, meta)
	second.Begin(runtrace.CallStart{Tool: "read_file", Arguments: `{"path":"b"}`}).End(runtrace.CallResult{})
	require.Equal(t, 2, store.BySeq(2).Seq)
	require.Equal(t, "first", second.Meta().BaseSHA)
	h, _ := store.Header(context.Background(), meta.RunID)
	require.Equal(t, "first", h.BaseSHA)
}

func TestAbandonRecordsUnfinishedCalls(t *testing.T) {
	rec, store := newRecorder(t, t.TempDir(), 0)
	rec.Open(runtrace.CallStart{CallID: "z", Tool: "run_terminal", Arguments: `{"command":"sleep 100"}`})
	rec.Abandon(context.Background())
	require.Equal(t, domain.TraceStatusUnfinished, store.BySeq(1).Status)
}

func TestScrubKeepsOrdinaryText(t *testing.T) {
	for _, s := range []string{"go test ./... -count=1", "max_tokens=5", "token_count 4", "git log --oneline"} {
		require.Equal(t, s, runtrace.Scrub(s))
	}
	require.NotContains(t, runtrace.Scrub("export GITHUB_TOKEN=abc123xyz"), "abc123xyz")
	require.NotContains(t, runtrace.Scrub("-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----"), "MIIabc")
	require.NotContains(t, runtrace.Scrub("npm config set //registry/:_authToken=npm_abcdefghijklmnopqrstuvwxyz0123456789"), "npm_abcdef")
}

type ctxKey string

type ctxStore struct {
	*runtracetest.MemStore
	seen []any
}

func (c *ctxStore) Append(ctx context.Context, evs []domain.RunTraceEvent) error {
	c.seen = append(c.seen, ctx.Value(ctxKey("tenant")))
	return c.MemStore.Append(ctx, evs)
}

func TestEventsAreWrittenUnderTheRunsOwnContext(t *testing.T) {
	store := &ctxStore{MemStore: runtracetest.NewMemStore()}
	ctx := context.WithValue(context.Background(), ctxKey("tenant"), "t-1")
	rec := runtrace.Start(ctx, store, runtrace.Meta{RunID: uuid.New(), TaskID: uuid.New(), AgentID: uuid.New(), WorkDir: t.TempDir()})
	rec.Begin(runtrace.CallStart{Tool: "read_file", Arguments: `{"path":"a"}`}).End(runtrace.CallResult{})
	rec.Open(runtrace.CallStart{CallID: "z", Tool: "run_terminal", Arguments: `{"command":"ls"}`})
	rec.Close("z", runtrace.CallResult{})
	require.Equal(t, []any{"t-1", "t-1"}, store.seen)
}

func TestRemoteRunsNeverTouchThisFilesystem(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("here"), 0o644))
	store := runtracetest.NewMemStore()
	rec := runtrace.Start(context.Background(), store, runtrace.Meta{
		RunID: uuid.New(), TaskID: uuid.New(), AgentID: uuid.New(), WorkDir: dir, Remote: true, DisplayRoot: "/Users/m/work/repo",
	})
	rec.Begin(runtrace.CallStart{Tool: "edit_file", WorkDir: dir, Arguments: `{"path":"/Users/m/work/repo/src/a.go"}`}).End(runtrace.CallResult{})
	rec.Begin(runtrace.CallStart{Tool: "edit_file", WorkDir: dir, Arguments: `{"path":"f.txt"}`}).End(runtrace.CallResult{})
	first, second := store.BySeq(1), store.BySeq(2)
	require.Equal(t, "src/a.go", first.Target)
	require.NotContains(t, string(second.Detail), "hash_before")
	require.NotContains(t, string(second.Detail), "hash_after")
}
