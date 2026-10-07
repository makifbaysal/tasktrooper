package storeops_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeTestBuildStore struct {
	mu     sync.Mutex
	rows   map[uuid.UUID]domain.StoreTestBuild
	auto   map[string][]string
	nextAt time.Time
}

var _ port.StoreTestBuildStore = (*fakeTestBuildStore)(nil)

func newFakeTestBuildStore() *fakeTestBuildStore {
	return &fakeTestBuildStore{rows: map[uuid.UUID]domain.StoreTestBuild{}, auto: map[string][]string{}, nextAt: time.Now()}
}

func (f *fakeTestBuildStore) Create(_ context.Context, b domain.StoreTestBuild) (domain.StoreTestBuild, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.RepositoryID == b.RepositoryID && r.Platform == b.Platform && r.BuildNumber == b.BuildNumber {
			return domain.StoreTestBuild{}, errors.New("duplicate build number")
		}
	}
	b.ID = uuid.New()
	f.nextAt = f.nextAt.Add(time.Second)
	b.CreatedAt, b.UpdatedAt = f.nextAt, f.nextAt
	if b.Groups == nil {
		b.Groups = []string{}
	}
	f.rows[b.ID] = b
	return b, nil
}

func (f *fakeTestBuildStore) Update(_ context.Context, b domain.StoreTestBuild) (domain.StoreTestBuild, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.rows[b.ID]
	if !ok {
		return domain.StoreTestBuild{}, domain.ErrTestBuildNotFound
	}
	b.CreatedAt = cur.CreatedAt
	b.HasArtifact = b.ArtifactPath != ""
	f.rows[b.ID] = b
	return b, nil
}

func (f *fakeTestBuildStore) Get(_ context.Context, id uuid.UUID) (domain.StoreTestBuild, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok {
		return domain.StoreTestBuild{}, domain.ErrTestBuildNotFound
	}
	return b, nil
}

