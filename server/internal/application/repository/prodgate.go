package repository

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ErrMigrationNotStaged blocks the release of a task that changes the database
// schema before a stage deploy has actually applied that change. A migration
// that has never run anywhere is the one failure the QA gate cannot catch:
// build and test both stay green while production breaks on rollout.
var ErrMigrationNotStaged = errors.New("this task changes the database schema and has not been verified on stage yet — run the stage deploy first")

// DetectTaskMigration inspects the task branch's changed files and records
// whether the task carries a schema change. It runs off the request path
// (git is slow) and comments on the task the first time it finds one, so the
// developer and the QA agent both learn that the stage gate is now armed.
// ctx for its identity only (context.WithoutCancel): the stamp and the comment it
// writes are both policy-protected rows.
func (s *Service) DetectTaskMigration(ctx context.Context, task domain.BoardTask) {
	if s.git == nil || s.workspaceRoot == "" || s.tasks == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()

		workspacePath := s.taskWorkspacePath(task.ID)
		if workspacePath == "" {
			return
		}
		files, err := s.git.TaskChangedFiles(ctx, workspacePath)
		if err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("changed files for migration detection failed")
			return
		}
		hits := domain.DetectMigrationChange(files)
		hasMigration := len(hits) > 0
		// A stamp earned by an earlier stage deploy says nothing about the
		// commits that brought the task back through review: re-arm the gate
		// and let the stage deploy triggered by this same transition re-earn
		// it. Detection runs on every code_review/ready_for_qa entry, so the
		// stamp is only ever as old as the last review cycle.
		if hasMigration && task.StageVerifiedAt != nil {
			if err := s.tasks.ClearStageVerification(ctx, task.ID); err != nil {
				log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("clear stage verification failed")
			}
		}
		if hasMigration == task.HasMigration {
			return
		}
		if err := s.tasks.SetMigrationFlag(ctx, task.ID, hasMigration); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("persist migration flag failed")
			return
		}
		if !hasMigration || s.comments == nil {
			return
		}
		if len(hits) > 10 {
			hits = hits[:10]
		}
		_, _ = s.comments.Create(ctx, domain.TaskComment{
			TaskID:     task.ID,
			AuthorType: "system",
			Content: "Schema change detected — this task must pass a stage deploy before it can be released to production:\n- " +
				strings.Join(hits, "\n- ") +
				"\n\nQA: verify the migration applied on stage (and that a rollback path exists) before approving.",
		})
	}()
}

// MarkTaskStageVerified stamps the successful stage deploy of a task. It is the
// only thing that opens the production gate for a schema change.
func (s *Service) MarkTaskStageVerified(ctx context.Context, taskID uuid.UUID) error {
	if s.tasks == nil {
		return errors.New("board tasks unavailable")
	}
	return s.tasks.MarkStageVerified(ctx, taskID, time.Now())
}

// migrationGate refuses a production release for a schema-changing task that
// stage never ran, and says so on the task so the block is visible on the board
// instead of only in a failed API call.
func (s *Service) migrationGate(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) error {
	if !task.HasMigration || task.StageVerifiedAt != nil {
		return nil
	}
	if s.comments != nil {
		_, _ = s.comments.Create(ctx, domain.TaskComment{
			TaskID:     task.ID,
			AuthorType: "system",
			Content: "Release blocked: this task changes the database schema and no stage deploy has succeeded for it. " +
				"Move it back to ready_for_qa (or dispatch the stage deploy manually), confirm the migration applies, then release.",
		})
	}
	log.Warn().Str("task_id", task.ID.String()).Str("repository_id", repositoryID.String()).
		Msg("release blocked: unstaged migration")
	return ErrMigrationNotStaged
}

// ErrReleaseNotDone refuses a production deploy for a task the board has not
// signed off. It is the assertion the trigger_release tool description has
// always made and nothing enforced.
var ErrReleaseNotDone = errors.New("a task can only be released from the done column — it has not been signed off")

