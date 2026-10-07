package board

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// advisoryChecks are the coverage and mutation runs a passing verification
// still owes but this repository does not enforce. They can take the better
// part of an hour, so they run after the hand-off and report as a comment.
// Everything they need is copied in: the job and the run are gone by then.
type advisoryChecks struct {
	workspace string
	repo      domain.Repository
	coverage  bool
	mutation  bool
	// commit is what was handed off; the checks measure it and nothing else.
	commit string
}

func (c advisoryChecks) pending() bool {
	return c.workspace != "" && (c.coverage || c.mutation)
}

// advisoryChecksMarker opens every advisory-checks comment (the catalog
// template renders it first), so a revision run can leave the note out of the
// feedback that sent the task back.
const advisoryChecksMarker = "[advisory checks]"

func isAdvisoryChecksComment(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), advisoryChecksMarker)
}

// handoffCheckout is the optional half of port.GitClient the advisory checks
// run on. They measure the handed-off commit in a checkout of their own: the
// task workspace belongs to whichever run comes next, which may reset it
// under them or commit what they write (coverage.out, lcov, .stryker-tmp).
type handoffCheckout interface {
	HeadSHA(ctx context.Context, workspacePath string) (string, error)
	AddDetachedWorktree(ctx context.Context, repoPath, rev string) (dir string, remove func(), err error)
}

// advisorySlot lets one advisory job run at a time across every task and
// every Runner: a mutation run can occupy the machine for most of an hour.
var advisorySlot = make(chan struct{}, 1)

// enforcedQualityChecks runs, inline, the checks the repository enforces and
// returns their notes; the ones it does not enforce come back for later.
func enforcedQualityChecks(ctx context.Context, workspace string, repo domain.Repository) ([]string, advisoryChecks) {
	deferred := advisoryChecks{workspace: workspace, repo: repo}
	var notes []string
	if repo.EffectiveCoverageGate("").Enabled {
		notes = append(notes, coverageReport(ctx, workspace, repo, ""))
	} else {
		deferred.coverage = detectCoverage(workspace) != nil
	}
	if repo.EffectiveMutationGate("").Enabled {
		notes = append(notes, runMutation(ctx, workspace, repo, ""))
	} else {
		deferred.mutation = detectMutation(workspace) != nil
	}
	return notes, deferred
}

// beginBackground admits one piece of post-run work, or refuses it once the
// runner is shutting down; the WaitGroup is only ever added to under the same
// lock that stops it, so a run finishing during shutdown cannot race Wait.
func (r *Runner) beginBackground() (context.Context, bool) {
	r.bgMu.Lock()
	defer r.bgMu.Unlock()
	if r.bgStopped {
		return nil, false
	}
	if r.bgCtx == nil {
		r.bgCtx, r.bgCancel = context.WithCancel(context.Background())
	}
	if r.bgCtx.Err() != nil {
		return nil, false
	}
	r.bgWG.Add(1)
	return r.bgCtx, true
}

func (r *Runner) startBackground(parent context.Context) {
	r.bgMu.Lock()
	defer r.bgMu.Unlock()
	r.bgCtx, r.bgCancel = context.WithCancel(parent)
}

// stopBackground cancels post-run work and waits for it: it is advisory, so a
// shutdown never waits for it to finish, only for it to notice.
func (r *Runner) stopBackground() {
	r.bgMu.Lock()
	r.bgStopped = true
	if r.bgCancel != nil {
		r.bgCancel()
	}
	r.bgMu.Unlock()
	r.bgWG.Wait()
}

// handoffCommit is the commit the advisory checks will measure, read right
// after the hand-off commit was pushed; empty when nothing is pending or the
// commit cannot be read, which skips them.
func (r *Runner) handoffCommit(ctx context.Context, checks advisoryChecks) string {
	checkout, ok := r.git.(handoffCheckout)
	if !ok || !checks.pending() {
		return ""
	}
	sha, err := checkout.HeadSHA(ctx, checks.workspace)
	if err != nil {
		log.Warn().Err(err).Str("workspace", checks.workspace).Msg("advisory checks skipped: the hand-off commit is unreadable")
		return ""
	}
	return sha
}

