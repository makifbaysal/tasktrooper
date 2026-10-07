package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeStoreCredentialStore is a minimal in-memory port.StoreCredentialStore
// for the handler test below. It stores whatever bytes it's handed
// untouched (no encryption) so a test can seed a raw row and prove the HTTP
// layer never echoes stored bytes back — only the safe CredentialView
// projection.
type fakeStoreCredentialStore struct {
	rows map[string]credentialRow
}

type credentialRow struct {
	data      []byte
	updatedAt time.Time
}

var _ port.StoreCredentialStore = (*fakeStoreCredentialStore)(nil)

func newFakeStoreCredentialStore() *fakeStoreCredentialStore {
	return &fakeStoreCredentialStore{rows: map[string]credentialRow{}}
}

func (f *fakeStoreCredentialStore) Set(_ context.Context, provider string, encrypted []byte) error {
	f.rows[provider] = credentialRow{data: encrypted, updatedAt: time.Now()}
	return nil
}

func (f *fakeStoreCredentialStore) Get(_ context.Context, provider string) ([]byte, time.Time, error) {
	row, ok := f.rows[provider]
	if !ok {
		return nil, time.Time{}, port.ErrNotFound
	}
	return row.data, row.updatedAt, nil
}

func (f *fakeStoreCredentialStore) Delete(_ context.Context, provider string) error {
	delete(f.rows, provider)
	return nil
}

func (f *fakeStoreCredentialStore) List(_ context.Context) (map[string]time.Time, error) {
	out := make(map[string]time.Time, len(f.rows))
	for provider, row := range f.rows {
		out[provider] = row.updatedAt
	}
	return out, nil
}

// fakeHTTPMobileStoreAppStore is a minimal in-memory port.MobileStoreAppStore
// for the ListStoreApps/VerifyStoreOnboarding handler tests below, keyed on
// (repository_id, platform) like the postgres store's unique constraint.
type fakeHTTPMobileStoreAppStore struct {
	rows map[string]domain.MobileStoreApp
	// listErr scripts an infra failure (database down) so the handler's
	// 400-vs-500 split can be exercised.
	listErr error
}

var _ port.MobileStoreAppStore = (*fakeHTTPMobileStoreAppStore)(nil)

func newFakeHTTPMobileStoreAppStore() *fakeHTTPMobileStoreAppStore {
	return &fakeHTTPMobileStoreAppStore{rows: map[string]domain.MobileStoreApp{}}
}

func mobileAppKey(repositoryID uuid.UUID, platform string) string {
	return repositoryID.String() + "/" + platform
}

func (f *fakeHTTPMobileStoreAppStore) Upsert(_ context.Context, app domain.MobileStoreApp) (domain.MobileStoreApp, error) {
	f.rows[mobileAppKey(app.RepositoryID, app.Platform)] = app
	return app, nil
}

func (f *fakeHTTPMobileStoreAppStore) Get(_ context.Context, repositoryID uuid.UUID, platform string) (domain.MobileStoreApp, error) {
	app, ok := f.rows[mobileAppKey(repositoryID, platform)]
	if !ok {
		return domain.MobileStoreApp{}, fmt.Errorf("mobile store app: %w", port.ErrNotFound)
	}
	return app, nil
}

func (f *fakeHTTPMobileStoreAppStore) SetTracks(_ context.Context, repositoryID uuid.UUID, platform string, tracks domain.StoreTracks, syncedAt time.Time) (domain.MobileStoreApp, error) {
	key := mobileAppKey(repositoryID, platform)
	app, ok := f.rows[key]
	if !ok {
		return domain.MobileStoreApp{}, fmt.Errorf("mobile store app: %w", port.ErrNotFound)
	}
	app.Tracks = tracks
	app.TracksSyncedAt = &syncedAt
	f.rows[key] = app
	return app, nil
}

func (f *fakeHTTPMobileStoreAppStore) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []domain.MobileStoreApp
	for _, app := range f.rows {
		if app.RepositoryID == repositoryID {
			out = append(out, app)
		}
	}
	return out, nil
}

func (f *fakeHTTPMobileStoreAppStore) ListAll(_ context.Context) ([]domain.MobileStoreApp, error) {
	out := make([]domain.MobileStoreApp, 0, len(f.rows))
	for _, app := range f.rows {
		out = append(out, app)
	}
	return out, nil
}

