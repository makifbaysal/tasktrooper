package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// progressInterval paces indexing frames: a pass reports after every file,
// and the caller needs a moving number, not one line per file.
const progressInterval = 250 * time.Millisecond

const stepIndexContext = "index_context"

var errIndexMissing = errors.New("this executor has no local code index")

func indexFailure(ctx context.Context, err error) *Failure {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		if errors.Is(context.Cause(ctx), errShuttingDown) {
			return &Failure{Code: CodeCancelled, Message: errShuttingDown.Error()}
		}
		return &Failure{Code: CodeCancelled, Message: "the index pass was cancelled: " + err.Error()}
	case errors.Is(err, port.ErrLocalIndexInvalid):
		return &Failure{Code: CodeBadRequest, Message: err.Error()}
	case errors.Is(err, port.ErrLocalIndexUnavailable), errors.Is(err, port.ErrLocalIndexNotReady):
		return &Failure{Code: CodeNotReady, Message: err.Error()}
	case errors.Is(err, port.ErrLocalIndexEmbedder):
		return &Failure{Code: CodeUpstream, Message: err.Error()}
	default:
		return &Failure{Code: CodeInternal, Message: err.Error()}
	}
}

func indexState(s port.LocalIndexState) IndexState {
	return IndexState{
		RepoKey:        s.RepoKey,
		Branch:         s.Branch,
		Status:         string(s.Status),
		Files:          s.Files,
		Chunks:         s.Chunks,
		Symbols:        s.Symbols,
		CommitSHA:      s.CommitSHA,
		EmbeddingModel: s.EmbeddingModel,
		EmbeddingDims:  s.EmbeddingDims,
		SeededFrom:     s.SeededFrom,
	}
}

// PreparedEnsure is a validated index pass holding its stream id, the way a
// PreparedRun holds its run id: a cancel names it, and shutdown ends it.
type PreparedEnsure struct {
	svc    *Service
	id     string
	req    port.LocalIndexEnsure
	ctx    context.Context
	cancel context.CancelCauseFunc
	once   sync.Once
}

func (s *Service) PrepareIndexEnsure(ctx context.Context, id string, req IndexEnsureRequest) (*PreparedEnsure, *Failure) {
	if s.deps.Index == nil {
		return nil, &Failure{Code: CodeNotReady, Message: errIndexMissing.Error()}
	}
	req.RepoKey = strings.TrimSpace(req.RepoKey)
	req.Branch = strings.TrimSpace(req.Branch)
	if req.RepoKey == "" {
		return nil, badRequest("repo_key is required")
	}
	if strings.TrimSpace(req.Workspace) == "" {
		return nil, badRequest("workspace is required: index.ensure indexes a checkout already on this computer")
	}
	dir, failure := s.resolveWorkspace(req.Workspace)
	if failure != nil {
		return nil, failure
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = "index:" + req.RepoKey + "@" + req.Branch
	}
	ensureCtx, cancel := context.WithCancelCause(ctx)
	prepared := &PreparedEnsure{
		svc: s, id: id, ctx: ensureCtx, cancel: cancel,
		req: port.LocalIndexEnsure{LocalIndexRef: port.LocalIndexRef{RepoKey: req.RepoKey, Branch: req.Branch}, Dir: dir},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		cancel(nil)
		return nil, &Failure{Code: CodeCancelled, Message: errShuttingDown.Error()}
	}
	if _, busy := s.ensures[id]; busy {
		cancel(nil)
		return nil, &Failure{Code: CodeConflict, Message: fmt.Sprintf("index pass %q is already running", id)}
	}
	s.ensures[id] = prepared
	return prepared, nil
}

func (p *PreparedEnsure) ID() string { return p.id }

func (p *PreparedEnsure) Release() {
	p.once.Do(func() {
		p.cancel(nil)
		p.svc.mu.Lock()
		if p.svc.ensures[p.id] == p {
			delete(p.svc.ensures, p.id)
		}
		p.svc.mu.Unlock()
	})
}

