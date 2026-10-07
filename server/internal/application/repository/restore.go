package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const restoreCloneTimeout = 30 * time.Minute

func (s *Service) RestoreWorkingCopy(ctx context.Context, repositoryID uuid.UUID) (domain.Repository, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.Repository{}, err
	}
	if s.git == nil {
		return domain.Repository{}, fmt.Errorf("git client is not configured")
	}

	if s.IsRestoreRunning(repositoryID) {
		return s.withGitWarning(repo), nil
	}

	if ok, why := domain.CanRestoreWorkingCopy(s.git.Presence(repo.RootPath), repo.RemoteURL); !ok {
		return domain.Repository{}, fmt.Errorf("%s", why)
	}
	dest, err := s.restoreDestination(repo)
	if err != nil {
		return domain.Repository{}, err
	}

	if _, statErr := os.Stat(dest); statErr == nil {
		if !s.git.HasGit(dest) {
			return domain.Repository{}, fmt.Errorf(
				"a folder already exists at %s but is not a git repository. Nothing was changed or deleted — move or remove it yourself, then try again", dest)
		}

		if err := s.assertSameRepo(ctx, dest, repo.RemoteURL); err != nil {
			return domain.Repository{}, err
		}
		updated, err := s.repos.UpdateRootPath(ctx, repositoryID, dest)
		if err != nil {
			return domain.Repository{}, fmt.Errorf("save restored path: %w", err)
		}
		updated.ProjectIDs = repo.ProjectIDs
		s.recordRestore(repositoryID, &domain.RepositoryRestore{
			Status: domain.RepositoryRestoreCompleted, RootPath: dest,
			StartedAt: time.Now(), FinishedAt: ptrNow(),
		})
		s.startIndex(ctx, repositoryID, dest)
		return s.withGitWarning(updated), nil
	}

	s.recordRestore(repositoryID, &domain.RepositoryRestore{
		Status: domain.RepositoryRestoreRunning, RootPath: dest, StartedAt: time.Now(),
	})
	cloneURL := repo.RemoteURL

	restoreCtx := context.WithoutCancel(ctx)
	s.launchRestore(func() { s.runRestore(restoreCtx, repositoryID, cloneURL, dest) })
	return s.withGitWarning(repo), nil
}

func (s *Service) runRestore(parent context.Context, repositoryID uuid.UUID, cloneURL, dest string) {
	ctx, cancel := context.WithTimeout(parent, restoreCloneTimeout)
	defer cancel()

	if err := s.git.CloneRepo(ctx, cloneURL, dest); err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("dest", dest).
			Msg("restore: clone failed")
		s.finishRestore(repositoryID, dest, err)
		return
	}

	if _, err := s.repos.UpdateRootPath(ctx, repositoryID, dest); err != nil {
		log.Error().Err(err).Str("repository_id", repositoryID.String()).Str("dest", dest).
			Msg("restore: clone succeeded but root_path could not be updated")
		s.finishRestore(repositoryID, dest, fmt.Errorf("the code was cloned to %s but the project could not be re-pointed at it: %w", dest, err))
		return
	}
	s.finishRestore(repositoryID, dest, nil)
	log.Info().Str("repository_id", repositoryID.String()).Str("dest", dest).
		Msg("restore: working copy is back on this host")

	s.startIndex(parent, repositoryID, dest)
}

func (s *Service) restoreDestination(repo domain.Repository) (string, error) {
	if strings.TrimSpace(s.workspaceRoot) == "" {
		return "", fmt.Errorf("this runtime has no workspace root configured, so there is nowhere to put the working copy")
	}
	name := restoreDirName(repo)
	if name == "" {
		return "", fmt.Errorf("could not work out a folder name for this project")
	}
	return s.workspaceRepoPath(name)
}

func (s *Service) workspaceRepoPath(name string) (string, error) {
	return workspace.RepoDir(s.workspaceRoot, name)
}

func restoreDirName(repo domain.Repository) string {
	if name := workspace.CleanDirName(workspace.BaseName(repo.RootPath)); name != "" {
		return name
	}
	if name := workspace.CleanDirName(repoNameFromRemote(repo.RemoteURL)); name != "" {
		return name
	}
	return workspace.CleanDirName(repo.Name)
}

func repoNameFromRemote(remoteURL string) string {
	trimmed := strings.TrimSpace(remoteURL)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(strings.TrimRight(trimmed, "/"), ".git")
	if idx := strings.LastIndexAny(trimmed, "/:"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	return trimmed
}

func (s *Service) launchRestore(fn func()) {
	if s.restoreRun != nil {
		s.restoreRun(fn)
		return
	}
	go fn()
}

func (s *Service) IsRestoreRunning(repositoryID uuid.UUID) bool {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	state := s.restores[repositoryID]
	return state != nil && state.Status == domain.RepositoryRestoreRunning
}

func (s *Service) recordRestore(repositoryID uuid.UUID, state *domain.RepositoryRestore) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.restores == nil {
		s.restores = make(map[uuid.UUID]*domain.RepositoryRestore)
	}
	s.restores[repositoryID] = state
}

func (s *Service) finishRestore(repositoryID uuid.UUID, dest string, err error) {
	state := &domain.RepositoryRestore{
		Status: domain.RepositoryRestoreCompleted, RootPath: dest, FinishedAt: ptrNow(),
	}
	s.restoreMu.Lock()
	if previous := s.restores[repositoryID]; previous != nil {
		state.StartedAt = previous.StartedAt
	}
	s.restoreMu.Unlock()
	if state.StartedAt.IsZero() {
		state.StartedAt = time.Now()
	}
	if err != nil {
		state.Status = domain.RepositoryRestoreFailed
		state.Error = err.Error()
	}
	s.recordRestore(repositoryID, state)
}

func (s *Service) restoreState(repositoryID uuid.UUID) *domain.RepositoryRestore {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	state, ok := s.restores[repositoryID]
	if !ok || state == nil {
		return nil
	}
	copied := *state
	return &copied
}

func ptrNow() *time.Time {
	now := time.Now()
	return &now
}
