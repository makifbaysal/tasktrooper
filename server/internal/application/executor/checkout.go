package executor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// The post-run half of a board run, for a caller whose checkout lives on
// this computer: the verification pass, git's view of the checkout, and the
// commit and push.

const (
	EventVerifyStage  = "verify_stage"
	EventVerifyOutput = "verify_output"

	VerifyPhaseStarted  = "started"
	VerifyPhaseFinished = "finished"

	// ReasonWorkflowScope is a push GitHub refused because the token may not
	// change .github/workflows: no retry helps until a person saves a token
	// that may.
	ReasonWorkflowScope = "workflow_scope"
)

const (
	maxVerifyCommands      = 32
	maxVerifyArgs          = 256
	maxVerifyArgBytes      = 4096
	verifyOutputChunk      = 16 << 10
	verifyOutputStageLimit = 256 << 10

	defaultGitTimeout        = time.Minute
	defaultCommitPushTimeout = 10 * time.Minute
)

var errGitMissing = errors.New("this executor has no git for its checkouts")

type VerifyCommand struct {
	Dir  string   `json:"dir,omitempty"`
	Argv []string `json:"argv"`
}

type QualityGate struct {
	Enabled bool `json:"enabled"`
	// Threshold is a percentage; 0 is the board's default bar.
	Threshold float64 `json:"threshold,omitempty"`
}

type VerifyQuality struct {
	Coverage QualityGate `json:"coverage"`
	Mutation QualityGate `json:"mutation"`
}