// releaseColumnGate refuses to release a task that is not in done (or already
// released, which is a re-release).
//
// Written like migrationGate rather than like releaseTargetGate: it comments on
// the task so the block is visible on the board, not only in a failed API call,
// and it is checked FIRST among the state gates because it is the cheapest and
// the most fundamental — every gate after it is answering "is this code safe to
// ship", and this one answers "did anybody agree to ship it".
func (s *Service) releaseColumnGate(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) error {
	switch task.Column {
	case domain.TaskColumnDone, domain.TaskColumnReleased:
		return nil
	}
	err := fmt.Errorf("%w (it is in `%s`)", ErrReleaseNotDone, task.Column)
	if s.comments != nil {
		_, _ = s.comments.Create(ctx, domain.TaskComment{
			TaskID:     task.ID,
			AuthorType: "system",
			Content: err.Error() + "\n\nMove the task through its review chain to `done` first. " +
				"Reaching done is also what stamps the verified commit the release is checked against, so releasing from anywhere else could not have shipped reviewed code even if this gate allowed it.",
		})
	}
	log.Warn().Str("task_id", task.ID.String()).Str("repository_id", repositoryID.String()).
		Str("column", string(task.Column)).Msg("release blocked: task is not in done")
	return err
}

// releaseTargetResolveTimeout bounds the git calls the release-identity checks
// make. Both run on a caller's context (a column move, a release dispatch), and
// a wedged git process must not hold either open indefinitely — the failure
// direction is "no stamp"/"blocked", never "assume it matches".
const releaseTargetResolveTimeout = 15 * time.Second

// taskWorkspacePath is where a task's branch is checked out. Same layout the
// PR-open and migration detectors use.
func (s *Service) taskWorkspacePath(taskID uuid.UUID) string {
	// Through workspace.TaskDir rather than a local Join: creating a checkout
	// and deleting one have to agree on the path exactly, and they no longer
	// live in the same file. An empty path is already the "no workspace"
	// answer both callers handle.
	path, err := workspace.TaskDir(s.workspaceRoot, taskID)
	if err != nil {
		return ""
	}
	return path
}

// resolveReleaseTargetSHA reports the commit the task's branch points at right
// now — the code a deploy dispatched for this task would carry. It is
// deliberately the only source of a "current" SHA: both the stamp and the
// re-check must read the same thing, or the comparison compares nothing.
//
// Every failure is an error, never an empty SHA with a nil error: callers turn
// "cannot resolve" into a block, and a silent "" would turn it into a pass.
func (s *Service) resolveReleaseTargetSHA(ctx context.Context, taskID uuid.UUID) (string, error) {
	if s.git == nil || s.workspaceRoot == "" {
		return "", errors.New("git is not wired into the control plane")
	}
	path := s.taskWorkspacePath(taskID)
	if !s.git.HasGit(path) {
		return "", fmt.Errorf("no git working copy at %s", path)
	}
	ctx, cancel := context.WithTimeout(ctx, releaseTargetResolveTimeout)
	defer cancel()
	info, err := s.git.TaskGitInfo(ctx, path)
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(info.HeadSHA)
	if sha == "" {
		return "", errors.New("task branch HEAD resolved to an empty commit")
	}
	return sha, nil
}

// verifiedSHAForMove is what UpdateTask writes into the task's verified-commit
// stamp as part of a column move, so the stamp and the move land in one write
// and can never disagree:
//
//   - entering done stamps the commit the review/QA/UAT chain just signed off
//     on. done is the last gate anybody looks at the code in; everything after
//     it is a dispatch, not a review.
//   - leaving done for anything but released drops the stamp. The sign-off was
//     withdrawn, and a stamp left behind would still authorise a release —
//     the same re-arming ClearStageVerification does for the migration gate.
//   - every other move carries the existing stamp through untouched.
//
// A task that cannot be stamped (no workspace, git down) keeps an empty stamp
// rather than failing the move: the release gate then refuses it, which is the
// safe direction. Blocking a board move over a git hiccup is not.
func (s *Service) verifiedSHAForMove(ctx context.Context, task domain.BoardTask, prev, next domain.TaskColumn) string {
	switch {
	case next == domain.TaskColumnDone:
		sha, err := s.resolveReleaseTargetSHA(ctx, task.ID)
		if err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).
				Msg("stamping the verified commit on done failed; release will be blocked until it can be re-stamped")
			return ""
		}
		return sha
	case prev == domain.TaskColumnDone && next != domain.TaskColumnReleased:
		return ""
	default:
		return task.VerifiedSHA
	}
}

