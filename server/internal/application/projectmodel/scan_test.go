package projectmodel

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ScanSuite struct {
	suite.Suite

	store     *fakeStore
	repos     *fakeRepos
	projector *fakeProjector
	projects  *fakeProjects
	pipelines *fakePipelines
	scanner   *fakeScanner
	svc       *Service

	repo domain.Repository
}

func TestScanSuite(t *testing.T) {
	suite.Run(t, new(ScanSuite))
}

func (s *ScanSuite) SetupTest() {
	s.store = newFakeStore()
	s.repo = domain.Repository{ID: uuid.New(), Name: "widgets", RootPath: "/repos/widgets"}
	s.repos = newFakeRepos(s.repo)
	s.projector = &fakeProjector{repos: s.repos}
	s.projects = newFakeProjects()
	s.pipelines = newFakePipelines()
	s.scanner = &fakeScanner{result: domain.ScanResult{Git: domain.ScanGit{HeadSHA: "sha1"}}}

	s.svc = NewService(Deps{
		Store:     s.store,
		Repos:     s.repos,
		Projector: s.projector,
		Projects:  s.projects,
		Pipelines: s.pipelines,
		Scanner:   s.scanner,
	})
	s.svc.SetBackgroundContext(context.Background())
}

func (s *ScanSuite) awaitScan(scanID uuid.UUID) domain.ProjectScan {
	s.T().Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		scan, err := s.store.GetScan(context.Background(), scanID)
		s.Require().NoError(err)
		if scan.Status == domain.ScanSucceeded || scan.Status == domain.ScanFailed {
			return scan
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.FailNow("scan did not finish in time")
	return domain.ProjectScan{}
}

func (s *ScanSuite) TestStartScanSingleFlightReturnsRunningScan() {
	s.scanner.block = make(chan struct{})
	s.scanner.started = make(chan struct{})

	first, started, err := s.svc.StartScan(context.Background(), s.repo.ID, domain.ScanTriggerManual)
	s.Require().NoError(err)
	s.True(started)

	select {
	case <-s.scanner.started:
	case <-time.After(2 * time.Second):
		s.FailNow("scan never started")
	}

	second, startedAgain, err := s.svc.StartScan(context.Background(), s.repo.ID, domain.ScanTriggerManual)
	s.Require().NoError(err)
	s.False(startedAgain)
	s.Equal(first.ID, second.ID)
	s.Equal(1, s.scanner.callCount())

	close(s.scanner.block)
	finished := s.awaitScan(first.ID)
	s.Equal(domain.ScanSucceeded, finished.Status)

	s.scanner.mu.Lock()
	s.scanner.block = nil
	s.scanner.started = nil
	s.scanner.mu.Unlock()

	third, startedOnceMore, err := s.svc.StartScan(context.Background(), s.repo.ID, domain.ScanTriggerManual)
	s.Require().NoError(err)
	s.True(startedOnceMore)
	s.NotEqual(first.ID, third.ID)
}

func (s *ScanSuite) TestRunScanPersistsEventsInOrder() {
	s.scanner.events = []domain.ScanEvent{
		{Stage: domain.ScanStageClone, Done: true, At: time.Now()},
		{Stage: domain.ScanStageInventory, Done: true, At: time.Now()},
	}

	scan, _, err := s.svc.StartScan(context.Background(), s.repo.ID, domain.ScanTriggerManual)
	s.Require().NoError(err)

	finished := s.awaitScan(scan.ID)
	s.Require().GreaterOrEqual(len(finished.Events), 3)
	s.Equal(domain.ScanStageClone, finished.Events[0].Stage)
	s.Equal(domain.ScanStageInventory, finished.Events[1].Stage)
	s.Equal(domain.ScanStageMatch, finished.Events[len(finished.Events)-2].Stage)
	s.False(finished.Events[len(finished.Events)-2].Done)
	last := finished.Events[len(finished.Events)-1]
	s.Equal(domain.ScanStageMatch, last.Stage)
	s.True(last.Done)
	s.Contains(last.Summary, "component")
}

func (s *ScanSuite) TestRunScanCallsDeployMatcherWithTheScanResult() {
	matcher := &fakeDeployMatcher{}
	s.svc.SetDeployMatcher(matcher)
	s.scanner.result = domain.ScanResult{
		Git:           domain.ScanGit{HeadSHA: "sha1"},
		DeploySignals: []domain.DeploySignal{{ComponentPath: ".", Provider: "vercel"}},
	}

	scan, _, err := s.svc.StartScan(context.Background(), s.repo.ID, domain.ScanTriggerManual)
	s.Require().NoError(err)
	finished := s.awaitScan(scan.ID)
	s.Require().Equal(domain.ScanSucceeded, finished.Status)

	s.Require().Equal(1, matcher.callCount())
	call := matcher.calls[0]
	s.Equal(s.repo.ID, call.repositoryID)
	s.Equal(s.scanner.result, call.result)
}

func (s *ScanSuite) TestRunScanDetectsDeliveryFromScannedChecks() {
	s.scanner.result = domain.ScanResult{
		Git:        domain.ScanGit{HeadSHA: "sha1"},
		Components: []domain.DetectedComponent{{Path: "."}},
		Checks: []domain.DetectedCheck{{
			ComponentPath: ".",
			Workflow:      ".github/workflows/deploy.yml",
			JobKey:        "deploy",
			Purpose:       domain.CheckDeploy,
			Environment:   domain.EnvironmentProduction,
			Triggers:      []string{"push:main"},
			Confidence:    domain.ConfidenceHigh,
		}},
	}

	scan, _, err := s.svc.StartScan(context.Background(), s.repo.ID, domain.ScanTriggerImport)
	s.Require().NoError(err)
	finished := s.awaitScan(scan.ID)
	s.Require().Equal(domain.ScanSucceeded, finished.Status)

	components, err := s.store.ListComponents(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.Require().Len(components, 1)
	s.Require().NotNil(components[0].Delivery.Detected)
	s.Equal(domain.DeliveryOnMerge, components[0].Delivery.Detected.Mode)
	s.Equal(domain.ExecutorGitHubActions, components[0].Delivery.Detected.Executor)
	s.Equal("deploy.yml", components[0].Delivery.Detected.Workflow)
}

func (s *ScanSuite) TestRunScanFailurePath() {
	s.scanner.err = context.DeadlineExceeded

	scan, _, err := s.svc.StartScan(context.Background(), s.repo.ID, domain.ScanTriggerManual)
	s.Require().NoError(err)

	finished := s.awaitScan(scan.ID)
	s.Equal(domain.ScanFailed, finished.Status)
	s.NotEmpty(finished.Error)
	s.NotNil(finished.FinishedAt)

	s.svc.mu.Lock()
	_, running := s.svc.inflight[s.repo.ID]
	s.svc.mu.Unlock()
	s.False(running)
}

func (s *ScanSuite) TestRunScanMissingRootPathFails() {
	repo := domain.Repository{ID: uuid.New(), Name: "no-root"}
	s.repos.set(repo)

	scan, _, err := s.svc.StartScan(context.Background(), repo.ID, domain.ScanTriggerManual)
	s.Require().NoError(err)

	finished := s.awaitScan(scan.ID)
	s.Equal(domain.ScanFailed, finished.Status)
	s.Contains(finished.Error, "root path")
}

func (s *ScanSuite) TestRefreshAsyncTriggerMapping() {
	tests := []struct {
		reason  string
		trigger domain.ScanTrigger
	}{
		{"import", domain.ScanTriggerImport},
		{"manual", domain.ScanTriggerManual},
		{"push", domain.ScanTriggerPush},
		{"poll", domain.ScanTriggerPush},
		{"whatever", domain.ScanTriggerStale},
	}
	for _, tt := range tests {
		s.Run(tt.reason, func() {
			s.scanner.block = make(chan struct{})
			s.scanner.started = make(chan struct{})
			repo := domain.Repository{ID: uuid.New(), Name: "repo-" + tt.reason, RootPath: "/repos/" + tt.reason}
			s.repos.set(repo)

			started := s.svc.RefreshAsync(context.Background(), repo.ID, tt.reason)
			s.True(started)

			select {
			case <-s.scanner.started:
			case <-time.After(2 * time.Second):
				s.FailNow("scan never started")
			}

			latest, err := s.store.LatestScan(context.Background(), repo.ID)
			require.NoError(s.T(), err)
			s.Equal(tt.trigger, latest.Trigger)
			close(s.scanner.block)
			s.awaitScan(latest.ID)
		})
	}
}

func (s *ScanSuite) TestRefreshIfStaleOnlyWhenStaleOrMissing() {
	repo := domain.Repository{ID: uuid.New(), Name: "stale-check", RootPath: "/repos/stale-check"}
	s.repos.set(repo)

	s.svc.RefreshIfStale(context.Background(), repo.ID, "poll")
	s.Eventually(func() bool { return s.scanner.callCount() == 1 }, time.Second, 5*time.Millisecond)
	latest, err := s.store.LatestScan(context.Background(), repo.ID)
	s.Require().NoError(err)
	first := s.awaitScan(latest.ID)
	s.Equal(domain.ScanSucceeded, first.Status)

	s.svc.RefreshIfStale(context.Background(), repo.ID, "poll")
	time.Sleep(20 * time.Millisecond)
	s.Equal(1, s.scanner.callCount())

	s.store.mu.Lock()
	agedScan := s.store.scans[first.ID]
	agedScan.StartedAt = s.svc.now().Add(-8 * 24 * time.Hour)
	s.store.scans[first.ID] = agedScan
	s.store.mu.Unlock()

	s.svc.RefreshIfStale(context.Background(), repo.ID, "poll")
	s.Eventually(func() bool { return s.scanner.callCount() == 2 }, time.Second, 5*time.Millisecond)
}