// TestListStoreCredentialsNeverLeaksPayload proves the HTTP layer only ever
// serializes the storeops.CredentialView projection — never a stored
// payload — even when a row's raw bytes are seeded directly (bypassing
// SaveCredential's encryption), so an accidental leak in the handler would
// actually be caught here rather than masked by encryption.
func TestListStoreCredentialsNeverLeaksPayload(t *testing.T) {
	const secret = "super-secret-p8-key-material-must-never-appear"
	creds := newFakeStoreCredentialStore()
	if err := creds.Set(context.Background(), "asc", []byte(secret)); err != nil {
		t.Fatal(err)
	}

	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Credentials: creds})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/store/credentials", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	if strings.Contains(body, secret) {
		t.Fatalf("response leaked the stored payload: %s", body)
	}
	if strings.Contains(body, `"data"`) {
		t.Fatalf("response carries a data field: %s", body)
	}
	if !strings.Contains(body, `"provider":"asc"`) || !strings.Contains(body, `"configured":true`) {
		t.Fatalf("expected asc configured true in response: %s", body)
	}
	if !strings.Contains(body, `"provider":"google_play"`) || !strings.Contains(body, `"configured":false`) {
		t.Fatalf("expected google_play configured false (ruling 1: known providers always listed): %s", body)
	}
}

// TestRegisterStoreOpsRoutesNoopWithoutService confirms the route group is
// skipped entirely when storeOpsSvc is nil — matching the graceful-degrade
// pattern the other opt-in route groups follow (registerDeployRoutes,
// registerProdOpsRoutes).
func TestRegisterStoreOpsRoutesNoopWithoutService(t *testing.T) {
	h := &Handler{}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/store/credentials", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404 (route must not be registered)", resp.StatusCode)
	}
}

// TestSaveStoreCredentialRejectsUnknownProviderWith400 proves the caller's
// own bad input (a provider that isn't asc/google_play) reports 400, not
// 500 — the validation-class branch of storeOpsCredentialError.
func TestSaveStoreCredentialRejectsUnknownProviderWith400(t *testing.T) {
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Credentials: newFakeStoreCredentialStore()})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	req := httptest.NewRequest("PUT", "/v1/store/credentials/not-a-real-provider", strings.NewReader(`{"data":{"key_id":"x"}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestSaveStoreCredentialReturns500OnInfraFailure proves a backend/config
// failure (here: no cipher configured — the runtime's own graceful-degrade
// default when MCP_SECRETS_KEY/SERVER_API_KEY is absent) reports 500, not
// 400 — it is not the caller's input that's wrong.
func TestSaveStoreCredentialReturns500OnInfraFailure(t *testing.T) {
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Credentials: newFakeStoreCredentialStore()})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	req := httptest.NewRequest("PUT", "/v1/store/credentials/asc", strings.NewReader(`{"data":{"key_id":"K1","issuer_id":"I1","p8":"fake"}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

// TestListStoreAppsReturnsTheRegistryRows exercises the GET route end to
// end: the JSON response carries the registry row AppsByRepository loaded.
func TestListStoreAppsReturnsTheRegistryRows(t *testing.T) {
	apps := newFakeHTTPMobileStoreAppStore()
	repoID := uuid.New()
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.ios",
		State:        domain.MobileStoreStateOnboarding,
	}); err != nil {
		t.Fatal(err)
	}

	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: apps})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/store/apps", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if body := string(raw); !strings.Contains(body, "com.example.ios") {
		t.Fatalf("expected the registry row in the response: %s", body)
	}
}

// TestListStoreAppsRejectsInvalidRepositoryID covers the uuid.Parse guard.
func TestListStoreAppsRejectsInvalidRepositoryID(t *testing.T) {
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: newFakeHTTPMobileStoreAppStore()})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/not-a-uuid/store/apps", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestVerifyStoreOnboardingRejectsInvalidRepositoryID covers the uuid.Parse
// guard on the verify route.
func TestVerifyStoreOnboardingRejectsInvalidRepositoryID(t *testing.T) {
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: newFakeHTTPMobileStoreAppStore()})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("POST", "/v1/repositories/not-a-uuid/store/apps/ios/verify", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestVerifyStoreOnboardingPropagatesServiceError proves the route actually
// reaches storeOpsSvc.VerifyOnboarding: a repository/platform with no
// registry row is a 404, not the blanket 400 this route used to report for
// every failure alike.
func TestVerifyStoreOnboardingPropagatesServiceError(t *testing.T) {
	repoID := uuid.New()
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: newFakeHTTPMobileStoreAppStore()})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/store/apps/ios/verify", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404 (no registered app row for this repo/platform)", resp.StatusCode)
	}
}