// throttledProgress passes on every phase change but at most one indexing
// count per progressInterval, and always the last one. The indexer reports
// the same count more than once (the pass and its store both do); a repeat is
// dropped.
func throttledProgress(emit func(*IndexProgressEvent)) func(port.LocalIndexProgress) {
	var mu sync.Mutex
	var last time.Time
	var previous port.LocalIndexProgress
	return func(pr port.LocalIndexProgress) {
		mu.Lock()
		if pr == previous {
			mu.Unlock()
			return
		}
		if pr.Phase == port.LocalIndexPhaseIndexing {
			now := time.Now()
			finished := pr.FilesTotal > 0 && pr.FilesProcessed >= pr.FilesTotal
			if !finished && pr.FilesProcessed > 0 && now.Sub(last) < progressInterval {
				mu.Unlock()
				return
			}
			last = now
		}
		previous = pr
		mu.Unlock()
		emit(&IndexProgressEvent{
			Phase: pr.Phase, FilesProcessed: pr.FilesProcessed, FilesTotal: pr.FilesTotal, SeededFrom: pr.SeededFrom,
		})
	}
}

func (p *PreparedEnsure) Execute(sink Sink) (*IndexEnsureResult, *Failure) {
	defer p.Release()
	started := time.Now()
	em := newEmitter(sink, func() { p.cancel(errCallerGone) })
	progress := throttledProgress(func(ev *IndexProgressEvent) { em.emit(EventIndexProgress, ev) })
	state, err := p.svc.deps.Index.Ensure(p.ctx, p.req, progress)
	if err != nil {
		failure := indexFailure(p.ctx, err)
		if failure.Code == CodeInternal {
			log.Warn().Err(err).Str("repo_key", p.req.RepoKey).Str("branch", p.req.Branch).Msg("executor index pass failed")
		}
		return nil, failure
	}
	return &IndexEnsureResult{IndexState: indexState(state), DurationMS: time.Since(started).Milliseconds()}, nil
}

// CancelIndexEnsure reports whether the pass streaming under id was running.
func (s *Service) CancelIndexEnsure(id string) bool {
	s.mu.Lock()
	prepared, ok := s.ensures[strings.TrimSpace(id)]
	s.mu.Unlock()
	if ok {
		prepared.cancel(errCancelRequested)
	}
	return ok
}

func (s *Service) SearchIndex(ctx context.Context, req IndexSearchRequest) (*IndexSearchResult, *Failure) {
	if s.deps.Index == nil {
		return nil, &Failure{Code: CodeNotReady, Message: errIndexMissing.Error()}
	}
	if req.K < 0 {
		return nil, badRequest("k cannot be negative")
	}
	found, err := s.deps.Index.Search(ctx, port.LocalIndexQuery{
		LocalIndexRef: port.LocalIndexRef{RepoKey: strings.TrimSpace(req.RepoKey), Branch: strings.TrimSpace(req.Branch)},
		Query:         req.Query,
		K:             req.K,
	})
	if err != nil {
		return nil, indexFailure(ctx, err)
	}
	out := &IndexSearchResult{Index: indexState(found.Index), Results: make([]IndexHit, 0, len(found.Hits))}
	for _, h := range found.Hits {
		out.Results = append(out.Results, IndexHit{
			Path: h.Path, Symbol: h.Symbol, Kind: h.Kind, Language: h.Language, Signature: h.Signature,
			StartLine: h.StartLine, EndLine: h.EndLine, Score: h.Score, Snippet: h.Snippet,
		})
	}
	return out, nil
}