// startAdvisoryChecks is called only for a hand-off that happened: a commit
// pushed and the task moved on. Anything else has no commit to measure.
func (r *Runner) startAdvisoryChecks(job RunJob, checks advisoryChecks) {
	if !checks.pending() || checks.commit == "" {
		return
	}
	checkout, ok := r.git.(handoffCheckout)
	if !ok {
		return
	}
	ctx, ok := r.beginBackground()
	if !ok {
		return
	}
	go func() {
		defer r.bgWG.Done()
		r.runAdvisoryChecks(ctx, job, checks, checkout)
	}()
}

// runAdvisoryChecks posts nothing it cannot stand behind: a workspace reaped,
// or one that moved past the handed-off commit (a revision pushed while this
// waited or ran), is no longer what the numbers describe.
func (r *Runner) runAdvisoryChecks(ctx context.Context, job RunJob, checks advisoryChecks, checkout handoffCheckout) {
	logger := log.With().Str("task_id", job.Task.ID.String()).Str("commit", checks.commit).Logger()
	select {
	case advisorySlot <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-advisorySlot }()

	if !stillAtHandoff(ctx, checks, checkout) {
		logger.Info().Msg("advisory checks skipped: the task workspace is gone or has moved past the hand-off")
		return
	}
	dir, remove, err := checkout.AddDetachedWorktree(ctx, checks.workspace, checks.commit)
	if err != nil {
		logger.Warn().Err(err).Msg("advisory checks skipped: the hand-off commit could not be checked out")
		return
	}
	defer remove()
	linkInstalledDependencies(checks.workspace, dir)

	var notes []string
	if checks.coverage {
		notes = append(notes, coverageReport(ctx, dir, checks.repo, ""))
	}
	if checks.mutation {
		notes = append(notes, runMutation(ctx, dir, checks.repo, ""))
	}
	if ctx.Err() != nil {
		logger.Info().Msg("advisory checks dropped: the server is shutting down")
		return
	}
	if !stillAtHandoff(ctx, checks, checkout) {
		logger.Info().Msg("advisory checks dropped: the task workspace was removed or moved on while they ran")
		return
	}
	report := joinNotes(notes)
	if report == "" || r.taskUpdater == nil {
		return
	}
	if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		AuthorType:    "system",
		Content:       advisoryChecksCommentKey.Render(advisoryChecksCommentInput{Marker: advisoryChecksMarker, Report: report}),
		Informational: true,
	}); err != nil {
		logger.Warn().Err(err).Msg("advisory checks: posting the comment failed")
	}
}

func stillAtHandoff(ctx context.Context, checks advisoryChecks, checkout handoffCheckout) bool {
	if !dirExists(checks.workspace) {
		return false
	}
	head, err := checkout.HeadSHA(ctx, checks.workspace)
	return err == nil && head == checks.commit
}

// linkInstalledDependencies lends the checkout the task workspace's installed
// node_modules: a fresh worktree has none, and installing them for an
// advisory note would cost more than the note. A link that cannot be made
// leaves the check to report itself unverified.
func linkInstalledDependencies(workspace, checkout string) {
	src := filepath.Join(workspace, "node_modules")
	if !dirExists(src) {
		return
	}
	dst := filepath.Join(checkout, "node_modules")
	if _, err := os.Lstat(dst); err == nil {
		return
	}
	if err := os.Symlink(src, dst); err != nil {
		log.Debug().Err(err).Str("checkout", checkout).Msg("advisory checks: linking node_modules into the checkout failed")
	}
}

func joinNotes(notes []string) string {
	kept := make([]string, 0, len(notes))
	for _, n := range notes {
		if n = strings.TrimSpace(n); n != "" {
			kept = append(kept, n)
		}
	}
	return strings.Join(kept, "\n")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
