package storeops_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type releaseFixture struct {
	repoID uuid.UUID
	repos  *fakeRepositoryResolver
	apps   *fakeMobileStoreAppStore
	asc    *fakeASC
	play   *fakePlay
	audit  *fakeOpsAuditStore
}

func (f *releaseFixture) setState(state string) {
	for _, platform := range []string{domain.MobileStorePlatformIOS, domain.MobileStorePlatformAndroid} {
		app, err := f.apps.Get(context.Background(), f.repoID, platform)
		if err != nil {
			continue
		}
		app.State = state
		_, _ = f.apps.Upsert(context.Background(), app)
	}
}

func newReleaseTestService(t *testing.T, repoName string) (*storeops.Service, *releaseFixture) {
	t.Helper()

	repoID := uuid.New()
	repos := newFakeRepositoryResolver()
	repos.set(domain.Repository{ID: repoID, Name: repoName})

	apps := newFakeMobileStoreAppStore()
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.ios",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateLive,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
	}); err != nil {
		t.Fatal(err)
	}

	asc := &fakeASC{LatestVersionResult: port.AppStoreVersionInfo{Version: "1.2.3"}}
	play := &fakePlay{}
	audit := newFakeOpsAuditStore()

	cipher, err := secrets.NewCipher([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}

	svc := storeops.NewService(storeops.Deps{
		Credentials: newFakeCredentialStore(),
		Apps:        apps,
		Cipher:      cipher,
		NewASC:      newFakeASCFactory(asc, nil),
		NewPlay:     newFakePlayFactory(play, nil),
		Repos:       repos,
	})
	svc.SetAuditor(audit)

	if err := svc.SaveCredential(context.Background(), domain.StoreCredentialASC, map[string]string{
		"key_id": "K1", "issuer_id": "I1", "p8": "fake-p8",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay, map[string]string{
		"service_account_json": "{}",
	}); err != nil {
		t.Fatal(err)
	}

	return svc, &releaseFixture{repoID: repoID, repos: repos, apps: apps, asc: asc, play: play, audit: audit}
}

func TestSubmitIOSRequiresConfirmPhrase(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	err := svc.SubmitIOS(context.Background(), f.repoID, "wrong", "akif")
	if !errors.Is(err, storeops.ErrConfirmMismatch) {
		t.Fatalf("err = %v, want ErrConfirmMismatch", err)
	}
	if f.asc.submits != 0 {
		t.Fatal("submitted to Apple despite a failed confirmation")
	}
}

func TestSubmitIOSRefusesAnAppThatIsNotOnboarded(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	f.setState(domain.MobileStoreStateOnboarding)
	err := svc.SubmitIOS(context.Background(), f.repoID, "r", "akif")
	if err == nil || f.asc.submits != 0 {
		t.Fatalf("err = %v, submits = %d", err, f.asc.submits)
	}
}

func TestSetAndroidRolloutRejectsFractionOutOfRange(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	// 0 is the dangerous one: Play reads a zero fraction as a completed release.
	for _, bad := range []float64{-0.1, 0, 1.5} {
		err := svc.SetAndroidRollout(context.Background(), f.repoID, bad, "r", "akif")
		if !errors.Is(err, storeops.ErrInvalidRolloutFraction) {
			t.Fatalf("fraction %v: err = %v, want ErrInvalidRolloutFraction", bad, err)
		}
	}
	if len(f.play.SetRolloutFractionCalls) != 0 {
		t.Fatalf("an out-of-range fraction reached Play: %+v", f.play.SetRolloutFractionCalls)
	}
}

func TestSetAndroidRolloutCompletingItRequiresTheConfirmPhrase(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	ctx := context.Background()
	if err := svc.SetAndroidRollout(ctx, f.repoID, 0.5, "", "akif"); err != nil {
		t.Fatalf("a staged share needs no confirmation: %v", err)
	}
	if err := svc.SetAndroidRollout(ctx, f.repoID, 1, "", "akif"); !errors.Is(err, storeops.ErrConfirmMismatch) {
		t.Fatalf("completing without a confirmation: err = %v, want ErrConfirmMismatch", err)
	}
	if err := svc.SetAndroidRollout(ctx, f.repoID, 1, " tasktrooper ", "akif"); err != nil {
		t.Fatalf("completing with the repository name: %v", err)
	}
	calls := f.play.SetRolloutFractionCalls
	if len(calls) != 2 || calls[0].UserFraction != 0.5 || calls[1].UserFraction != 1 {
		t.Fatalf("SetRolloutFractionCalls = %+v, want 0.5 then 1", calls)
	}
}

func TestPromoteAndroidRequiresConfirmOnlyForProduction(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	if err := svc.PromoteAndroid(context.Background(), f.repoID, "internal", 1, "", "akif"); err != nil {
		t.Fatalf("internal promote should not need a confirmation: %v", err)
	}
	if err := svc.PromoteAndroid(context.Background(), f.repoID, "production", 1, "", "akif"); !errors.Is(err, storeops.ErrConfirmMismatch) {
		t.Fatalf("production promote err = %v, want ErrConfirmMismatch", err)
	}
}

func TestRefusedStoreActionIsAudited(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	_ = svc.HaltAndroid(context.Background(), f.repoID, "nope", "akif")
	if len(f.audit.entries) != 1 || f.audit.entries[0].Outcome != domain.OpsOutcomeError {
		t.Fatalf("audit = %+v", f.audit.entries)
	}
}

func TestSubmitIOSPropagatesAppNotFound(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	otherRepo := uuid.New()
	f.repos.set(domain.Repository{ID: otherRepo, Name: "other"})
	err := svc.SubmitIOS(context.Background(), otherRepo, "other", "akif")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want port.ErrNotFound", err)
	}
	if len(f.audit.entries) != 0 {
		t.Fatalf("a not-found app must not be audited: %+v", f.audit.entries)
	}
}

