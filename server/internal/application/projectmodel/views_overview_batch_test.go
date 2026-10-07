package projectmodel

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *ViewsSuite) TestProjectsOverviewSummariesCarryEachRepositorysOwnChecksAndLatestScan() {
	ctx := context.Background()
	scanned := s.newRepo("scanned")
	unscanned := s.newRepo("unscanned")

	scannedComp := s.store.seedComponent(domain.Component{RepositoryID: scanned.ID, Path: ".", Status: domain.ComponentStatusActive})
	unscannedComp := s.store.seedComponent(domain.Component{RepositoryID: unscanned.ID, Path: ".", Status: domain.ComponentStatusActive})
	s.store.seedCheck(domain.ComponentCheck{
		RepositoryID: scanned.ID, ComponentID: scannedComp.ID, JobKey: "test", Status: domain.ModelStatusActive,
		Gate: domain.Detected(domain.CheckGateRequired, domain.ConfidenceHigh),
	})
	s.store.seedCheck(domain.ComponentCheck{
		RepositoryID: scanned.ID, ComponentID: scannedComp.ID, JobKey: "lint", Status: domain.ModelStatusActive,
	})
	s.store.seedCheck(domain.ComponentCheck{
		RepositoryID: unscanned.ID, ComponentID: unscannedComp.ID, JobKey: "test", Status: domain.ModelStatusActive,
	})

	started := time.Now().Add(-time.Hour)
	_, err := s.store.CreateScan(ctx, domain.ProjectScan{RepositoryID: scanned.ID, Status: domain.ScanSucceeded, StartedAt: started})
	s.Require().NoError(err)
	newest, err := s.store.CreateScan(ctx, domain.ProjectScan{RepositoryID: scanned.ID, Status: domain.ScanRunning, StartedAt: started.Add(time.Minute)})
	s.Require().NoError(err)

	overview, err := s.svc.ProjectsOverview(ctx)
	s.Require().NoError(err)
	byID := map[uuid.UUID]domain.RepositorySummary{}
	for _, r := range overview.Unassigned {
		byID[r.ID] = r
	}
	s.Require().Len(byID, 2)

	got := byID[scanned.ID]
	s.Require().Len(got.Components, 1)
	s.Equal(2, got.Components[0].Checks)
	s.Equal(1, got.Components[0].RequiredChecks)
	s.Require().NotNil(got.LastScan)
	s.Equal(newest.ID, got.LastScan.ID)
	s.Equal(domain.ScanRunning, got.LastScan.Status)

	got = byID[unscanned.ID]
	s.Require().Len(got.Components, 1)
	s.Equal(1, got.Components[0].Checks)
	s.Nil(got.LastScan)
}
