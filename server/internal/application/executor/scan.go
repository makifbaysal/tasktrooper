package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A repository scan of a checkout on this computer: the caller keeps the
// project model, and what it needs of the code (components, checks, links,
// deploy signals, git facts) is read here and leaves as the scan's result.

const EventScanStage = "scan_stage"

const defaultScanTimeout = 10 * time.Minute

var errScanMissing = errors.New("this executor has no repository scanner")

type ScanRequest struct {
	Workspace string `json:"workspace"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

// ScanStageEvent is one domain.ScanEvent: the stage, whether it finished, and
// its summary. The header's at is when the scanner reported it.
type ScanStageEvent struct {
	EventHeader
	Stage   domain.ScanStage `json:"stage"`
	Done    bool             `json:"done"`
	Summary string           `json:"summary,omitempty"`
}

// PreparedScan is a validated scan holding its stream id and its checkout:
// one scan of a checkout at a time, since a second would read the same tree
// for the same answer.
type PreparedScan struct {
	svc     *Service
	id      string
	dir     string
	timeout time.Duration
	ctx     context.Context
	cancel  context.CancelCauseFunc
	once    sync.Once
}

func (s *Service) PrepareScan(ctx context.Context, id string, req ScanRequest) (*PreparedScan, *Failure) {
	if s.deps.Scanner == nil {
		return nil, &Failure{Code: CodeNotReady, Message: errScanMissing.Error()}
	}
	if strings.TrimSpace(req.Workspace) == "" {
		return nil, badRequest("workspace is required: scan reads a checkout already on this computer")
	}
	dir, failure := s.resolveWorkspace(req.Workspace)
	if failure != nil {
		return nil, failure
	}
	if req.TimeoutMS < 0 {
		return nil, badRequest("timeout_ms cannot be negative")
	}
	timeout := defaultScanTimeout
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = "scan:" + strings.TrimSpace(req.Workspace)
	}
	scanCtx, cancel := context.WithCancelCause(ctx)
	prepared := &PreparedScan{svc: s, id: id, dir: dir, timeout: timeout, ctx: scanCtx, cancel: cancel}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		cancel(nil)
		return nil, &Failure{Code: CodeCancelled, Message: errShuttingDown.Error()}
	}
	if _, busy := s.scans[id]; busy {
		cancel(nil)
		return nil, &Failure{Code: CodeConflict, Message: fmt.Sprintf("scan %q is already running", id)}
	}
	for _, other := range s.scans {
		if other.dir == dir {
			cancel(nil)
			return nil, &Failure{Code: CodeConflict, Message: fmt.Sprintf("workspace %q is already being scanned (%s)", req.Workspace, other.id)}
		}
	}
	s.scans[id] = prepared
	return prepared, nil
}

func (p *PreparedScan) ID() string { return p.id }

func (p *PreparedScan) Release() {
	p.once.Do(func() {
		p.cancel(nil)
		p.svc.mu.Lock()
		if p.svc.scans[p.id] == p {
			delete(p.svc.scans, p.id)
		}
		p.svc.mu.Unlock()
	})
}

func (p *PreparedScan) Execute(sink Sink) (*domain.ScanResult, *Failure) {
	defer p.Release()
	ctx, stop := context.WithTimeoutCause(p.ctx, p.timeout, errRunTimeout)
	defer stop()
	em := newEmitter(sink, func() { p.cancel(errCallerGone) })
	result, err := p.svc.deps.Scanner.Scan(ctx, p.dir, func(ev domain.ScanEvent) {
		em.emit(EventScanStage, &ScanStageEvent{Stage: ev.Stage, Done: ev.Done, Summary: ev.Summary})
	})
	if ctx.Err() != nil {
		cause := context.Cause(ctx)
		if errors.Is(cause, errRunTimeout) {
			return nil, &Failure{Code: CodeTimeout, Message: fmt.Sprintf("the scan reached its timeout of %s", p.timeout)}
		}
		return nil, &Failure{Code: CodeCancelled, Message: "the scan was cancelled: " + cause.Error()}
	}
	if err != nil {
		return nil, &Failure{Code: CodeInternal, Message: "the scan failed: " + err.Error()}
	}
	return &result, nil
}

// CancelScan reports whether the scan streaming under id was running.
func (s *Service) CancelScan(id string) bool {
	s.mu.Lock()
	prepared, ok := s.scans[strings.TrimSpace(id)]
	s.mu.Unlock()
	if ok {
		prepared.cancel(errCancelRequested)
	}
	return ok
}