func TestSubmitIOSSucceedsAndAudits(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	if err := svc.SubmitIOS(context.Background(), f.repoID, "tasktrooper", "akif"); err != nil {
		t.Fatalf("SubmitIOS: %v", err)
	}
	if f.asc.submits != 1 {
		t.Fatalf("submits = %d, want 1", f.asc.submits)
	}
	if len(f.audit.entries) != 1 || f.audit.entries[0].Outcome != domain.OpsOutcomeOK {
		t.Fatalf("audit = %+v", f.audit.entries)
	}
	app, err := f.apps.Get(context.Background(), f.repoID, domain.MobileStorePlatformIOS)
	if err != nil {
		t.Fatal(err)
	}
	if app.ReviewState != domain.ReviewStateWaiting {
		t.Fatalf("review_state = %q, want %q so the monitor follows the review", app.ReviewState, domain.ReviewStateWaiting)
	}
}

func TestReleaseIOSRequiresConfirmPhrase(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	err := svc.ReleaseIOS(context.Background(), f.repoID, "wrong", "akif")
	if !errors.Is(err, storeops.ErrConfirmMismatch) {
		t.Fatalf("err = %v, want ErrConfirmMismatch", err)
	}
	if f.asc.releases != 0 {
		t.Fatal("released despite a failed confirmation")
	}
}

func TestResumeAndroidNeedsNoConfirmation(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	if err := svc.ResumeAndroid(context.Background(), f.repoID, "akif"); err != nil {
		t.Fatalf("ResumeAndroid: %v", err)
	}
	if len(f.play.ResumeRolloutCalls) != 1 {
		t.Fatalf("ResumeRolloutCalls = %+v", f.play.ResumeRolloutCalls)
	}
}

func TestHaltAndroidWithNoCredentialReportsCredentialUnavailable(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	if err := svc.DeleteCredential(context.Background(), domain.StoreCredentialGooglePlay); err != nil {
		t.Fatal(err)
	}
	err := svc.HaltAndroid(context.Background(), f.repoID, "tasktrooper", "akif")
	if !errors.Is(err, storeops.ErrStoreCredentialUnavailable) {
		t.Fatalf("err = %v, want ErrStoreCredentialUnavailable", err)
	}
}