// SetEmbeddings points the index at the embedding engine's new address: the
// desktop restarts its embedder on another port while the executor runs on.
func (s *Service) SetEmbeddings(req EmbeddingsRequest) (*EmbeddingsState, *Failure) {
	if s.deps.Embeddings == nil {
		return nil, &Failure{Code: CodeNotReady, Message: errIndexMissing.Error()}
	}
	if err := s.deps.Embeddings.SetEmbeddingsEndpoint(strings.TrimSpace(req.BaseURL), strings.TrimSpace(req.Source)); err != nil {
		return nil, indexFailure(context.Background(), err)
	}
	baseURL, source := s.deps.Embeddings.EmbeddingsEndpoint()
	return &EmbeddingsState{V: ProtocolVersion, BaseURL: baseURL, Source: source}, nil
}

// runModel pins a request that names no model to the run's own provider and
// model, which is what the injector's query rewrite sends.
type runModel struct {
	port.LLMClient
	provider domain.LLMProviderType
	model    string
}

func (c runModel) pin(req domain.AgentRequest) domain.AgentRequest {
	if req.ProviderType == "" {
		req.ProviderType = c.provider
	}
	if req.Model == "" {
		req.Model = c.model
	}
	return req
}

func (c runModel) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	return c.LLMClient.Chat(ctx, c.pin(req))
}

func (c runModel) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	return c.LLMClient.ChatStream(ctx, c.pin(req), onToken)
}

const (
	stepIndexEnsure = "index_ensure"

	defaultIndexWait = 3 * time.Minute
	maxIndexWait     = 30 * time.Minute
)

type ensureStep struct {
	RepoKey    string `json:"repo_key"`
	Branch     string `json:"branch,omitempty"`
	Status     string `json:"status,omitempty"`
	Files      int    `json:"files,omitempty"`
	Chunks     int    `json:"chunks,omitempty"`
	SeededFrom string `json:"seeded_from,omitempty"`
	// StillIndexing is a pass the run stopped waiting for; it goes on.
	StillIndexing bool   `json:"still_indexing,omitempty"`
	Error         string `json:"error,omitempty"`
	DurationMS    int64  `json:"duration_ms"`
}

type ensureOutcome struct {
	state port.LocalIndexState
	err   error
}

func (r *PreparedRun) indexWait() time.Duration {
	wait := time.Duration(r.spec.Index.WaitMS) * time.Millisecond
	if wait <= 0 {
		return defaultIndexWait
	}
	return min(wait, maxIndexWait)
}

// ensureIndex brings the run's index up to date from the run's own checkout
// before its context is injected: the caller sends the index with the run
// and never asks for a pass of its own. A failed pass never fails the run.
// The pass runs under the service's life, not the run's: a pass the run
// stops waiting for, or a run that ends, leaves an index the next run can
// use instead of a half-built one to start over.
func (r *PreparedRun) ensureIndex(ctx context.Context, em *emitter) {
	ref := r.spec.Index
	if ref == nil || r.workDir == "" || r.svc.deps.Index == nil {
		return
	}
	started := time.Now()
	var waiting atomic.Bool
	waiting.Store(true)
	progress := throttledProgress(func(ev *IndexProgressEvent) {
		if waiting.Load() {
			em.emit(EventIndexProgress, ev)
		}
	})
	outcome := make(chan ensureOutcome, 1)
	req := port.LocalIndexEnsure{LocalIndexRef: port.LocalIndexRef{RepoKey: ref.RepoKey, Branch: ref.Branch}, Dir: r.workDir}
	go func() {
		state, err := r.svc.deps.Index.Ensure(r.svc.life, req, progress)
		if err != nil && !waiting.Load() {
			log.Warn().Err(err).Str("repo_key", ref.RepoKey).Str("branch", ref.Branch).Msg("executor: an index pass no run waits for failed")
		}
		outcome <- ensureOutcome{state: state, err: err}
	}()

	step := ensureStep{RepoKey: ref.RepoKey, Branch: ref.Branch}
	timer := time.NewTimer(r.indexWait())
	defer timer.Stop()
	select {
	case out := <-outcome:
		if out.err != nil {
			step.Error = out.err.Error()
			break
		}
		step.Status, step.Files, step.Chunks, step.SeededFrom = string(out.state.Status), out.state.Files, out.state.Chunks, out.state.SeededFrom
	case <-timer.C:
		step.StillIndexing = true
		step.Error = "the index is still being built; this run starts without the context of this checkout"
	case <-ctx.Done():
		step.StillIndexing = true
		step.Error = context.Cause(ctx).Error()
	}
	waiting.Store(false)
	step.DurationMS = time.Since(started).Milliseconds()
	if step.Error != "" {
		log.Warn().Str("run_id", r.spec.RunID).Str("repo_key", ref.RepoKey).Str("error", step.Error).Msg("executor run: index pass did not finish; continuing")
	}
	activity.FromContext(ctx).Step(stepIndexEnsure, step)
}

