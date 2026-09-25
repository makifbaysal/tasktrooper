package projectmodel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	scanTimeout    = 10 * time.Minute
	scanStaleAfter = 7 * 24 * time.Hour
)

// StartScan is single-flight per repository: a repository already being
// scanned returns that scan instead of starting a second one. The placeholder
// uuid.Nil claims the repository's slot before any I/O so two concurrent
// callers can never both pass the "not running" check.
func (s *Service) StartScan(ctx context.Context, repositoryID uuid.UUID, trigger domain.ScanTrigger) (domain.ProjectScan, bool, error) {
	s.mu.Lock()
	if scanID, running := s.inflight[repositoryID]; running {
		s.mu.Unlock()
		if scanID == uuid.Nil {
			return domain.ProjectScan{RepositoryID: repositoryID, Trigger: trigger, Status: domain.ScanQueued}, false, nil
		}
		scan, err := s.store.GetScan(ctx, scanID)
		return scan, false, err
	}
	s.inflight[repositoryID] = uuid.Nil
	s.mu.Unlock()

	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		s.clearInflight(repositoryID, uuid.Nil)
		return domain.ProjectScan{}, false, err
	}

	scan, err := s.store.CreateScan(ctx, domain.ProjectScan{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		Trigger:      trigger,
		Status:       domain.ScanQueued,
		Events:       []domain.ScanEvent{},
		StartedAt:    s.now(),
	})
	if err != nil {
		s.clearInflight(repositoryID, uuid.Nil)
		return domain.ProjectScan{}, false, err
	}

	s.mu.Lock()
	s.inflight[repositoryID] = scan.ID
	s.mu.Unlock()

	go s.runScanGuarded(repo, scan)

	return scan, true, nil
}

func (s *Service) clearInflight(repositoryID, scanID uuid.UUID) {
	s.mu.Lock()
	if s.inflight[repositoryID] == scanID {
		delete(s.inflight, repositoryID)
	}
	s.mu.Unlock()
}

// runScanGuarded is the goroutine StartScan launches: it bounds the scan to
// scanTimeout under the process-lifetime background context (so a request's
// cancellation never kills a scan it started) and turns a panic into a
// failed scan instead of a crashed process.
func (s *Service) runScanGuarded(repo domain.Repository, scan domain.ProjectScan) {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(s.bgCtx), scanTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("repository_id", repo.ID.String()).Str("scan_id", scan.ID.String()).Msg("scan panicked")
			s.clearInflight(repo.ID, scan.ID)
			s.failScan(context.WithoutCancel(runCtx), scan, fmt.Sprintf("panic: %v", r))
		}
	}()
	s.runScan(runCtx, repo, scan)
}

func (s *Service) runScan(ctx context.Context, repo domain.Repository, scan domain.ProjectScan) {
	scan.Status = domain.ScanRunning
	if err := s.store.UpdateScan(ctx, scan); err != nil {
		log.Warn().Err(err).Str("scan_id", scan.ID.String()).Msg("scan: persisting running status failed")
	}

	if strings.TrimSpace(repo.RootPath) == "" {
		s.failScan(ctx, scan, "repository has no root path on this machine")
		s.clearInflight(repo.ID, scan.ID)
		return
	}

	emit := func(ev domain.ScanEvent) {
		scan.Events = append(scan.Events, ev)
		scan.Stage = ev.Stage
		if err := s.store.UpdateScan(ctx, scan); err != nil {
			log.Warn().Err(err).Str("scan_id", scan.ID.String()).Msg("scan: persisting progress event failed")
		}
	}

	result, err := s.scanner.Scan(ctx, repo.RootPath, emit)
	if err != nil {
		s.failScan(ctx, scan, err.Error())
		s.clearInflight(repo.ID, scan.ID)
		return
	}

	emit(domain.ScanEvent{Stage: domain.ScanStageMatch, Done: false, At: s.now()})

	summary, err := s.reconcileScan(ctx, repo, scan.ID, scan.Trigger, result)
	if err != nil {
		s.failScan(ctx, scan, err.Error())
		s.clearInflight(repo.ID, scan.ID)
		return
	}

	if err := s.project(ctx, repo.ID); err != nil {
		log.Warn().Err(err).Str("repository_id", repo.ID.String()).Msg("scan: legacy projection failed")
	}

	if s.deployMatcher != nil {
		if err := s.deployMatcher.MatchScan(ctx, repo.ID, result); err != nil {
			log.Warn().Err(err).Str("repository_id", repo.ID.String()).Msg("scan: deploy match failed")
		}
	}

	if err := s.Relink(ctx); err != nil {
		log.Warn().Err(err).Str("repository_id", repo.ID.String()).Msg("scan: relink failed")
	}

	scan.Events = append(scan.Events, domain.ScanEvent{
		Stage:   domain.ScanStageMatch,
		Done:    true,
		Summary: summarizeMatch(summary),
		At:      s.now(),
	})
	scan.Stage = domain.ScanStageMatch
	scan.Status = domain.ScanSucceeded
	scan.CommitSHA = result.Git.HeadSHA
	scan.Result = &result
	scan.ReviewCount = summary.ReviewCount
	finishedAt := s.now()
	scan.FinishedAt = &finishedAt
	if err := s.store.UpdateScan(ctx, scan); err != nil {
		log.Warn().Err(err).Str("scan_id", scan.ID.String()).Msg("scan: persisting finished scan failed")
	}

	s.clearInflight(repo.ID, scan.ID)
}

