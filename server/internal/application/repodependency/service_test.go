package repodependency_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repodependency"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeStore struct {
	rows map[uuid.UUID]domain.RepoDependency
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[uuid.UUID]domain.RepoDependency{}} }

func (f *fakeStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepoDependency, error) {
	var out []domain.RepoDependency
	for _, d := range f.rows {
		if d.RepositoryID == repositoryID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeStore) ListByProject(ctx context.Context, projectID uuid.UUID) ([]domain.RepoDependency, []domain.RepoDependency, error) {
	return nil, nil, nil
}

func (f *fakeStore) Get(ctx context.Context, id uuid.UUID) (domain.RepoDependency, error) {
	d, ok := f.rows[id]
	if !ok {
		return domain.RepoDependency{}, port.ErrNotFound
	}
	return d, nil
}

func (f *fakeStore) Create(ctx context.Context, repositoryID uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error) {
	d := domain.RepoDependency{
		ID:                   uuid.New(),
		RepositoryID:         repositoryID,
		TargetKind:           req.TargetKind,
		TargetRepositoryID:   req.TargetRepositoryID,
		TargetSubProjectPath: req.TargetSubProjectPath,
		DatabaseLabel:        req.DatabaseLabel,
	}
	if req.DatabaseSecret != "" {
		d.DatabaseSecret = secrets.MaskedValue()
	}
	f.rows[d.ID] = d
	return d, nil
}

func (f *fakeStore) Update(ctx context.Context, id uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error) {
	d, ok := f.rows[id]
	if !ok {
		return domain.RepoDependency{}, port.ErrNotFound
	}
	d.TargetKind = req.TargetKind
	d.TargetRepositoryID = req.TargetRepositoryID
	d.TargetSubProjectPath = req.TargetSubProjectPath
	d.DatabaseLabel = req.DatabaseLabel
	f.rows[id] = d
	return d, nil
}

func (f *fakeStore) Delete(ctx context.Context, id uuid.UUID) error {
	if _, ok := f.rows[id]; !ok {
		return port.ErrNotFound
	}
	delete(f.rows, id)
	return nil
}

func (f *fakeStore) SetCipher(c *secrets.Cipher, err error) {}

type fakeRepos struct {
	byID map[uuid.UUID]domain.Repository
}

func newFakeRepos(repos ...domain.Repository) *fakeRepos {
	m := make(map[uuid.UUID]domain.Repository, len(repos))
	for _, r := range repos {
		m[r.ID] = r
	}
	return &fakeRepos{byID: m}
}

func (f *fakeRepos) Get(ctx context.Context, id uuid.UUID) (domain.Repository, error) {
	r, ok := f.byID[id]
	if !ok {
		return domain.Repository{}, port.ErrNotFound
	}
	return r, nil
}

func TestServiceCreateAcceptsAValidRepoDependency(t *testing.T) {
	source := domain.Repository{ID: uuid.New()}
	target := domain.Repository{ID: uuid.New()}
	svc := repodependency.NewService(newFakeStore(), newFakeRepos(source, target))

	dep, err := svc.Create(context.Background(), source.ID, domain.SaveRepoDependencyRequest{
		TargetKind:         domain.DependencyTargetRepo,
		TargetRepositoryID: &target.ID,
	})
	require.NoError(t, err)
	require.Equal(t, domain.DependencyTargetRepo, dep.TargetKind)
}

func TestServiceCreateRejectsARepoDependencyOnAMissingTarget(t *testing.T) {
	source := domain.Repository{ID: uuid.New()}
	missing := uuid.New()
	svc := repodependency.NewService(newFakeStore(), newFakeRepos(source))

	_, err := svc.Create(context.Background(), source.ID, domain.SaveRepoDependencyRequest{
		TargetKind:         domain.DependencyTargetRepo,
		TargetRepositoryID: &missing,
	})
	require.ErrorIs(t, err, port.ErrNotFound)
}

func TestServiceCreateRejectsASubRepoDependencyOnARepoInADifferentProject(t *testing.T) {
	projA := uuid.New()
	projB := uuid.New()
	source := domain.Repository{ID: uuid.New(), ProjectIDs: []uuid.UUID{projA}}
	target := domain.Repository{
		ID:          uuid.New(),
		ProjectIDs:  []uuid.UUID{projB},
		SubProjects: []domain.RepoSubProject{{Path: "apps/api", Kind: domain.RepoKindBackend}},
	}
	svc := repodependency.NewService(newFakeStore(), newFakeRepos(source, target))

	_, err := svc.Create(context.Background(), source.ID, domain.SaveRepoDependencyRequest{
		TargetKind:           domain.DependencyTargetSubRepo,
		TargetRepositoryID:   &target.ID,
		TargetSubProjectPath: "apps/api",
	})
	require.ErrorIs(t, err, repodependency.ErrNotSameProject)
}

func TestServiceCreateRejectsASubRepoDependencyOnAMissingSubProjectPath(t *testing.T) {
	proj := uuid.New()
	source := domain.Repository{ID: uuid.New(), ProjectIDs: []uuid.UUID{proj}}
	target := domain.Repository{
		ID:          uuid.New(),
		ProjectIDs:  []uuid.UUID{proj},
		SubProjects: []domain.RepoSubProject{{Path: "apps/api", Kind: domain.RepoKindBackend}},
	}
	svc := repodependency.NewService(newFakeStore(), newFakeRepos(source, target))

	_, err := svc.Create(context.Background(), source.ID, domain.SaveRepoDependencyRequest{
		TargetKind:           domain.DependencyTargetSubRepo,
		TargetRepositoryID:   &target.ID,
		TargetSubProjectPath: "apps/worker",
	})
	require.ErrorIs(t, err, repodependency.ErrSubProjectNotFound)
}

func TestServiceCreateAcceptsAllThreeValidTargetKinds(t *testing.T) {
	proj := uuid.New()
	source := domain.Repository{ID: uuid.New(), ProjectIDs: []uuid.UUID{proj}}
	repoTarget := domain.Repository{ID: uuid.New()}
	subRepoTarget := domain.Repository{
		ID:          uuid.New(),
		ProjectIDs:  []uuid.UUID{proj},
		SubProjects: []domain.RepoSubProject{{Path: "apps/api", Kind: domain.RepoKindBackend}},
	}
	svc := repodependency.NewService(newFakeStore(), newFakeRepos(source, repoTarget, subRepoTarget))
	ctx := context.Background()

	_, err := svc.Create(ctx, source.ID, domain.SaveRepoDependencyRequest{
		TargetKind:         domain.DependencyTargetRepo,
		TargetRepositoryID: &repoTarget.ID,
	})
	require.NoError(t, err)

	_, err = svc.Create(ctx, source.ID, domain.SaveRepoDependencyRequest{
		TargetKind:           domain.DependencyTargetSubRepo,
		TargetRepositoryID:   &subRepoTarget.ID,
		TargetSubProjectPath: "apps/api",
	})
	require.NoError(t, err)

	_, err = svc.Create(ctx, source.ID, domain.SaveRepoDependencyRequest{
		TargetKind:    domain.DependencyTargetDatabase,
		DatabaseLabel: "Prod Postgres",
	})
	require.NoError(t, err)
}

func TestServiceCreateRejectsInvalidFieldRulesWrappedAsInvalidInput(t *testing.T) {
	source := domain.Repository{ID: uuid.New()}
	svc := repodependency.NewService(newFakeStore(), newFakeRepos(source))

	_, err := svc.Create(context.Background(), source.ID, domain.SaveRepoDependencyRequest{TargetKind: "bogus"})
	require.True(t, errors.Is(err, repodependency.ErrInvalidInput))
}