// TestVerifyStoreOnboardingRejectsUnknownPlatformWith400 is the caller-input
// half of the split: the platform is a URL segment, so a typo is a 400.
func TestVerifyStoreOnboardingRejectsUnknownPlatformWith400(t *testing.T) {
	repoID := uuid.New()
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: newFakeHTTPMobileStoreAppStore()})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/store/apps/windows-phone/verify", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestListStoreAppsReports500OnAnInfraFailure is the other half of the split
// this route used to get wrong: a database failure is not the caller telling
// us something malformed, and reporting it as 400 sends whoever is debugging
// it looking at the request instead of the backend.
func TestListStoreAppsReports500OnAnInfraFailure(t *testing.T) {
	apps := newFakeHTTPMobileStoreAppStore()
	apps.listErr = errors.New("connection refused")

	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: apps})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+uuid.New().String()+"/store/apps", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

// TestListStoreCredentialsReportsUnsavedProvidersEndToEnd walks the
// "declared but never saved" case all the way out of the HTTP layer: an
// empty vault still returns one row per known provider with
// configured:false and a zero (never omitted) updated_at, which is what the
// settings UI keys its "Not configured" badge off.
func TestListStoreCredentialsReportsUnsavedProvidersEndToEnd(t *testing.T) {
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Credentials: newFakeStoreCredentialStore()})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/store/credentials", nil))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var views []storeops.CredentialView
	if err := json.Unmarshal(raw, &views); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, raw)
	}
	if len(views) != 2 {
		t.Fatalf("got %d views, want one per known provider: %s", len(views), raw)
	}
	for _, v := range views {
		if v.Configured {
			t.Errorf("%s reported configured on an empty vault", v.Provider)
		}
	}
	if !strings.Contains(string(raw), `"updated_at"`) {
		t.Errorf("updated_at must always be serialized, never omitted: %s", raw)
	}
}

// fakeDeployTargetStore is a save-observing port.DeployTargetStore for the
// re-point refusal test below.
type fakeDeployTargetStore struct {
	saved *domain.DeployTarget
}

var _ port.DeployTargetStore = (*fakeDeployTargetStore)(nil)

func (f *fakeDeployTargetStore) ListByRepository(context.Context, uuid.UUID) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeDeployTargetStore) ListAll(context.Context) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeDeployTargetStore) Get(context.Context, uuid.UUID, string, string) (domain.DeployTarget, error) {
	return domain.DeployTarget{}, port.ErrNotFound
}
func (f *fakeDeployTargetStore) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	saved := t
	f.saved = &saved
	return t, nil
}
func (f *fakeDeployTargetStore) Delete(context.Context, uuid.UUID, string, string) error { return nil }

// TestSaveDeployTargetRefusingARePointReaches4xx walks the whole operator
// path: PUT the deploy target with a new bundle_id for a repository already
// live under a different one. Before the guard existed this returned 200 —
// the target persisted the new identifier, the store row kept `live` under
// the old one, and mobileStoreGate (state-keyed, not identifier-keyed) stayed
// green. The refusal must reach the caller as a 4xx they can act on.
func TestSaveDeployTargetRefusingARePointReaches4xx(t *testing.T) {
	repoID := uuid.New()
	apps := newFakeHTTPMobileStoreAppStore()
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.published",
		State:        domain.MobileStoreStateLive,
	}); err != nil {
		t.Fatal(err)
	}

	storeSvc := storeops.NewService(storeops.Deps{Apps: apps})
	targets := &fakeDeployTargetStore{}
	deploySvc := deploy.NewService(targets, nil)
	deploySvc.SetStoreIdentifierGuard(storeSvc.EnsureIdentifierAllowed)

	h := &Handler{deploySvc: deploySvc}
	app := fiber.New()
	h.registerDeployRoutes(app)

	body := `{"env":"prod","provider":"app_store","vars":{"bundle_id":"com.example.repointed","app_name":"MyApp"}}`
	req := httptest.NewRequest("PUT", "/v1/repositories/"+repoID.String()+"/deploy/targets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode < 400 || resp.StatusCode > 499 {
		t.Fatalf("status = %d, want a 4xx", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "com.example.published") {
		t.Errorf("response must tell the operator what it is already live as: %s", raw)
	}
	if targets.saved != nil {
		t.Fatalf("the deploy target must not be persisted: %+v", targets.saved)
	}
}

