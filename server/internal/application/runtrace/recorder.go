package runtrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ctxKey struct{}

const (
	persistTimeout = 10 * time.Second
	maxHashBytes   = 16 << 20
)

type Meta struct {
	RunID         uuid.UUID
	TaskID        uuid.UUID
	AgentID       uuid.UUID
	WorkDir       string
	WorkspacePath string
	BaseSHA       string
	Branch        string
	ColumnBefore  string
	Remote        bool
	// DisplayRoot is the checkout's absolute path on the computer that holds
	// it, used only to shorten the paths a remote run's tools name; this
	// process never opens a path of a remote run.
	DisplayRoot string
	Cap         int
}

// Recorder writes a run's tool calls into the trace store. Every method is
// safe on a nil receiver, which is what a chat turn, a test and any run
// started without a trace store hold.
type Recorder struct {
	store port.RunTraceStore
	meta  Meta
	ctx   context.Context
	now   func() time.Time

	mu       sync.Mutex
	seq      int
	admitted int
	dropped  int
	open     map[string]*Call
}

func WithRecorder(ctx context.Context, r *Recorder) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, r)
}

func FromContext(ctx context.Context) *Recorder {
	r, _ := ctx.Value(ctxKey{}).(*Recorder)
	return r
}

// Start opens the run's trace: the header row is written once, and the
// sequence continues after any events a resumed run already recorded.
func Start(ctx context.Context, store port.RunTraceStore, meta Meta) *Recorder {
	if store == nil || meta.RunID == uuid.Nil {
		return nil
	}
	if meta.Cap <= 0 {
		meta.Cap = domain.MaxTraceEventsPerRun
	}
	r := &Recorder{store: store, meta: meta, ctx: ctx, now: time.Now, open: map[string]*Call{}}
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if err := store.Begin(pctx, domain.RunTrace{
		RunID: meta.RunID, TaskID: meta.TaskID, AgentID: meta.AgentID,
		BaseSHA: meta.BaseSHA, Branch: meta.Branch, WorkspacePath: meta.WorkspacePath,
		ColumnBefore: meta.ColumnBefore, Remote: meta.Remote, StartedAt: r.now(),
	}); err != nil {
		log.Warn().Err(err).Str("run_id", meta.RunID.String()).Msg("run trace: header could not be written; the run goes untraced")
		return nil
	}
	if h, err := store.Header(pctx, meta.RunID); err == nil && h.BaseSHA != "" {
		r.meta.BaseSHA = h.BaseSHA
	}
	if n, err := store.NextSeq(pctx, meta.RunID); err == nil {
		r.seq = n - 1
		r.admitted = n - 1
	}
	return r
}

func persistCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

func (r *Recorder) Meta() Meta {
	if r == nil {
		return Meta{}
	}
	return r.meta
}

func (r *Recorder) Dropped() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

type CallStart struct {
	CallID    string
	Tool      string
	Native    string
	Arguments string
	Source    string
	WorkDir   string
}

type CallResult struct {
	Content string
	IsError bool
}

type fileState struct {
	exists bool
	size   int64
	hash   string
}

// Call is one tool call between its start and its result.
type Call struct {
	r        *Recorder
	seq      int
	drop     bool
	start    CallStart
	category string
	sum      summary
	began    time.Time
	before   map[string]fileState
	root     string
}

func (r *Recorder) Begin(start CallStart) *Call {
	if r == nil || start.Tool == "subagent" {
		return nil
	}
	root := start.WorkDir
	if root == "" {
		root = r.meta.WorkDir
	}
	stateRoot, relRoot := root, root
	if r.meta.Remote {
		stateRoot, relRoot = "", r.meta.DisplayRoot
	}
	if start.Source == "" {
		start.Source = domain.TraceSourceLoop
	}
	category := Classify(start.Tool, start.Native)
	name := start.Tool
	if name == "" {
		name = start.Native
	}
	c := &Call{
		r: r, start: start, category: category, began: r.now(), root: stateRoot,
		sum: summarizeCall(category, name, start.Native, start.Arguments, relRoot),
	}
	r.mu.Lock()
	r.seq++
	c.seq = r.seq
	if r.admitted >= r.meta.Cap {
		c.drop = true
		r.dropped++
	} else {
		r.admitted++
	}
	r.mu.Unlock()
	if !c.drop && category == domain.TraceCategoryChange {
		c.before = map[string]fileState{}
		for _, p := range c.sum.paths {
			c.before[p] = stateOf(stateRoot, p)
		}
	}
	return c
}

// Open and Close pair a call by the id a CLI stream gives it.
func (r *Recorder) Open(start CallStart) {
	if r == nil || start.CallID == "" {
		return
	}
	c := r.Begin(start)
	r.mu.Lock()
	r.open[start.CallID] = c
	r.mu.Unlock()
}

func (r *Recorder) Close(callID string, res CallResult) {
	if r == nil {
		return
	}
	r.mu.Lock()
	c := r.open[callID]
	delete(r.open, callID)
	r.mu.Unlock()
	c.End(res)
}