// releaseTargetGate refuses to dispatch a production deploy for code that is
// not the code the board signed off on.
//
// TriggerRelease's other gates read task state; this one re-reads the world.
// "done" says a task was verified, it does not say which commit was verified,
// and the window between the sign-off and the dispatch is real: a follow-up
// agent run on the same task commits more work, a force-push rewrites the
// branch, and the deploy ships something nobody reviewed. So: compare the
// commit stamped at done against the branch as it stands now, and block on any
// disagreement — including "there is nothing to compare", which is how an
// unstamped task, a missing workspace and a git failure all land here.
//
// Hard block, not a warning: a production deploy of unreviewed code costs more
// than a stalled release. The deliberate way through is the one the migration
// gate already established — send the task back through review and let it earn
// a fresh sign-off, which re-stamps it on the way back into done.
func (s *Service) releaseTargetGate(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) error {
	verified := strings.TrimSpace(task.VerifiedSHA)
	current, err := s.resolveReleaseTargetSHA(ctx, task.ID)
	if err != nil {
		return s.blockRelease(ctx, repositoryID, task,
			fmt.Errorf("%w: the task branch could not be resolved (%v)", domain.ErrReleaseTargetUnverified, err),
			"Restore the task workspace (or re-run the task) so the commit being released can be identified, then release again.")
	}
	// The comparison itself lives in domain.VerifiedCommitMatches, shared with
	// the pull-request merge gate — which asks the identical question of the PR
	// head. Two copies of "is this the code that was signed off" is one copy too
	// many for a rule that guards irreversible actions; only the wording and the
	// remedy are this gate's own.
	switch err := domain.VerifiedCommitMatches(verified, current); {
	case errors.Is(err, domain.ErrReleaseTargetUnverified):
		return s.blockRelease(ctx, repositoryID, task,
			fmt.Errorf("%w: no verified commit is stamped on this task, while its branch is at %s",
				domain.ErrReleaseTargetUnverified, shortSHA(current)),
			"Move the task back through review (need_revision → code_review → … → done). Reaching done stamps the commit that was signed off, which is what this gate compares against.")
	case errors.Is(err, domain.ErrReleaseTargetMoved):
		return s.blockRelease(ctx, repositoryID, task,
			fmt.Errorf("%w: verified at %s, but the branch is now at %s",
				domain.ErrReleaseTargetMoved, shortSHA(verified), shortSHA(current)),
			"Send the task back through review so the new commits are reviewed and QA'd; returning it to done re-stamps the verified commit and unblocks the release.")
	}
	return nil
}

// blockRelease records the refusal where a human will see it (the task) and
// where an operator will grep for it (the log), then returns the sentinel-wrapped
// error so callers and the agent tool can both explain the block. Same shape as
// migrationGate: a block that only exists in a failed API call is invisible.
func (s *Service) blockRelease(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, err error, remedy string) error {
	if s.comments != nil {
		_, _ = s.comments.Create(ctx, domain.TaskComment{
			TaskID:     task.ID,
			AuthorType: "system",
			Content:    err.Error() + "\n\n" + remedy,
		})
	}
	log.Warn().Err(err).Str("task_id", task.ID.String()).Str("repository_id", repositoryID.String()).
		Msg("release blocked: release target is not the verified code")
	return err
}

// shortSHA renders a commit the way a human reads one in a git log. The
// rendering moved to domain.ShortSHA when the merge gate started needing it
// too; this stays as the package's local name for it.
func shortSHA(sha string) string { return domain.ShortSHA(sha) }

