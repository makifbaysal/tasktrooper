package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repodependency"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeRepoDependencyStore struct {
	rows  map[uuid.UUID]domain.RepoDependency
	repos map[uuid.UUID]domain.Repository
}

func newFakeRepoDependencyStore(repos map[uuid.UUID]domain.Repository) *fakeRepoDependencyStore {
	return &fakeRepoDependencyStore{rows: map[uuid.UUID]domain.RepoDependency{}, repos: repos}
}

func (f *fakeRepoDependencyStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepoDependency, error) {
	var out []domain.RepoDependency
	for _, d := range f.rows {
		if d.RepositoryID == repositoryID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeRepoDependencyStore) inProject(repoID, projectID uuid.UUID) bool {
	for _, pid := range f.repos[repoID].ProjectIDs {
		if pid == projectID {
			return true
		}
	}
	return false
}

func (f *fakeRepoDependencyStore) ListByProject(ctx context.Context, projectID uuid.UUID) ([]domain.RepoDependency, []domain.RepoDependency, error) {
	var outgoing, incoming []domain.RepoDependency
	for _, d := range f.rows {
		sourceIn := f.inProject(d.RepositoryID, projectID)
		if sourceIn {
			outgoing = append(outgoing, d)
			continue
		}
		if d.TargetRepositoryID != nil && f.inProject(*d.TargetRepositoryID, projectID) {
			incoming = append(incoming, d)
		}
	}
	return outgoing, incoming, nil
}

func (f *fakeRepoDependencyStore) Get(ctx context.Context, id uuid.UUID) (domain.RepoDependency, error) {
	d, ok := f.rows[id]
	if !ok {
		return domain.RepoDependency{}, port.ErrNotFound
	}
	return d, nil
}

func (f *fakeRepoDependencyStore) Create(ctx context.Context, repositoryID uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error) {
	d := domain.RepoDependency{
		ID:                   uuid.New(),
		RepositoryID:         repositoryID,
		TargetKind:           req.TargetKind,
		TargetRepositoryID:   req.TargetRepositoryID,
		TargetSubProjectPath: req.TargetSubProjectPath,
		DatabaseLabel:        req.DatabaseLabel,
		DatabaseEnv:          req.DatabaseEnv,
		Note:                 req.Note,
	}
	if req.DatabaseSecret != "" {
		d.DatabaseSecret = secrets.MaskedValue()
	}
	f.rows[d.ID] = d
	return d, nil
}

func (f *fakeRepoDependencyStore) Update(ctx context.Context, id uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error) {
	d, ok := f.rows[id]
	if !ok {
		return domain.RepoDependency{}, port.ErrNotFound
	}
	d.TargetKind = req.TargetKind
	d.TargetRepositoryID = req.TargetRepositoryID
	d.TargetSubProjectPath = req.TargetSubProjectPath
	d.DatabaseLabel = req.DatabaseLabel
	d.DatabaseEnv = req.DatabaseEnv
	d.Note = req.Note
	if req.DatabaseSecret != "" && !secrets.IsMaskedValue(req.DatabaseSecret) {
		d.DatabaseSecret = secrets.MaskedValue()
	}
	f.rows[id] = d
	return d, nil
}

func (f *fakeRepoDependencyStore) Delete(ctx context.Context, id uuid.UUID) error {
	if _, ok := f.rows[id]; !ok {
		return port.ErrNotFound
	}
	delete(f.rows, id)
	return nil
}

func (f *fakeRepoDependencyStore) SetCipher(c *secrets.Cipher, err error) {}

type fakeRepoDependencyRepos struct {
	byID map[uuid.UUID]domain.Repository
}

func (f *fakeRepoDependencyRepos) Get(ctx context.Context, id uuid.UUID) (domain.Repository, error) {
	r, ok := f.byID[id]
	if !ok {
		return domain.Repository{}, port.ErrNotFound
	}
	return r, nil
}

func newRepoDependencyTestApp(repos ...domain.Repository) (*fiber.App, map[uuid.UUID]domain.Repository) {
	byID := make(map[uuid.UUID]domain.Repository, len(repos))
	for _, r := range repos {
		byID[r.ID] = r
	}
	store := newFakeRepoDependencyStore(byID)
	svc := repodependency.NewService(store, &fakeRepoDependencyRepos{byID: byID})
	h := &Handler{repoDependencySvc: svc}
	app := fiber.New()
	h.registerRepoDependencyRoutes(app)
	return app, byID
}

func requestJSON(t *testing.T, app *fiber.App, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	_, err = rec.Body.ReadFrom(resp.Body)
	require.NoError(t, err)
	return rec
}

func TestCreateRepoDependencyRepoTargetAppearsInTheList(t *testing.T) {
	source := domain.Repository{ID: uuid.New()}
	target := domain.Repository{ID: uuid.New()}
	app, _ := newRepoDependencyTestApp(source, target)

	rec := requestJSON(t, app, "POST", "/v1/repositories/"+source.ID.String()+"/dependencies",
		`{"target_kind":"repo","target_repository_id":"`+target.ID.String()+`"}`)
	require.Equal(t, fiber.StatusCreated, rec.Code)

	var created domain.RepoDependency
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Equal(t, domain.DependencyTargetRepo, created.TargetKind)

	rec = requestJSON(t, app, "GET", "/v1/repositories/"+source.ID.String()+"/dependencies", "")
	require.Equal(t, fiber.StatusOK, rec.Code)
	var listBody struct {
		Dependencies []domain.RepoDependency `json:"dependencies"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listBody))
	require.Len(t, listBody.Dependencies, 1)
	require.Equal(t, created.ID, listBody.Dependencies[0].ID)
}

func TestCreateRepoDependencyRejectsASubRepoInADifferentProject(t *testing.T) {
	projA, projB := uuid.New(), uuid.New()
	source := domain.Repository{ID: uuid.New(), ProjectIDs: []uuid.UUID{projA}}
	target := domain.Repository{
		ID:          uuid.New(),
		ProjectIDs:  []uuid.UUID{projB},
		SubProjects: []domain.RepoSubProject{{Path: "apps/api", Kind: domain.RepoKindBackend}},
	}
	app, _ := newRepoDependencyTestApp(source, target)

	rec := requestJSON(t, app, "POST", "/v1/repositories/"+source.ID.String()+"/dependencies",
		`{"target_kind":"sub_repo","target_repository_id":"`+target.ID.String()+`","target_sub_project_path":"apps/api"}`)
	require.True(t, rec.Code >= 400 && rec.Code < 500, "expected a 4xx, got %d: %s", rec.Code, rec.Body.String())
}

func TestCreateRepoDependencyDatabaseTargetMasksTheSecret(t *testing.T) {
	source := domain.Repository{ID: uuid.New()}
	app, _ := newRepoDependencyTestApp(source)

	rec := requestJSON(t, app, "POST", "/v1/repositories/"+source.ID.String()+"/dependencies",
		`{"target_kind":"database","database_label":"Prod Postgres","database_env":"prod","database_secret":"hunter2"}`)
	require.Equal(t, fiber.StatusCreated, rec.Code)

	var created domain.RepoDependency
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Equal(t, secrets.MaskedValue(), created.DatabaseSecret)
	require.NotContains(t, rec.Body.String(), "hunter2")
}

func TestUpdateRepoDependencyWithAnEmptySecretKeepsItMasked(t *testing.T) {
	source := domain.Repository{ID: uuid.New()}
	app, _ := newRepoDependencyTestApp(source)

	rec := requestJSON(t, app, "POST", "/v1/repositories/"+source.ID.String()+"/dependencies",
		`{"target_kind":"database","database_label":"Cache","database_secret":"hunter2"}`)
	require.Equal(t, fiber.StatusCreated, rec.Code)
	var created domain.RepoDependency
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	rec = requestJSON(t, app, "PATCH", "/v1/repositories/"+source.ID.String()+"/dependencies/"+created.ID.String(),
		`{"target_kind":"database","database_label":"Cache (renamed)"}`)
	require.Equal(t, fiber.StatusOK, rec.Code)

	rec = requestJSON(t, app, "GET", "/v1/repositories/"+source.ID.String()+"/dependencies", "")
	var listBody struct {
		Dependencies []domain.RepoDependency `json:"dependencies"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listBody))
	require.Len(t, listBody.Dependencies, 1)
	require.Equal(t, "Cache (renamed)", listBody.Dependencies[0].DatabaseLabel)
	require.Equal(t, secrets.MaskedValue(), listBody.Dependencies[0].DatabaseSecret)
}