// The same route must still save an unrelated store target normally — the
// guard is a refusal for one specific state, not a block on store saves.
func TestSaveDeployTargetStillSavesWhenTheGuardPasses(t *testing.T) {
	storeSvc := storeops.NewService(storeops.Deps{Apps: newFakeHTTPMobileStoreAppStore()})
	targets := &fakeDeployTargetStore{}
	deploySvc := deploy.NewService(targets, nil)
	deploySvc.SetStoreIdentifierGuard(storeSvc.EnsureIdentifierAllowed)

	h := &Handler{deploySvc: deploySvc}
	app := fiber.New()
	h.registerDeployRoutes(app)

	body := `{"env":"prod","provider":"app_store","vars":{"bundle_id":"com.example.fresh","app_name":"MyApp"}}`
	req := httptest.NewRequest("PUT", "/v1/repositories/"+uuid.New().String()+"/deploy/targets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if targets.saved == nil {
		t.Fatal("the target must be saved when the guard passes")
	}
}

// fakeHTTPRepositoryResolver is a minimal in-memory storeops.RepositoryResolver
// for the store release action handler tests below.
type fakeHTTPRepositoryResolver struct {
	rows map[uuid.UUID]domain.Repository
}

var _ storeops.RepositoryResolver = (*fakeHTTPRepositoryResolver)(nil)

func newFakeHTTPRepositoryResolver() *fakeHTTPRepositoryResolver {
	return &fakeHTTPRepositoryResolver{rows: map[uuid.UUID]domain.Repository{}}
}

func (f *fakeHTTPRepositoryResolver) set(repo domain.Repository) {
	f.rows[repo.ID] = repo
}

func (f *fakeHTTPRepositoryResolver) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	repo, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("get repository: %w", port.ErrNotFound)
	}
	return repo, nil
}

func (f *fakeHTTPRepositoryResolver) List(_ context.Context) ([]domain.Repository, error) {
	out := make([]domain.Repository, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

// TestSubmitIOSForReviewRequiresConfirmWith400 proves a missing confirm
// phrase on a production-class store release route reaches the caller as
// 400, not a 500 and not silently ignored.
func TestSubmitIOSForReviewRequiresConfirmWith400(t *testing.T) {
	repoID := uuid.New()
	repos := newFakeHTTPRepositoryResolver()
	repos.set(domain.Repository{ID: repoID, Name: "tasktrooper"})
	apps := newFakeHTTPMobileStoreAppStore()
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.ios",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateLive,
	}); err != nil {
		t.Fatal(err)
	}

	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: apps, Repos: repos})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/store/apps/ios/submit", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestSubmitIOSForReviewWithMissingCredentialReports424 proves a store
// release action that cannot build its store client — here, no App Store
// Connect credential was ever saved — reports 424 Failed Dependency rather
// than a generic 500, so the console can link the operator straight to the
// credentials section.
func TestSubmitIOSForReviewWithMissingCredentialReports424(t *testing.T) {
	repoID := uuid.New()
	repos := newFakeHTTPRepositoryResolver()
	repos.set(domain.Repository{ID: repoID, Name: "tasktrooper"})
	apps := newFakeHTTPMobileStoreAppStore()
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.ios",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateLive,
	}); err != nil {
		t.Fatal(err)
	}

	// No Cipher/Credentials/NewASC wired at all: svc.asc(ctx) fails
	// regardless of the precise reason, and SubmitIOS wraps that as
	// storeops.ErrStoreCredentialUnavailable.
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: apps, Repos: repos})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/store/apps/ios/submit", strings.NewReader(`{"confirm":"tasktrooper"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusFailedDependency {
		t.Fatalf("status = %d, want 424", resp.StatusCode)
	}
}

// TestSetAndroidRolloutRejectsOutOfRangeFractionWith400 proves the
// service's server-side clamp on the rollout fraction reaches the caller as
// 400 — the browser is not the enforcement point for a fraction heading to
// a production Play Developer API call.
func TestSetAndroidRolloutRejectsOutOfRangeFractionWith400(t *testing.T) {
	repoID := uuid.New()
	repos := newFakeHTTPRepositoryResolver()
	repos.set(domain.Repository{ID: repoID, Name: "tasktrooper"})

	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: newFakeHTTPMobileStoreAppStore(), Repos: repos})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/store/apps/android/rollout", strings.NewReader(`{"user_fraction":2}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestActionRoutesAreNotShadowedByVerifyRoute is a regression test for the