// mobileStoreGate refuses a stage or prod deploy dispatch for a store-shipped
// environment (App Store / Google Play) until the mobile app has reached the
// state that environment requires: stage needs the onboarding checklist
// finished (test_ready or later), prod needs the app to already be live —
// the first store submit is a manual product decision that this gate never
// makes for anyone. Any non-store deploy path is a no-op: no configured
// target for the env, or a target whose provider isn't a store provider,
// both return nil immediately.
func (s *Service) mobileStoreGate(ctx context.Context, repositoryID uuid.UUID, env string) error {
	if s.deployTargets == nil {
		return nil
	}
	target, err := s.deployTargets.Get(ctx, repositoryID, "", env)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			// No deploy target defined for this env: nothing to gate.
			return nil
		}
		// A real lookup failure (DB down, etc.) is not "no target
		// configured" — it must propagate so a transient infra error can
		// never silently disable this gate and let an unverified app ship.
		return err
	}
	if !domain.IsStoreProvider(target.Provider) {
		return nil
	}

	sentinel := domain.ErrMobileAppNotTestReady
	if env == domain.DeployEnvProd {
		sentinel = domain.ErrMobileAppNotLive
	}
	// A store target with no wired app store (SetMobileStoreApps never
	// called) can never be verified — treat it exactly like a missing row,
	// never nil-deref.
	if s.mobileStoreApps == nil {
		return sentinel
	}

	platform := domain.StoreProviderPlatform(target.Provider)
	app, err := s.mobileStoreApps.Get(ctx, repositoryID, platform)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return sentinel
		}
		// A real lookup failure (DB down, etc.) is not "not onboarded yet" —
		// it must propagate so the caller doesn't silently release/deploy.
		return err
	}

	if env == domain.DeployEnvProd {
		if app.State != domain.MobileStoreStateLive {
			return domain.ErrMobileAppNotLive
		}
		return nil
	}
	if app.State != domain.MobileStoreStateTestReady && app.State != domain.MobileStoreStateLive {
		return domain.ErrMobileAppNotTestReady
	}
	return nil
}

// hasAnyDeployTarget reports whether the repository has any deploy_target row
// — any environment, any sub-project. It answers "is there anywhere this
// repository's code could be deployed to", which is what decides whether a
// merge is the whole release or the first half of one.
func (s *Service) hasAnyDeployTarget(ctx context.Context, repositoryID uuid.UUID) (bool, error) {
	if s.deployTargets == nil {
		return false, nil
	}
	targets, err := s.deployTargets.ListByRepository(ctx, repositoryID)
	if err != nil {
		return false, err
	}
	return len(targets) > 0, nil
}

// AutoReleaseIfUndeployable moves a task straight to `released` right after
// its pull request merges, when the repository has nowhere configured to
// deploy it: merging IS the release. It is a no-op — returns false — on any
// repository with at least one deploy_target row; scenarios where a real
// deploy pipeline succeeds or fails are unaffected and decide those.
//
// It goes through the ordinary gated UpdateTask, not a raw column write, so a
// repository with require_release_deploy=true — which demands proof of an
// actual deploy (releaseDeployGate) — is still correctly refused rather than
// silently bypassed: that repository owner opted into "nothing ships without
// a verified deploy", and zero deploy targets can never produce one. The task
// stays in `done` in that case, with a comment explaining why, rather than
// failing the merge that already succeeded.
func (s *Service) AutoReleaseIfUndeployable(ctx context.Context, repositoryID, taskID uuid.UUID) bool {
	has, err := s.hasAnyDeployTarget(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).
			Msg("deploy target lookup for auto-release failed; leaving the task in done")
		return false
	}
	if has {
		return false
	}
	col := domain.TaskColumnReleased
	if _, err := s.UpdateTask(ctx, repositoryID, taskID, domain.UpdateBoardTaskRequest{
		Column:       &col,
		SystemReason: domain.MoveReasonMergeReleasedNoDeployTarget,
	}); err != nil {
		if errors.Is(err, domain.ErrReleaseNotDeployed) {
			if s.comments != nil {
				_, _ = s.comments.Create(ctx, domain.TaskComment{
					TaskID:     taskID,
					AuthorType: "system",
					Content: "This repository has no deploy_target configured anywhere, so merging cannot auto-release it: " +
						"require_release_deploy is on and demands proof of an actual production deploy, which nothing here can produce. " +
						"Turn require_release_deploy off for this repository, or configure a deploy target.",
				})
			}
			return false
		}
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("auto-release after merge failed")
		return false
	}
	return true
}