func TestCreateRepoDependencyRejectsASelfReferencingRepoTarget(t *testing.T) {
	source := domain.Repository{ID: uuid.New()}
	app, _ := newRepoDependencyTestApp(source)

	rec := requestJSON(t, app, "POST", "/v1/repositories/"+source.ID.String()+"/dependencies",
		`{"target_kind":"repo","target_repository_id":"`+source.ID.String()+`"}`)
	require.True(t, rec.Code >= 400 && rec.Code < 500, "expected a 4xx, got %d: %s", rec.Code, rec.Body.String())
}

func TestDeleteRepoDependencyRemovesItFromTheList(t *testing.T) {
	source := domain.Repository{ID: uuid.New()}
	target := domain.Repository{ID: uuid.New()}
	app, _ := newRepoDependencyTestApp(source, target)

	rec := requestJSON(t, app, "POST", "/v1/repositories/"+source.ID.String()+"/dependencies",
		`{"target_kind":"repo","target_repository_id":"`+target.ID.String()+`"}`)
	require.Equal(t, fiber.StatusCreated, rec.Code)
	var created domain.RepoDependency
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	rec = requestJSON(t, app, "DELETE", "/v1/repositories/"+source.ID.String()+"/dependencies/"+created.ID.String(), "")
	require.Equal(t, fiber.StatusNoContent, rec.Code)

	rec = requestJSON(t, app, "GET", "/v1/repositories/"+source.ID.String()+"/dependencies", "")
	var listBody struct {
		Dependencies []domain.RepoDependency `json:"dependencies"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listBody))
	require.Empty(t, listBody.Dependencies)
}

func TestListProjectDependenciesSplitsOutgoingAndIncoming(t *testing.T) {
	projA, projB := uuid.New(), uuid.New()
	repoX := domain.Repository{ID: uuid.New(), ProjectIDs: []uuid.UUID{projA}}
	repoY := domain.Repository{ID: uuid.New(), ProjectIDs: []uuid.UUID{projB}}
	app, _ := newRepoDependencyTestApp(repoX, repoY)

	rec := requestJSON(t, app, "POST", "/v1/repositories/"+repoX.ID.String()+"/dependencies",
		`{"target_kind":"repo","target_repository_id":"`+repoY.ID.String()+`"}`)
	require.Equal(t, fiber.StatusCreated, rec.Code)

	rec = requestJSON(t, app, "GET", "/v1/projects/"+projA.String()+"/dependencies", "")
	require.Equal(t, fiber.StatusOK, rec.Code)
	var viewA struct {
		Outgoing []domain.RepoDependency `json:"outgoing"`
		Incoming []domain.RepoDependency `json:"incoming"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &viewA))
	require.Len(t, viewA.Outgoing, 1)
	require.Empty(t, viewA.Incoming)
	require.Equal(t, repoY.ID, *viewA.Outgoing[0].TargetRepositoryID)

	rec = requestJSON(t, app, "GET", "/v1/projects/"+projB.String()+"/dependencies", "")
	require.Equal(t, fiber.StatusOK, rec.Code)
	var viewB struct {
		Outgoing []domain.RepoDependency `json:"outgoing"`
		Incoming []domain.RepoDependency `json:"incoming"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &viewB))
	require.Empty(t, viewB.Outgoing)
	require.Len(t, viewB.Incoming, 1)
	require.Equal(t, repoX.ID, viewB.Incoming[0].RepositoryID)
}