// exact route-ordering trap this package's route table warns about: the
// literal ios/android action segments (e.g. .../android/resume) must be
// registered ahead of the :platform/verify route, or :platform would
// swallow them and every action route would 404 with fiber's own "Cannot
// POST" text instead of ever reaching the handler.
func TestActionRoutesAreNotShadowedByVerifyRoute(t *testing.T) {
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{
		Apps:  newFakeHTTPMobileStoreAppStore(),
		Repos: newFakeHTTPRepositoryResolver(),
	})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("POST", "/v1/repositories/"+uuid.New().String()+"/store/apps/android/resume", nil))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Cannot POST") {
		t.Fatalf("android/resume route was shadowed by :platform/verify: %s", raw)
	}
}

// TestListAllStoreAppsJoinsRepositoryNames proves ListAllStoreApps returns
// the AllApps join end to end: the repository name is present alongside the
// app row, not just the raw registry fields.
func TestListAllStoreAppsJoinsRepositoryNames(t *testing.T) {
	repoID := uuid.New()
	repos := newFakeHTTPRepositoryResolver()
	repos.set(domain.Repository{ID: repoID, Name: "tasktrooper"})
	apps := newFakeHTTPMobileStoreAppStore()
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.ios",
		State:        domain.MobileStoreStateLive,
	}); err != nil {
		t.Fatal(err)
	}

	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{Apps: apps, Repos: repos})}
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/operations/apps", nil))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"repository_name":"tasktrooper"`) {
		t.Fatalf("expected the joined repository name in the response: %s", raw)
	}
}

// stubPlayClient is a port.GooglePlayClient whose only scripted answer is
// ListApps — enough to drive the listing route's 200-vs-5xx split without
// standing up the whole Play surface.
type stubPlayClient struct {
	listErr error
	apps    []port.StoreAppRef
}

var _ port.GooglePlayClient = (*stubPlayClient)(nil)

