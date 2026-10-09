package board

import (
	"bytes"
	"context"
	"io"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	VerifyStagePassed     = "passed"
	VerifyStageFailed     = "failed"
	VerifyStageUnverified = "unverified"
)

type VerifyStageInfo struct {
	Name string
	// Dir is relative to the checkout; empty is its root.
	Dir     string
	Command []string
	// Setup is an install step: when it fails, the checks after it are
	// skipped and the pass reports nothing rather than a failure.
	Setup bool
}

type VerifyStageResult struct {
	VerifyStageInfo
	// Outcome is VerifyStagePassed, VerifyStageFailed or VerifyStageUnverified
	// (the tool is not installed here).
	Outcome    string
	ExitCode   int
	DurationMS int64
}

// VerifyObserver watches a pass as it runs. Output arrives in the chunks
// the stage's processes wrote, stdout and stderr interleaved as they were.
type VerifyObserver struct {
	StageStarted  func(VerifyStageInfo)
	StageOutput   func(VerifyStageInfo, []byte)
	StageFinished func(VerifyStageResult)
}

func (o *VerifyObserver) stageStarted(info VerifyStageInfo) {
	if o != nil && o.StageStarted != nil {
		o.StageStarted(info)
	}
}

func (o *VerifyObserver) stageFinished(result VerifyStageResult) {
	if o != nil && o.StageFinished != nil {
		o.StageFinished(result)
	}
}

// stageWriter is shared by the stage's stdout and stderr: os/exec then
// writes to it from one goroutine at a time.
func (o *VerifyObserver) stageWriter(info VerifyStageInfo, buf *bytes.Buffer) io.Writer {
	if o == nil || o.StageOutput == nil {
		return buf
	}
	return &observedOutput{buf: buf, info: info, emit: o.StageOutput}
}

type observedOutput struct {
	buf  *bytes.Buffer
	info VerifyStageInfo
	emit func(VerifyStageInfo, []byte)
}

func (w *observedOutput) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	if n > 0 {
		w.emit(w.info, append([]byte(nil), p[:n]...))
	}
	return n, err
}

// WorkspaceVerification is one verification pass over a checkout on this
// machine, run exactly as a board run's verify step runs it: the project
// model's required commands, else the repository's VerifyCommand, else what
// the checkout's own build files declare.
type WorkspaceVerification struct {
	Dir string
	// Repo carries VerifyCommand and the coverage and mutation gates.
	Repo     domain.Repository
	Required []domain.LocalCommand
	// Env is NAME=value, added after the toolchain resolved from the
	// checkout's own version files.
	Env []string
	// Quality runs the enforced coverage and mutation gates once the build
	// passes, as a board run does.
	Quality  bool
	Observer *VerifyObserver
}

type WorkspaceVerifyResult struct {
	Passed bool
	// Report is the failure report a fix round is handed, or on a pass the
	// notes it carries (unverified stages, coverage figures).
	Report string
	Stages []VerifyStageResult
	// AdvisoryCoverage and AdvisoryMutation are the checks this checkout has
	// but the repository does not enforce; a board run runs them after the
	// hand-off.
	AdvisoryCoverage bool
	AdvisoryMutation bool
}

func VerifyWorkspace(ctx context.Context, v WorkspaceVerification) WorkspaceVerifyResult {
	ok, report, stages := runVerificationObserved(ctx, v.Dir, v.Repo, v.Required, v.Env, v.Observer)
	result := WorkspaceVerifyResult{Passed: ok, Report: report, Stages: stages}
	if ok && v.Quality {
		notes, deferred := enforcedQualityChecks(ctx, v.Dir, v.Repo)
		result.Report = appendVerifyNotes(report, notes)
		result.AdvisoryCoverage, result.AdvisoryMutation = deferred.coverage, deferred.mutation
	}
	return result
}
