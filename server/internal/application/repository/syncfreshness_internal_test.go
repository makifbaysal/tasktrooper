package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeSyncGit struct {
	fakeReleaseGit
	syncErr error
}

func (f *fakeSyncGit) SyncDefaultBranch(context.Context, string) error { return f.syncErr }

func newSyncService(git *fakeSyncGit) *Service {
	return &Service{
		git:           git,
		syncWarnings:  make(map[uuid.UUID]string),
		syncCheckedAt: make(map[uuid.UUID]time.Time),
	}
}

func TestPullFailureIsRecordedAndCleared(t *testing.T) {
	git := &fakeSyncGit{syncErr: errors.New("git fetch: authentication failed")}
	git.hasGit = true
	svc := newSyncService(git)
	id := uuid.New()

	svc.pullProjectRoot(context.Background(), id, "/data/workspaces/repos/app")
	if got := svc.syncWarning(id); got == "" {
		t.Fatal("failed pull left no warning")
	}

	git.syncErr = nil
	svc.pullProjectRoot(context.Background(), id, "/data/workspaces/repos/app")
	if got := svc.syncWarning(id); got != "" {
		t.Fatalf("successful pull did not clear the warning: %q", got)
	}
}

func TestFreshnessCheckIsThrottled(t *testing.T) {
	svc := newSyncService(&fakeSyncGit{})
	id := uuid.New()

	if !svc.claimFreshnessCheck(id) {
		t.Fatal("first check was not allowed")
	}
	if svc.claimFreshnessCheck(id) {
		t.Fatal("second check ran inside the throttle window")
	}

	svc.syncMu.Lock()
	svc.syncCheckedAt[id] = time.Now().Add(-freshnessCheckInterval - time.Second)
	svc.syncMu.Unlock()

	if !svc.claimFreshnessCheck(id) {
		t.Fatal("check did not resume after the interval elapsed")
	}
}

func TestPullSkippedWithoutWorkingCopy(t *testing.T) {
	git := &fakeSyncGit{syncErr: errors.New("should not be called")}
	git.hasGit = false
	svc := newSyncService(git)
	id := uuid.New()

	svc.pullProjectRoot(context.Background(), id, "/data/workspaces/repos/app")
	if got := svc.syncWarning(id); got != "" {
		t.Fatalf("non-git root produced a warning: %q", got)
	}
}

func TestFreshnessWaitDoublesPerFailedPullUpToAnHour(t *testing.T) {
	tests := []struct {
		name     string
		failures int
		want     time.Duration
	}{
		{"pulling fine", 0, 10 * time.Minute},
		{"one failed pull", 1, 20 * time.Minute},
		{"two failed pulls", 2, 40 * time.Minute},
		{"three failed pulls", 3, time.Hour},
		{"offline all day", 50, time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, freshnessWait(tt.failures))
		})
	}
}

func TestFailedPullsBackOffTheFreshnessCheckAndASuccessResetsIt(t *testing.T) {
	git := &fakeSyncGit{syncErr: errors.New("git fetch: could not resolve host: github.com")}
	git.hasGit = true
	svc := newSyncService(git)
	id := uuid.New()
	checkedAgo := func(d time.Duration) {
		svc.syncMu.Lock()
		svc.syncCheckedAt[id] = time.Now().Add(-d)
		svc.syncMu.Unlock()
	}

	svc.pullProjectRoot(context.Background(), id, "/data/workspaces/repos/app")
	svc.pullProjectRoot(context.Background(), id, "/data/workspaces/repos/app")

	checkedAgo(freshnessCheckInterval + time.Second)
	assert.False(t, svc.freshnessCheckDue(id), "an offline repository is retried on the plain interval")
	assert.False(t, svc.claimFreshnessCheck(id))
	checkedAgo(40*time.Minute + time.Second)
	assert.True(t, svc.freshnessCheckDue(id))

	git.syncErr = nil
	svc.pullProjectRoot(context.Background(), id, "/data/workspaces/repos/app")
	checkedAgo(freshnessCheckInterval + time.Second)
	assert.True(t, svc.claimFreshnessCheck(id), "a successful pull goes back to the plain interval")
}