func (s *Service) failScan(ctx context.Context, scan domain.ProjectScan, message string) {
	scan.Status = domain.ScanFailed
	scan.Error = message
	finishedAt := s.now()
	scan.FinishedAt = &finishedAt
	if err := s.store.UpdateScan(ctx, scan); err != nil {
		log.Warn().Err(err).Str("scan_id", scan.ID.String()).Msg("scan: persisting failed status failed")
	}
}

func summarizeMatch(summary reconcileSummary) string {
	text := fmt.Sprintf("%s, %s, %s", pluralize(summary.Components, "component"), pluralize(summary.Checks, "check"), pluralize(summary.Links, "link"))
	if summary.ReviewCount > 0 {
		text += fmt.Sprintf("; %d to review", summary.ReviewCount)
	}
	return text
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func scanTriggerForReason(reason string) domain.ScanTrigger {
	switch reason {
	case "import":
		return domain.ScanTriggerImport
	case "manual":
		return domain.ScanTriggerManual
	case "push", "poll":
		return domain.ScanTriggerPush
	default:
		return domain.ScanTriggerStale
	}
}

// RefreshAsync is the repository service's compat seam for what used to
// trigger a profile refresh; it now starts a scan.
func (s *Service) RefreshAsync(ctx context.Context, repositoryID uuid.UUID, reason string) bool {
	_, started, err := s.StartScan(ctx, repositoryID, scanTriggerForReason(reason))
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("reason", reason).Msg("scan: RefreshAsync failed to start")
		return false
	}
	return started
}

func (s *Service) RefreshIfStale(ctx context.Context, repositoryID uuid.UUID, reason string) {
	latest, err := s.store.LatestScan(ctx, repositoryID)
	if err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("scan: staleness check failed")
			return
		}
		s.RefreshAsync(ctx, repositoryID, reason)
		return
	}
	if s.now().Sub(latest.StartedAt) > scanStaleAfter {
		s.RefreshAsync(ctx, repositoryID, reason)
	}
}

// RefreshAfterPush always starts a scan: scans are cheap, unlike the agent
// profile refresh they replaced.
func (s *Service) RefreshAfterPush(ctx context.Context, repositoryID uuid.UUID, reason string) {
	if _, _, err := s.StartScan(ctx, repositoryID, domain.ScanTriggerPush); err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("reason", reason).Msg("scan: RefreshAfterPush failed to start")
	}
}

func (s *Service) LatestScan(ctx context.Context, repositoryID uuid.UUID) (*domain.ProjectScan, error) {
	scan, err := s.store.LatestScan(ctx, repositoryID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &scan, nil
}

func (s *Service) GetScan(ctx context.Context, scanID uuid.UUID) (domain.ProjectScan, error) {
	scan, err := s.store.GetScan(ctx, scanID)
	if err != nil {
		return domain.ProjectScan{}, err
	}
	scan.Result = nil
	return scan, nil
}