// SetTestStrategy changes how the repository's tasks are verified: workspace
// tests only, a staging deploy before QA, or a deploy at every reviewed step.
func (s *Service) SetTestStrategy(ctx context.Context, repositoryID uuid.UUID, strategy string) (domain.Repository, error) {
	strategy = strings.TrimSpace(strategy)
	if !domain.ValidTestStrategy(strategy) {
		return domain.Repository{}, fmt.Errorf("invalid test strategy %q", strategy)
	}
	return s.repos.UpdateTestStrategy(ctx, repositoryID, strategy)
}

// testStrategy reads the repo's strategy, defaulting to stage for rows written
// before the setting existed.
func (s *Service) testStrategy(ctx context.Context, repositoryID uuid.UUID) string {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil || !domain.ValidTestStrategy(repo.TestStrategy) {
		return domain.TestStrategyStage
	}
	return repo.TestStrategy
}

// EnvFile is one environment-declaring file found in a repository, with the
// variable names it declares. Values are never read: the point is to know what
// an environment must provide, not to copy secrets around.
type EnvFile struct {
	Path string   `json:"path"`
	Keys []string `json:"keys"`
}

// EnvInventory is "what does this project need to run, and where does each
// environment answer" in one payload.
type EnvInventory struct {
	Files   []EnvFile             `json:"files"`
	Keys    []string              `json:"keys"`
	Targets []domain.DeployTarget `json:"targets,omitempty"`
}

// envFileNames are scanned at the repo root and one level down (monorepo apps).
var envFileNames = []string{
	".env.example", ".env.sample", ".env.template", ".env.defaults", ".env.dist",
}

const envScanMaxBytes = 256 * 1024

// EnvInventory scans the repository for declared environment variables. Only
// example/template files are read — never a real .env — so a scan can never
// surface a live secret.
func (s *Service) EnvInventory(ctx context.Context, repositoryID uuid.UUID) (EnvInventory, error) {
	root, err := s.ResolveRootPath(ctx, repositoryID)
	if err != nil {
		return EnvInventory{}, err
	}
	inv := EnvInventory{}
	seen := map[string]bool{}

	scan := func(path, rel string) {
		keys, err := readEnvKeys(path)
		if err != nil || len(keys) == 0 {
			return
		}
		inv.Files = append(inv.Files, EnvFile{Path: rel, Keys: keys})
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				inv.Keys = append(inv.Keys, k)
			}
		}
	}

	for _, name := range envFileNames {
		scan(filepath.Join(root, name), name)
	}
	// One level down covers monorepos (apps/backend/.env.example) without
	// walking the whole tree.
	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" {
				continue
			}
			for _, name := range envFileNames {
				scan(filepath.Join(root, entry.Name(), name), filepath.Join(entry.Name(), name))
			}
			sub, subErr := os.ReadDir(filepath.Join(root, entry.Name()))
			if subErr != nil {
				continue
			}
			for _, child := range sub {
				if !child.IsDir() || strings.HasPrefix(child.Name(), ".") {
					continue
				}
				for _, name := range envFileNames {
					rel := filepath.Join(entry.Name(), child.Name(), name)
					scan(filepath.Join(root, rel), rel)
				}
			}
		}
	}
	sort.Strings(inv.Keys)

	if s.deployTargets != nil {
		if targets, terr := s.deployTargets.ListByRepository(ctx, repositoryID); terr == nil {
			inv.Targets = targets
		}
	}
	return inv, nil
}

// readEnvKeys returns the variable names declared by a dotenv-style file.
func readEnvKeys(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > envScanMaxBytes {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var keys []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, _, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys, scanner.Err()
}
