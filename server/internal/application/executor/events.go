package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	toolResultContentLimit = 4000
	attachmentSizeLimit    = 2 << 20
)

type emitter struct {
	mu       sync.Mutex
	seq      int64
	sink     Sink
	broken   bool
	onBroken func()
	now      func() time.Time
}

func newEmitter(sink Sink, onBroken func()) *emitter {
	return &emitter{sink: sink, onBroken: onBroken, now: time.Now}
}

func (e *emitter) emit(kind string, ev Event) {
	e.mu.Lock()
	if e.broken {
		e.mu.Unlock()
		return
	}
	e.seq++
	h := ev.header()
	h.Seq, h.At, h.Kind = e.seq, e.now().UTC(), kind
	err := e.sink.Send(ev)
	if err != nil {
		e.broken = true
	}
	e.mu.Unlock()
	if err != nil && e.onBroken != nil {
		e.onBroken()
	}
}

func (e *emitter) step(stepType string, payload []byte) {
	data := json.RawMessage(payload)
	if !json.Valid(data) {
		data = json.RawMessage("{}")
	}
	e.emit(EventStep, &StepEvent{Step: stepType, Data: data})
}

func (e *emitter) textDelta(delta string) {
	if delta == "" {
		return
	}
	e.emit(EventText, &TextEvent{Delta: delta})
}

func (e *emitter) segmentBreak() {
	e.emit(EventText, &TextEvent{SegmentBreak: true})
}

// stepStore is the activity store a run's recorder writes through. Nothing is
// kept: each step goes to the caller as a step event, and the caller's own
// store is the record.
type stepStore struct {
	em *emitter
}

var _ port.ActivityStore = stepStore{}

func (s stepStore) CreateRun(_ context.Context, sessionID *uuid.UUID, requestID, model string) (domain.SessionRun, error) {
	return domain.SessionRun{ID: uuid.New(), SessionID: sessionID, RequestID: requestID, Model: model, StartedAt: time.Now()}, nil
}

func (s stepStore) CompleteRun(context.Context, uuid.UUID, string) error { return nil }

func (s stepStore) CancelRun(context.Context, uuid.UUID) (bool, error) { return false, nil }

func (s stepStore) RunStatus(context.Context, uuid.UUID) (string, error) { return "", nil }

func (s stepStore) AppendStep(_ context.Context, _ uuid.UUID, stepType string, payload []byte) error {
	s.em.step(stepType, payload)
	return nil
}

func (s stepStore) ListRunsBySession(context.Context, uuid.UUID, int) ([]domain.SessionRun, error) {
	return nil, nil
}

func (s stepStore) ListStepsByRun(context.Context, uuid.UUID) ([]domain.SessionStep, error) {
	return nil, nil
}

func (s stepStore) ListStepsByRunSince(context.Context, uuid.UUID, time.Time) ([]domain.SessionStep, error) {
	return nil, nil
}

func (s stepStore) ListActiveRuns(context.Context) ([]domain.SessionRun, error) { return nil, nil }

type usageMeter struct {
	mu     sync.Mutex
	totals UsageTotals
}

func (m *usageMeter) add(u domain.Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totals.LLMCalls++
	m.totals.PromptTokens += int64(u.PromptTokens)
	m.totals.CompletionTokens += int64(u.CompletionTokens)
	total := u.TotalTokens
	if total == 0 {
		total = u.PromptTokens + u.CompletionTokens
	}
	m.totals.TotalTokens += int64(total)
	m.totals.CacheReadTokens += int64(u.CacheReadTokens)
	m.totals.CacheWriteTokens += int64(u.CacheWriteTokens)
}

func (m *usageMeter) snapshot() UsageTotals {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totals
}

// meteredClient is the run's view of the model: every answered call is
// counted toward the run's totals and reported as a usage event, the
// summariser's and the wrap-up's included, since the user pays for those too.
type meteredClient struct {
	inner port.LLMClient
	meter *usageMeter
	em    *emitter
}

var _ port.LLMClient = (*meteredClient)(nil)

func (c *meteredClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	resp, err := c.inner.Chat(ctx, req)
	if err == nil {
		c.record(req, resp.Usage)
	}
	return resp, err
}

func (c *meteredClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	resp, err := c.inner.ChatStream(ctx, req, onToken)
	if err == nil {
		c.record(req, resp.Usage)
	}
	return resp, err
}

func (c *meteredClient) Models(ctx context.Context) ([]string, error) { return c.inner.Models(ctx) }

func (c *meteredClient) Embed(ctx context.Context, input, model string) ([]float32, error) {
	return c.inner.Embed(ctx, input, model)
}

func (c *meteredClient) record(req domain.AgentRequest, u domain.Usage) {
	c.meter.add(u)
	c.em.emit(EventUsage, &UsageEvent{ProviderID: string(req.ProviderType), Model: req.Model, Usage: u})
}

// eventingRegistry reports every tool call the loop makes, and where it ran,
// before and after it runs. The loop runs read-only calls side by side, so
// this is entered concurrently; the emitter serialises the frames.
type eventingRegistry struct {
	port.ToolRegistry
	sources map[string]string
	em      *emitter
}

func (r *eventingRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return r.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (r *eventingRegistry) ExecuteWithPolicy(ctx context.Context, call domain.ToolCall, policy domain.ToolPolicy) domain.ToolResult {
	source := r.sources[call.Function.Name]
	r.em.emit(EventToolCall, &ToolCallEvent{
		CallID: call.ID, Name: call.Function.Name, Source: source, Arguments: call.Function.Arguments,
	})
	started := time.Now()
	result := r.ToolRegistry.ExecuteWithPolicy(ctx, call, policy)
	content, truncated := result.Content, false
	if len(content) > toolResultContentLimit {
		content, truncated = domain.TruncateHead(content, toolResultContentLimit), true
	}
	r.em.emit(EventToolResult, &ToolResultEvent{
		CallID: call.ID, Name: call.Function.Name, Source: source,
		IsError: result.IsError, Content: content, Truncated: truncated,
		Images: len(result.Images), DurationMS: time.Since(started).Milliseconds(),
	})
	for _, img := range result.Images {
		r.em.emit(EventAttachment, attachmentEvent(call.ID, img))
	}
	return result
}

func attachmentEvent(callID string, img domain.ToolResultImage) *AttachmentEvent {
	size := base64.StdEncoding.DecodedLen(len(img.Data))
	if n := len(img.Data); n > 0 {
		for i := n - 1; i >= 0 && i >= n-2 && img.Data[i] == '='; i-- {
			size--
		}
	}
	ev := &AttachmentEvent{CallID: callID, MIME: img.MediaType, Size: size}
	if size > attachmentSizeLimit {
		ev.TooLarge = true
	} else {
		ev.DataBase64 = img.Data
	}
	return ev
}