// VerifyRequest picks the checks the way a board run does: Commands (the
// project model's required checks) when there are any, else VerifyCommand
// (the repository's own), else what the checkout's build files declare.
type VerifyRequest struct {
	Workspace     string            `json:"workspace"`
	Commands      []VerifyCommand   `json:"commands,omitempty"`
	VerifyCommand string            `json:"verify_command,omitempty"`
	Quality       *VerifyQuality    `json:"quality,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	TimeoutMS     int64             `json:"timeout_ms,omitempty"`
}

type VerifyStage struct {
	Name       string   `json:"name"`
	Dir        string   `json:"dir,omitempty"`
	Command    []string `json:"command"`
	Setup      bool     `json:"setup,omitempty"`
	Outcome    string   `json:"outcome,omitempty"`
	ExitCode   int      `json:"exit_code,omitempty"`
	DurationMS int64    `json:"duration_ms,omitempty"`
}

type VerifyAdvisory struct {
	Coverage bool `json:"coverage"`
	Mutation bool `json:"mutation"`
}

type VerifyResult struct {
	Passed bool `json:"passed"`
	// Report is what a fix round is handed on a failure, and on a pass the
	// notes it carries (unverified stages, coverage figures).
	Report string        `json:"report"`
	Stages []VerifyStage `json:"stages"`
	// Advisory is the checks this checkout has that the repository does not
	// enforce: a board run measures them after the hand-off.
	Advisory   VerifyAdvisory `json:"advisory"`
	DurationMS int64          `json:"duration_ms"`
}

type VerifyStageEvent struct {
	EventHeader
	Phase string `json:"phase"`
	VerifyStage
}

type VerifyOutputEvent struct {
	EventHeader
	Stage     string `json:"stage"`
	Data      string `json:"data,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// PreparedVerify is a validated verification pass holding its stream id and
// its checkout: one pass per checkout at a time, since two would build and
// test over each other's output.
type PreparedVerify struct {
	svc     *Service
	id      string
	dir     string
	spec    board.WorkspaceVerification
	timeout time.Duration
	ctx     context.Context
	cancel  context.CancelCauseFunc
	once    sync.Once
}

func (s *Service) PrepareVerify(ctx context.Context, id string, req VerifyRequest) (*PreparedVerify, *Failure) {
	if strings.TrimSpace(req.Workspace) == "" {
		return nil, badRequest("workspace is required: verify checks a checkout already on this computer")
	}
	dir, failure := s.resolveWorkspace(req.Workspace)
	if failure != nil {
		return nil, failure
	}
	required, failure := verifyCommands(req.Commands)
	if failure != nil {
		return nil, failure
	}
	if strings.ContainsRune(req.VerifyCommand, 0) || len(req.VerifyCommand) > maxVerifyArgBytes {
		return nil, badRequest("verify_command is longer than %d bytes or holds a NUL", maxVerifyArgBytes)
	}
	env, failure := runEnv(req.Env)
	if failure != nil {
		return nil, failure
	}
	if req.TimeoutMS < 0 {
		return nil, badRequest("timeout_ms cannot be negative")
	}
	repo := domain.Repository{VerifyCommand: strings.TrimSpace(req.VerifyCommand)}
	if q := req.Quality; q != nil {
		repo.RequireOverallCoverage, repo.CoverageThreshold = q.Coverage.Enabled, q.Coverage.Threshold
		repo.MutationEnabled, repo.MutationThreshold = q.Mutation.Enabled, q.Mutation.Threshold
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = "verify:" + strings.TrimSpace(req.Workspace)
	}
	verifyCtx, cancel := context.WithCancelCause(ctx)
	prepared := &PreparedVerify{
		svc: s, id: id, dir: dir, ctx: verifyCtx, cancel: cancel,
		timeout: time.Duration(req.TimeoutMS) * time.Millisecond,
		spec: board.WorkspaceVerification{
			Dir: dir, Repo: repo, Required: required, Env: env, Quality: req.Quality != nil,
		},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		cancel(nil)
		return nil, &Failure{Code: CodeCancelled, Message: errShuttingDown.Error()}
	}
	if _, busy := s.verifies[id]; busy {
		cancel(nil)
		return nil, &Failure{Code: CodeConflict, Message: fmt.Sprintf("verification %q is already running", id)}
	}
	for _, other := range s.verifies {
		if other.dir == dir {
			cancel(nil)
			return nil, &Failure{Code: CodeConflict, Message: fmt.Sprintf("workspace %q is already being verified (%s)", req.Workspace, other.id)}
		}
	}
	s.verifies[id] = prepared
	return prepared, nil
}

func verifyCommands(commands []VerifyCommand) ([]domain.LocalCommand, *Failure) {
	if len(commands) > maxVerifyCommands {
		return nil, badRequest("commands has %d entries; at most %d", len(commands), maxVerifyCommands)
	}
	out := make([]domain.LocalCommand, 0, len(commands))
	for i, cmd := range commands {
		if len(cmd.Argv) == 0 || strings.TrimSpace(cmd.Argv[0]) == "" {
			return nil, badRequest("commands[%d] has no program", i)
		}
		if len(cmd.Argv) > maxVerifyArgs {
			return nil, badRequest("commands[%d] has %d arguments; at most %d", i, len(cmd.Argv), maxVerifyArgs)
		}
		for _, arg := range cmd.Argv {
			if len(arg) > maxVerifyArgBytes || strings.ContainsRune(arg, 0) {
				return nil, badRequest("commands[%d] has an argument longer than %d bytes or holding a NUL", i, maxVerifyArgBytes)
			}
		}
		dir := strings.TrimSpace(cmd.Dir)
		if dir != "" && dir != "." {
			clean := filepath.Clean(filepath.FromSlash(dir))
			if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return nil, badRequest("commands[%d].dir %q leaves the workspace", i, cmd.Dir)
			}
		}
		out = append(out, domain.LocalCommand{Dir: dir, Argv: cmd.Argv})
	}
	return out, nil
}

func (p *PreparedVerify) ID() string { return p.id }

func (p *PreparedVerify) Release() {
	p.once.Do(func() {
		p.cancel(nil)
		p.svc.mu.Lock()
		if p.svc.verifies[p.id] == p {
			delete(p.svc.verifies, p.id)
		}
		p.svc.mu.Unlock()
	})
}

func (p *PreparedVerify) Execute(sink Sink) (*VerifyResult, *Failure) {
	defer p.Release()
	started := time.Now()
	ctx := p.ctx
	if p.timeout > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeoutCause(ctx, p.timeout, errRunTimeout)
		defer stop()
	}
	em := newEmitter(sink, func() { p.cancel(errCallerGone) })
	spec := p.spec
	spec.Observer = verifyObserver(em)
	outcome := board.VerifyWorkspace(ctx, spec)
	if ctx.Err() != nil {
		cause := context.Cause(ctx)
		if errors.Is(cause, errRunTimeout) {
			return nil, &Failure{Code: CodeTimeout, Message: fmt.Sprintf("the verification reached its timeout of %s", p.timeout)}
		}
		return nil, &Failure{Code: CodeCancelled, Message: "the verification was cancelled: " + cause.Error()}
	}
	stages := make([]VerifyStage, 0, len(outcome.Stages))
	for _, stage := range outcome.Stages {
		stages = append(stages, verifyStage(stage))
	}
	return &VerifyResult{
		Passed:     outcome.Passed,
		Report:     outcome.Report,
		Stages:     stages,
		Advisory:   VerifyAdvisory{Coverage: outcome.AdvisoryCoverage, Mutation: outcome.AdvisoryMutation},
		DurationMS: time.Since(started).Milliseconds(),
	}, nil
}