func (f *fakeTestBuildStore) List(_ context.Context, repositoryID uuid.UUID, platform string, taskID *uuid.UUID, limit int) ([]domain.StoreTestBuild, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.StoreTestBuild
	for _, b := range f.rows {
		if b.RepositoryID != repositoryID || (platform != "" && b.Platform != platform) {
			continue
		}
		if taskID != nil && (b.TaskID == nil || *b.TaskID != *taskID) {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeTestBuildStore) MaxSequence(_ context.Context, repositoryID uuid.UUID, platform string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var m int64
	for _, b := range f.rows {
		if b.RepositoryID == repositoryID && b.Platform == platform {
			m = max(m, b.Sequence)
		}
	}
	return m, nil
}

func (f *fakeTestBuildStore) MaxAttempt(_ context.Context, taskID uuid.UUID, platform string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := 0
	for _, b := range f.rows {
		if b.TaskID != nil && *b.TaskID == taskID && b.Platform == platform {
			m = max(m, b.Attempt)
		}
	}
	return m, nil
}

func (f *fakeTestBuildStore) ListUnfinished(context.Context) ([]domain.StoreTestBuild, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.StoreTestBuild
	for _, b := range f.rows {
		if !domain.TestBuildTerminal(b.Status) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeTestBuildStore) AutoGroups(_ context.Context, repositoryID uuid.UUID, platform string) ([]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.auto[repositoryID.String()+platform]
	return g, ok, nil
}

func (f *fakeTestBuildStore) SetAutoGroups(_ context.Context, repositoryID uuid.UUID, platform string, groups []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auto[repositoryID.String()+platform] = groups
	return nil
}

type fakeTestFlight struct {
	mu sync.Mutex

	Sequence   int64
	Groups     []port.BetaGroup
	Build      port.TestFlightBuild
	BuildFound bool

	Added      map[string][]string
	Removed    map[string][]string
	Submitted  []string
	WhatToTest map[string]string
	Declared   map[string]bool
}

var _ port.TestFlightClient = (*fakeTestFlight)(nil)

func newFakeTestFlight() *fakeTestFlight {
	return &fakeTestFlight{Added: map[string][]string{}, Removed: map[string][]string{}, WhatToTest: map[string]string{}, Declared: map[string]bool{}}
}

func (f *fakeTestFlight) LatestBuildSequence(context.Context, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Sequence, nil
}

func (f *fakeTestFlight) FindBuild(_ context.Context, _, buildNumber string) (port.TestFlightBuild, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.Build
	b.BuildNumber = buildNumber
	if b.ID == "" {
		b.ID = "asc-" + buildNumber
	}
	return b, f.BuildFound, nil
}

func (f *fakeTestFlight) SetWhatToTest(_ context.Context, buildID, _, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.WhatToTest[buildID] = text
	return nil
}

func (f *fakeTestFlight) DeclareEncryption(_ context.Context, buildID string, v bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Declared[buildID] = v
	return nil
}

func (f *fakeTestFlight) BetaGroups(context.Context, string) ([]port.BetaGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]port.BetaGroup(nil), f.Groups...), nil
}

func (f *fakeTestFlight) CreateBetaGroup(_ context.Context, _, name string, internal bool) (port.BetaGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g := port.BetaGroup{ID: "g-" + name, Name: name, Internal: internal}
	f.Groups = append(f.Groups, g)
	return g, nil
}

func (f *fakeTestFlight) BuildGroupIDs(_ context.Context, buildID string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Added[buildID], nil
}

func (f *fakeTestFlight) AddBuildToGroups(_ context.Context, buildID string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Added[buildID] = append(f.Added[buildID], ids...)
	return nil
}

func (f *fakeTestFlight) RemoveBuildFromGroups(_ context.Context, buildID string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Removed[buildID] = append(f.Removed[buildID], ids...)
	return nil
}

func (f *fakeTestFlight) SubmitForBetaReview(_ context.Context, buildID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Submitted = append(f.Submitted, buildID)
	return nil
}

func (f *fakeTestFlight) BetaReviewState(context.Context, string) (string, error) { return "", nil }

func (f *fakeTestFlight) GroupTesters(context.Context, string) ([]port.BetaTester, error) {
	return []port.BetaTester{{ID: "t1", Email: "a@example.com"}}, nil
}

func (f *fakeTestFlight) AddTester(_ context.Context, _, email, first, last string) (port.BetaTester, error) {
	return port.BetaTester{ID: "t-new", Email: email, FirstName: first, LastName: last}, nil
}

func (f *fakeTestFlight) RemoveTester(context.Context, string, string) error { return nil }

type releaseToTrackCall struct {
	Track       string
	AAB         string
	VersionCode int64
	Name        string
}

type fakePlayTesting struct {
	mu          sync.Mutex
	VersionCode int64
	Tracks      []port.PlayTrack
	Uploads     []string
	Releases    []releaseToTrackCall
	UploadErr   error
}

var _ port.PlayTestingClient = (*fakePlayTesting)(nil)

func (f *fakePlayTesting) LatestVersionCode(context.Context, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.VersionCode, nil
}

func (f *fakePlayTesting) UploadInternalSharing(_ context.Context, _, aab string) (port.InternalShareLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UploadErr != nil {
		return port.InternalShareLink{}, f.UploadErr
	}
	f.Uploads = append(f.Uploads, aab)
	return port.InternalShareLink{DownloadURL: "https://play.google.com/apps/test/share/" + filepath.Base(aab)}, nil
}

func (f *fakePlayTesting) ListTracks(context.Context, string) ([]port.PlayTrack, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Tracks, nil
}

func (f *fakePlayTesting) ReleaseToTrack(_ context.Context, _, track, aab string, vc int64, name, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Releases = append(f.Releases, releaseToTrackCall{Track: track, AAB: aab, VersionCode: vc, Name: name})
	return nil
}

type fakeMobileBuilder struct {
	mu        sync.Mutex
	OK        bool
	Reason    string
	Err       error
	Artifact  string
	Requests  []port.MobileBuildRequest
	writeAAB  bool
	outputDir string
}

func (f *fakeMobileBuilder) Available(context.Context, string) (bool, string) { return f.OK, f.Reason }

func (f *fakeMobileBuilder) Run(_ context.Context, req port.MobileBuildRequest) (port.MobileBuildResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, req)
	if req.Log != nil {
		req.Log("==> build " + req.Env["BUILD_NUMBER"])
	}
	if f.Err != nil {
		return port.MobileBuildResult{}, f.Err
	}
	artifact := f.Artifact
	if f.writeAAB {
		artifact = filepath.Join(f.outputDir, req.BuildID.String()+"-built.aab")
		if err := os.WriteFile(artifact, []byte("aab"), 0o600); err != nil {
			return port.MobileBuildResult{}, err
		}
	}
	return port.MobileBuildResult{Artifact: artifact, RunURL: "https://github.com/o/r/actions/runs/1"}, nil
}

func (f *fakeMobileBuilder) requests() []port.MobileBuildRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Requests)
}

type fakeTestBuildSource struct {
	mu        sync.Mutex
	SHA       string
	Published int
}

var _ storeops.TestBuildSource = (*fakeTestBuildSource)(nil)

func (f *fakeTestBuildSource) Head(context.Context, domain.Repository, *domain.BoardTask) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.SHA, "feature/t-54", nil
}

func (f *fakeTestBuildSource) Checkout(ctx context.Context, repo domain.Repository, task *domain.BoardTask) (storeops.TestBuildCheckout, error) {
	sha, branch, _ := f.Head(ctx, repo, task)
	return storeops.TestBuildCheckout{Dir: os.TempDir(), SHA: sha, Branch: branch, Cleanup: func() {}}, nil
}

func (f *fakeTestBuildSource) Publish(ctx context.Context, repo domain.Repository, task *domain.BoardTask) (string, string, error) {
	f.mu.Lock()
	f.Published++
	f.mu.Unlock()
	return f.Head(ctx, repo, task)
}

type fakeTaskReader struct {
	tasks map[uuid.UUID]domain.BoardTask
}

func (f *fakeTaskReader) GetTask(_ context.Context, _, taskID uuid.UUID) (domain.BoardTask, error) {
	t, ok := f.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, port.ErrNotFound
	}
	return t, nil
}
