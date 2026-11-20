package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) EnsureIndexMirror(ctx context.Context, repositoryID uuid.UUID, rootPath string) error {
	if s == nil || s.noServerCheckout {
		return nil
	}
	if s.git == nil {

		return nil
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("the code at %s is missing and the repository record could not be read to restore it: %w", rootPath, err)
	}
	if s.git.HasGit(rootPath) {

		return s.assertSameRepo(ctx, rootPath, repo.RemoteURL)
	}
	remote := strings.TrimSpace(repo.RemoteURL)
	if remote == "" {
		return fmt.Errorf("repository %q has no clone at %s and no remote_url on record to restore it from — re-import it from GitHub "+
			"(or set its remote) before it can be indexed", repo.Name, rootPath)
	}
	if entries, readErr := os.ReadDir(rootPath); readErr == nil && len(entries) > 0 {

		return fmt.Errorf("repository %q root %s exists but is not a git repository; refusing to index it, because indexing whatever "+
			"is in that folder would produce an index that describes something other than this repository", repo.Name, rootPath)
	}

	if s.IsRestoreRunning(repositoryID) {
		return fmt.Errorf("repository %q is being restored to this server right now; indexing will pick it up on the next pass", repo.Name)
	}

	log.Info().
		Str("repository_id", repositoryID.String()).
		Str("repository", repo.Name).
		Str("root", rootPath).
		Msg("index mirror is missing on this replica; restoring it from origin before indexing")

	s.recordRestore(repositoryID, &domain.RepositoryRestore{
		Status: domain.RepositoryRestoreRunning, RootPath: rootPath, StartedAt: time.Now(),
	})
	cctx, cancel := context.WithTimeout(ctx, restoreCloneTimeout)
	defer cancel()

	if cloneErr := s.git.CloneRepo(cctx, remote, rootPath); cloneErr != nil {
		err := fmt.Errorf("restoring the clone of %q from %s failed, so there was nothing to index: %w", repo.Name, remote, cloneErr)
		s.finishRestore(repositoryID, rootPath, err)
		return err
	}
	if !s.git.HasGit(rootPath) {
		err := fmt.Errorf("restoring the clone of %q reported success but %s is still not a git repository", repo.Name, rootPath)
		s.finishRestore(repositoryID, rootPath, err)
		return err
	}
	s.finishRestore(repositoryID, rootPath, nil)
	log.Info().Str("repository_id", repositoryID.String()).Str("root", rootPath).
		Msg("index mirror restored; indexing continues")
	return nil
}