func verifyStage(r board.VerifyStageResult) VerifyStage {
	return VerifyStage{
		Name: r.Name, Dir: r.Dir, Command: r.Command, Setup: r.Setup,
		Outcome: r.Outcome, ExitCode: r.ExitCode, DurationMS: r.DurationMS,
	}
}

// verifyObserver streams a pass: each stage's start and end, and its output
// in chunks of at most verifyOutputChunk, up to verifyOutputStageLimit per
// stage. The failure report keeps the tail of a longer output either way.
func verifyObserver(em *emitter) *board.VerifyObserver {
	var mu sync.Mutex
	sent := map[string]int{}
	return &board.VerifyObserver{
		StageStarted: func(info board.VerifyStageInfo) {
			em.emit(EventVerifyStage, &VerifyStageEvent{Phase: VerifyPhaseStarted, VerifyStage: verifyStage(board.VerifyStageResult{VerifyStageInfo: info})})
		},
		StageOutput: func(info board.VerifyStageInfo, chunk []byte) {
			mu.Lock()
			already := sent[info.Name]
			if already >= verifyOutputStageLimit {
				mu.Unlock()
				return
			}
			if room := verifyOutputStageLimit - already; len(chunk) > room {
				chunk = chunk[:room]
			}
			sent[info.Name] = already + len(chunk)
			full := sent[info.Name] >= verifyOutputStageLimit
			mu.Unlock()
			for len(chunk) > 0 {
				n := min(len(chunk), verifyOutputChunk)
				em.emit(EventVerifyOutput, &VerifyOutputEvent{Stage: info.Name, Data: string(chunk[:n])})
				chunk = chunk[n:]
			}
			if full {
				em.emit(EventVerifyOutput, &VerifyOutputEvent{Stage: info.Name, Truncated: true})
			}
		},
		StageFinished: func(result board.VerifyStageResult) {
			em.emit(EventVerifyStage, &VerifyStageEvent{Phase: VerifyPhaseFinished, VerifyStage: verifyStage(result)})
		},
	}
}

// CancelVerify reports whether the pass streaming under id was running.
func (s *Service) CancelVerify(id string) bool {
	s.mu.Lock()
	prepared, ok := s.verifies[strings.TrimSpace(id)]
	s.mu.Unlock()
	if ok {
		prepared.cancel(errCancelRequested)
	}
	return ok
}