type indexStep struct {
	RepoKey    string `json:"repo_key"`
	Branch     string `json:"branch,omitempty"`
	Injected   bool   `json:"injected"`
	Tools      int    `json:"tools"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// attachIndex is best effort: a run whose index cannot be read still runs,
// without the context and the index tools, and says why in a step.
func (r *PreparedRun) attachIndex(ctx context.Context, llm port.LLMClient) *port.LocalIndexAttachment {
	ref := r.spec.Index
	if ref == nil {
		return nil
	}
	started := time.Now()
	err := errIndexMissing
	if r.svc.deps.Index != nil {
		chat := runModel{LLMClient: llm, provider: domain.LLMProviderType(r.spec.Agent.ProviderID), model: r.model}
		var att port.LocalIndexAttachment
		if att, err = r.svc.deps.Index.Attach(ctx, port.LocalIndexRef{RepoKey: ref.RepoKey, Branch: ref.Branch}, chat); err == nil {
			return &att
		}
	}
	log.Warn().Err(err).Str("run_id", r.spec.RunID).Msg("executor run continues without its local index")
	activity.FromContext(ctx).Step(stepIndexContext, indexStep{
		RepoKey: ref.RepoKey, Branch: ref.Branch, Error: err.Error(), DurationMS: time.Since(started).Milliseconds(),
	})
	return nil
}

// withIndexContext adds the index context where the server's own runs put
// it: after the history for a board run, before the turn being answered for
// a chat, whose query is also rewritten by the run's model.
func (r *PreparedRun) withIndexContext(ctx context.Context, att *port.LocalIndexAttachment, messages []domain.Message) []domain.Message {
	if att == nil || att.ContextMessage == nil {
		return messages
	}
	started := time.Now()
	step := indexStep{RepoKey: r.spec.Index.RepoKey, Branch: r.spec.Index.Branch, Tools: len(att.Tools)}
	defer func() {
		step.DurationMS = time.Since(started).Milliseconds()
		activity.FromContext(ctx).Step(stepIndexContext, step)
	}()
	msg, ok, err := att.ContextMessage(ctx, messages, r.spec.Kind == KindChat)
	if err != nil {
		step.Error = err.Error()
		log.Warn().Err(err).Str("run_id", r.spec.RunID).Msg("executor run: index context injection failed, continuing without it")
		return messages
	}
	if !ok {
		return messages
	}
	step.Injected = true
	if r.spec.Kind == KindChat {
		return insertBeforeLastUser(messages, msg)
	}
	return append(append([]domain.Message(nil), messages...), msg)
}

func insertBeforeLastUser(history []domain.Message, msg domain.Message) []domain.Message {
	at := len(history)
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == domain.RoleUser {
			at = i
			break
		}
	}
	out := make([]domain.Message, 0, len(history)+1)
	out = append(out, history[:at]...)
	out = append(out, msg)
	return append(out, history[at:]...)
}

func indexContext(ctx context.Context, att *port.LocalIndexAttachment) context.Context {
	if att == nil {
		return ctx
	}
	return registry.ContextWithBranch(registry.ContextWithProjectID(ctx, att.RepositoryID), att.Branch)
}
