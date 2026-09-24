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

var ErrMigrationNotStaged = errors.New("this task changes the database schema and has not been verified on stage yet — run the stage deploy first")

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

func (s *Service) MarkTaskStageVerified(ctx context.Context, taskID uuid.UUID) error {
	if s.tasks == nil {
		return errors.New("board tasks unavailable")
	}
	return s.tasks.MarkStageVerified(ctx, taskID, time.Now())
}

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

var ErrReleaseNotDone = errors.New("a task can only be released from the done column — it has not been signed off")

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

const releaseTargetResolveTimeout = 15 * time.Second

func (s *Service) taskWorkspacePath(taskID uuid.UUID) string {

	path, err := workspace.TaskDir(s.workspaceRoot, taskID)
	if err != nil {
		return ""
	}
	return path
}

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

func (s *Service) releaseTargetGate(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) error {
	verified := strings.TrimSpace(task.VerifiedSHA)
	current, err := s.resolveReleaseTargetSHA(ctx, task.ID)
	if err != nil {
		return s.blockRelease(ctx, repositoryID, task,
			fmt.Errorf("%w: the task branch could not be resolved (%v)", domain.ErrReleaseTargetUnverified, err),
			"Restore the task workspace (or re-run the task) so the commit being released can be identified, then release again.")
	}

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

func shortSHA(sha string) string { return domain.ShortSHA(sha) }

func (s *Service) mobileStoreGate(ctx context.Context, repositoryID uuid.UUID, env string) error {
	if s.deployTargets == nil {
		return nil
	}
	target, err := s.deployTargets.Get(ctx, repositoryID, "", env)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {

			return nil
		}

		return err
	}
	if !domain.IsStoreProvider(target.Provider) {
		return nil
	}

	sentinel := domain.ErrMobileAppNotTestReady
	if env == domain.DeployEnvProd {
		sentinel = domain.ErrMobileAppNotLive
	}

	if s.mobileStoreApps == nil {
		return sentinel
	}

	platform := domain.StoreProviderPlatform(target.Provider)
	app, err := s.mobileStoreApps.Get(ctx, repositoryID, platform)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return sentinel
		}

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
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("auto-release after merge failed")
		return false
	}
	return true
}

func (s *Service) SetTestStrategy(ctx context.Context, repositoryID uuid.UUID, strategy string) (domain.Repository, error) {
	strategy = strings.TrimSpace(strategy)
	if !domain.ValidTestStrategy(strategy) {
		return domain.Repository{}, fmt.Errorf("invalid test strategy %q", strategy)
	}
	return s.repos.UpdateTestStrategy(ctx, repositoryID, strategy)
}

func (s *Service) testStrategy(ctx context.Context, repositoryID uuid.UUID) string {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil || !domain.ValidTestStrategy(repo.TestStrategy) {
		return domain.TestStrategyStage
	}
	return repo.TestStrategy
}

type EnvFile struct {
	Path string   `json:"path"`
	Keys []string `json:"keys"`
}

type EnvInventory struct {
	Files   []EnvFile             `json:"files"`
	Keys    []string              `json:"keys"`
	Targets []domain.DeployTarget `json:"targets,omitempty"`
}

var envFileNames = []string{
	".env.example", ".env.sample", ".env.template", ".env.defaults", ".env.dist",
}

const envScanMaxBytes = 256 * 1024

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