func (s *stubPlayClient) ValidateAuth(context.Context) error              { return nil }
func (s *stubPlayClient) AppExists(context.Context, string) (bool, error) { return true, nil }
func (s *stubPlayClient) TrackInfo(context.Context, string, string) (port.PlayTrackInfo, error) {
	return port.PlayTrackInfo{}, nil
}
func (s *stubPlayClient) LiveVersion(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (s *stubPlayClient) PromoteTrack(context.Context, string, string, string, float64) error {
	return nil
}
func (s *stubPlayClient) SetRolloutFraction(context.Context, string, string, float64) error {
	return nil
}
func (s *stubPlayClient) HaltRollout(context.Context, string, string) error   { return nil }
func (s *stubPlayClient) ResumeRollout(context.Context, string, string) error { return nil }
func (s *stubPlayClient) ListApps(context.Context) ([]port.StoreAppRef, error) {
	return s.apps, s.listErr
}
func (s *stubPlayClient) Tracks(context.Context, string) (domain.StoreTracks, error) {
	return domain.StoreTracks{}, nil
}

// newListingHandler wires a handler whose Google Play credential is saved and
// whose client answers with play.
func newListingHandler(t *testing.T, play *stubPlayClient) *Handler {
	t.Helper()
	cipher, err := secrets.NewCipher([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	svc := storeops.NewService(storeops.Deps{
		Credentials: newFakeStoreCredentialStore(),
		Apps:        newFakeHTTPMobileStoreAppStore(),
		Repos:       newFakeHTTPRepositoryResolver(),
		Cipher:      cipher,
		NewPlay: func(domain.StoreCredential) (port.GooglePlayClient, error) {
			return play, nil
		},
	})
	if err := svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay,
		map[string]string{"service_account_json": "{}"}); err != nil {
		t.Fatal(err)
	}
	return &Handler{storeOpsSvc: svc}
}

// A credential that cannot enumerate is a 200 saying so, never a 5xx: the Play
// Developer API has no listing endpoint, and the operator's next move is to
// type the package name, not to retry. A 500 here would send them to a retry
// loop that can never succeed.
func TestListStoreCredentialAppsAnswers200WhenListingIsUnavailable(t *testing.T) {
	h := newListingHandler(t, &stubPlayClient{listErr: port.ErrAppListingUnavailable})
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/store/credentials/google_play/apps", nil))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), `"listing_available":false`) {
		t.Fatalf("body = %s, want listing_available:false", raw)
	}
}

// A real store failure still reports as one — the 200 above is only for the
// credential that structurally cannot list.
func TestListStoreCredentialAppsStillReports500OnARealFailure(t *testing.T) {
	h := newListingHandler(t, &stubPlayClient{listErr: errors.New("play api is down")})
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/store/credentials/google_play/apps", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestListStoreCredentialAppsListsWhatTheStoreReported(t *testing.T) {
	h := newListingHandler(t, &stubPlayClient{apps: []port.StoreAppRef{{Identifier: "com.example.app", Name: "Example"}}})
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/store/credentials/google_play/apps", nil))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK || !strings.Contains(string(raw), `"listing_available":true`) ||
		!strings.Contains(string(raw), "com.example.app") {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
}

// newBindingHandler wires a handler over one live Android app, so the link,
// tracks, promote and build routes have a row to act on.
func newBindingHandler(t *testing.T, repoID uuid.UUID, play *stubPlayClient) (*Handler, *storeops.Service) {
	t.Helper()
	cipher, err := secrets.NewCipher([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	repos := newFakeHTTPRepositoryResolver()
	repos.set(domain.Repository{ID: repoID, Name: "trooper", Kind: domain.RepoKindMobile})
	apps := newFakeHTTPMobileStoreAppStore()
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
	}); err != nil {
		t.Fatal(err)
	}
	svc := storeops.NewService(storeops.Deps{
		Credentials: newFakeStoreCredentialStore(),
		Apps:        apps,
		Repos:       repos,
		Cipher:      cipher,
		NewPlay: func(domain.StoreCredential) (port.GooglePlayClient, error) {
			return play, nil
		},
	})
	if err := svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay,
		map[string]string{"service_account_json": "{}"}); err != nil {
		t.Fatal(err)
	}
	return &Handler{storeOpsSvc: svc}, svc
}

// A promotion that skips the middle channel is the caller's own mistake, and
// the console branches on 400 to say so — a 500 would send them to a bug report.
func TestPromoteStoreChannelRejectsASkippedChannelWith400(t *testing.T) {
	repoID := uuid.New()
	h, _ := newBindingHandler(t, repoID, &stubPlayClient{})
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/store/apps/android/promote",
		strings.NewReader(`{"from":"internal","to":"production","confirm":"trooper"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, raw)
	}
}

// The literal android/promote route shadows the generic :platform one, so an
// Android channel promotion has to be recognised there or it can never be made
// at all. This is the regression test for that shadowing.
func TestAndroidPromoteRouteAcceptsTheChannelBody(t *testing.T) {
	repoID := uuid.New()
	play := &stubPlayClient{}
	h, _ := newBindingHandler(t, repoID, play)
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/store/apps/android/promote",
		strings.NewReader(`{"from":"internal","to":"external"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 204: %s", resp.StatusCode, raw)
	}
}

// No engine to build on is a conflict with the state of the world, not a
// server fault: the fix is to settle a bill or open a laptop, and a 500 would
// tell the operator to file a bug instead.
func TestStartStoreBuildAnswers409WhenNoEngineCanRun(t *testing.T) {
	repoID := uuid.New()
	h, svc := newBindingHandler(t, repoID, &stubPlayClient{})
	svc.SetEngineProbes(
		func(context.Context, domain.Repository, string, string) error {
			return storeops.ErrActionsBillingBlocked
		},
		func(context.Context) (storeops.LocalRunnerHost, error) { return storeops.LocalRunnerHost{}, nil },
	)
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/store/apps/android/build",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 409: %s", resp.StatusCode, raw)
	}
}

// The link route writes the binding and hands the row back, and the identifier
// it confirms is the one the store answered for.
func TestLinkStoreAppRouteWritesTheBinding(t *testing.T) {
	repoID := uuid.New()
	h, _ := newBindingHandler(t, repoID, &stubPlayClient{})
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	req := httptest.NewRequest("PUT", "/v1/repositories/"+repoID.String()+"/store/apps/android/link",
		strings.NewReader(`{"identifier":"com.example.android","name":"Trooper"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK || !strings.Contains(string(raw), `"app_name":"Trooper"`) {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
}

// The tracks route reads the channels through the service, so the response
// carries all three even when the store reported nothing on them.
func TestStoreAppTracksRouteReturnsAllThreeChannels(t *testing.T) {
	repoID := uuid.New()
	h, _ := newBindingHandler(t, repoID, &stubPlayClient{})
	app := fiber.New()
	h.registerStoreOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/store/apps/android/tracks", nil))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, raw)
	}
	for _, channel := range []string{`"internal"`, `"external"`, `"production"`} {
		if !strings.Contains(string(raw), channel) {
			t.Fatalf("body = %s, want %s", raw, channel)
		}
	}
}
