package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type chanMarker chan domain.Release

func (c chanMarker) MarkDeployed(_ context.Context, r domain.Release) { c <- r }

func TestASucceededDeployIsMarked(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		f := newSweepFixture()
		marks := make(chanMarker, 1)
		f.svc.SetDeployMarker(marks)
		f.legacy.target = domain.DeployTarget{BaseURL: server.URL}
		repositoryID := uuid.New()
		task := f.withTask(repositoryID)
		r := deployingRelease(repositoryID, f.clock.Now())
		r.Profile.Verify.Smoke = []domain.SmokeCheck{{Method: "GET", Path: "/smoke"}}
		created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
		require.NoError(t, err)
		f.ds.set(mergeSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchSuccess})

		f.svc.SweepOnce(context.Background())

		select {
		case got := <-marks:
			assert.Equal(t, created.ID, got.ID)
			assert.Equal(t, mergeSHA, got.CommitSHA)
			assert.NotNil(t, got.DeployedAt, "the marker carries when it went live")
		case <-time.After(2 * time.Second):
			t.Fatalf("a deploy that went live (smoke %d) was not marked", status)
		}
		server.Close()
	}
}

func TestAFailedDeployIsNotMarked(t *testing.T) {
	f := newSweepFixture()
	marks := make(chanMarker, 1)
	f.svc.SetDeployMarker(marks)
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	_, err := f.store.Create(context.Background(), deployingRelease(repositoryID, f.clock.Now()), []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.ds.set(mergeSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchFailure, Detail: "build failed"})

	f.svc.SweepOnce(context.Background())

	select {
	case <-marks:
		t.Fatal("a failed deploy must not be marked")
	case <-time.After(100 * time.Millisecond):
	}
}