// Abandon records the calls a stream opened and never saw a result for.
func (r *Recorder) Abandon(ctx context.Context) {
	if r == nil {
		return
	}
	r.mu.Lock()
	pending := make([]*Call, 0, len(r.open))
	for id, c := range r.open {
		pending = append(pending, c)
		delete(r.open, id)
	}
	r.mu.Unlock()
	for _, c := range pending {
		c.finish(ctx, "", domain.TraceStatusUnfinished, "the run ended before this call reported a result")
	}
}

func (c *Call) End(res CallResult) {
	if c == nil {
		return
	}
	status, errText := domain.TraceStatusOK, ""
	if res.IsError {
		status = domain.TraceStatusError
		errText = Scrub(domain.TruncateHead(firstLine(res.Content, 400), 400))
	}
	c.finish(c.r.ctx, res.Content, status, errText)
}

func (c *Call) finish(ctx context.Context, content, status, errText string) {
	if c.drop {
		return
	}
	detail := c.sum.detail
	switch c.category {
	case domain.TraceCategoryChange:
		c.describeChange(detail)
	case domain.TraceCategoryExec:
		code, output := execOutcome(content, status == domain.TraceStatusError)
		if code != nil {
			detail["exit_code"] = *code
		}
		head, tail, total := edges(output)
		if total > 0 {
			detail["output_head"] = head
			if len(tail) > 0 {
				detail["output_tail"] = tail
				detail["output_lines"] = total
			}
		}
	case domain.TraceCategoryBoard:
		if ids := resultIDs(content); len(ids) > 0 && status == domain.TraceStatusOK {
			detail["result_ids"] = ids
		}
	}
	raw, _ := json.Marshal(detail)
	ev := domain.RunTraceEvent{
		RunID: c.r.meta.RunID, TaskID: c.r.meta.TaskID, AgentID: c.r.meta.AgentID,
		Seq: c.seq, At: c.began, DurationMS: c.r.now().Sub(c.began).Milliseconds(),
		Tool: firstNonEmpty(c.start.Tool, c.start.Native), Source: c.start.Source, Category: c.category,
		Target: domain.TruncateHead(c.sum.target, 300), Status: status, Error: errText, Detail: raw,
	}
	if c.start.Native != "" && c.start.Native != ev.Tool {
		ev.NativeTool = c.start.Native
	}
	c.r.append(ctx, ev)
}

func (c *Call) describeChange(detail map[string]any) {
	if len(c.sum.paths) == 0 {
		return
	}
	op, _ := detail["op"].(string)
	target := c.sum.paths[len(c.sum.paths)-1]
	if op == "move" && len(c.sum.paths) == 2 {
		target = c.sum.paths[1]
	}
	subject := c.sum.paths[0]
	b := c.before[subject]
	a := stateOf(c.root, target)
	if op == "move" {
		a = stateOf(c.root, target)
	}
	detail["existed_before"] = b.exists
	if b.hash != "" {
		detail["hash_before"] = b.hash
	}
	detail["size_before"] = b.size
	if a.exists {
		if a.hash != "" {
			detail["hash_after"] = a.hash
		}
		detail["size_after"] = a.size
		detail["size_delta"] = a.size - b.size
	} else {
		detail["size_after"] = int64(0)
		detail["size_delta"] = -b.size
	}
	detail["changed"] = b.hash != a.hash || b.exists != a.exists
}

func (r *Recorder) append(ctx context.Context, ev domain.RunTraceEvent) {
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if err := r.store.Append(pctx, []domain.RunTraceEvent{ev}); err != nil {
		log.Debug().Err(err).Str("run_id", r.meta.RunID.String()).Int("seq", ev.Seq).Msg("run trace: event could not be written")
	}
}

// Note appends an event the system itself produced (an undo), after the
// run's calls.
func (r *Recorder) Note(ctx context.Context, ev domain.RunTraceEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.seq++
	ev.Seq = r.seq
	r.mu.Unlock()
	ev.RunID, ev.TaskID, ev.AgentID = r.meta.RunID, r.meta.TaskID, r.meta.AgentID
	if ev.At.IsZero() {
		ev.At = r.now()
	}
	r.append(ctx, ev)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func stateOf(root, rel string) fileState {
	if rel == "" || root == "" {
		return fileState{}
	}
	abs := rel
	if !filepath.IsAbs(rel) {
		abs = filepath.Join(root, rel)
	}
	if !within(root, abs) {
		return fileState{}
	}
	info, err := os.Lstat(abs)
	if err != nil || info.IsDir() {
		return fileState{}
	}
	st := fileState{exists: true, size: info.Size()}
	if info.Mode().IsRegular() && info.Size() <= maxHashBytes {
		if f, err := os.Open(abs); err == nil {
			h := sha256.New()
			if _, err := io.Copy(h, f); err == nil {
				st.hash = hex.EncodeToString(h.Sum(nil))
			}
			_ = f.Close()
		}
	}
	return st
}

func within(root, abs string) bool {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