type GitStatusRequest struct {
	Workspace string `json:"workspace"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

// GitDiffRequest's empty Base is the task's whole change: the working tree
// against its merge-base with the remote's default branch.
type GitDiffRequest struct {
	Workspace string `json:"workspace"`
	Base      string `json:"base,omitempty"`
	NameOnly  bool   `json:"name_only,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

type GitLogRequest struct {
	Workspace string `json:"workspace"`
	Base      string `json:"base,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

type GitLogResult struct {
	Commits []port.CheckoutCommit `json:"commits"`
}

type CommitPushRequest struct {
	Workspace string `json:"workspace"`
	Message   string `json:"message"`
	Branch    string `json:"branch"`
	// GitHubToken is optional: git gets it through its environment for this
	// one push, never argv. Without it the push uses this computer's own
	// git credentials.
	GitHubToken string `json:"github_token,omitempty"`
	TimeoutMS   int64  `json:"timeout_ms,omitempty"`
}

func (s *Service) checkout(ctx context.Context, workspace string, timeoutMS int64, fallback time.Duration) (string, context.Context, context.CancelFunc, *Failure) {
	if s.deps.Git == nil {
		return "", nil, nil, &Failure{Code: CodeNotReady, Message: errGitMissing.Error()}
	}
	if strings.TrimSpace(workspace) == "" {
		return "", nil, nil, badRequest("workspace is required")
	}
	if timeoutMS < 0 {
		return "", nil, nil, badRequest("timeout_ms cannot be negative")
	}
	dir, failure := s.resolveWorkspace(workspace)
	if failure != nil {
		return "", nil, nil, failure
	}
	timeout := fallback
	if timeoutMS > 0 {
		timeout = time.Duration(timeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeoutCause(ctx, timeout, errRunTimeout)
	return dir, ctx, cancel, nil
}

func (s *Service) GitStatus(ctx context.Context, req GitStatusRequest) (*port.CheckoutStatus, *Failure) {
	dir, ctx, cancel, failure := s.checkout(ctx, req.Workspace, req.TimeoutMS, defaultGitTimeout)
	if failure != nil {
		return nil, failure
	}
	defer cancel()
	status, err := s.deps.Git.Status(ctx, dir)
	if err != nil {
		return nil, gitFailure(ctx, err, CodeInternal)
	}
	return &status, nil
}

func (s *Service) GitDiff(ctx context.Context, req GitDiffRequest) (*port.CheckoutDiff, *Failure) {
	if req.MaxBytes < 0 {
		return nil, badRequest("max_bytes cannot be negative")
	}
	dir, ctx, cancel, failure := s.checkout(ctx, req.Workspace, req.TimeoutMS, defaultGitTimeout)
	if failure != nil {
		return nil, failure
	}
	defer cancel()
	diff, err := s.deps.Git.Diff(ctx, dir, port.CheckoutDiffQuery{Base: req.Base, NameOnly: req.NameOnly, MaxBytes: req.MaxBytes})
	if err != nil {
		return nil, gitFailure(ctx, err, CodeInternal)
	}
	return &diff, nil
}

func (s *Service) GitLog(ctx context.Context, req GitLogRequest) (*GitLogResult, *Failure) {
	if req.Limit < 0 {
		return nil, badRequest("limit cannot be negative")
	}
	dir, ctx, cancel, failure := s.checkout(ctx, req.Workspace, req.TimeoutMS, defaultGitTimeout)
	if failure != nil {
		return nil, failure
	}
	defer cancel()
	commits, err := s.deps.Git.Log(ctx, dir, port.CheckoutLogQuery{Base: req.Base, Limit: req.Limit})
	if err != nil {
		return nil, gitFailure(ctx, err, CodeInternal)
	}
	return &GitLogResult{Commits: commits}, nil
}

func (s *Service) CommitPush(ctx context.Context, req CommitPushRequest) (*port.CheckoutPushResult, *Failure) {
	dir, ctx, cancel, failure := s.checkout(ctx, req.Workspace, req.TimeoutMS, defaultCommitPushTimeout)
	if failure != nil {
		return nil, failure
	}
	defer cancel()
	result, err := s.deps.Git.CommitPush(ctx, dir, port.CheckoutCommitPush{
		Message: req.Message, Branch: req.Branch, Token: req.GitHubToken,
	})
	if err != nil {
		failure := gitFailure(ctx, err, CodeUpstream)
		if failure.Code == CodeUpstream {
			log.Warn().Err(err).Str("workspace", req.Workspace).Str("branch", req.Branch).Msg("executor commit_push failed")
		}
		return nil, failure
	}
	return &result, nil
}

// gitFailure names a failed git call; fallback is the code for a git command
// that simply failed, which for a push is the remote refusing or unreachable.
func gitFailure(ctx context.Context, err error, fallback string) *Failure {
	switch {
	case ctx.Err() != nil && errors.Is(context.Cause(ctx), errRunTimeout):
		return &Failure{Code: CodeTimeout, Message: "the git call reached its timeout: " + err.Error()}
	case ctx.Err() != nil:
		return &Failure{Code: CodeCancelled, Message: "the git call was cancelled: " + err.Error()}
	case errors.Is(err, port.ErrCheckoutInvalid), errors.Is(err, port.ErrCheckoutNotRepository):
		return &Failure{Code: CodeBadRequest, Message: err.Error()}
	case errors.Is(err, port.ErrCheckoutConflict):
		return &Failure{Code: CodeConflict, Message: err.Error()}
	case errors.Is(err, domain.ErrGitHubWorkflowScope):
		return &Failure{Code: CodeUpstream, Reason: ReasonWorkflowScope, Message: err.Error()}
	default:
		return &Failure{Code: fallback, Message: err.Error()}
	}
}
