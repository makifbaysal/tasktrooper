package githubauth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeTokens struct{ pat string }

func (f *fakeTokens) GitHubToken(context.Context) (string, error)      { return f.pat, nil }
func (f *fakeTokens) SetGitHubToken(_ context.Context, t string) error { f.pat = t; return nil }
func (f *fakeTokens) DeleteGitHubToken(context.Context) error          { f.pat = ""; return nil }

type fakeApps struct {
	mu   sync.Mutex
	auth *domain.GitHubAppAuth
}

func (f *fakeApps) GitHubAppAuth(context.Context) (domain.GitHubAppAuth, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.auth == nil {
		return domain.GitHubAppAuth{}, false, nil
	}
	return *f.auth, true, nil
}
func (f *fakeApps) SetGitHubAppAuth(_ context.Context, a domain.GitHubAppAuth) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = &a
	return nil
}
func (f *fakeApps) DeleteGitHubAppAuth(context.Context) error { f.auth = nil; return nil }

type fakeGitHub struct {
	mu            sync.Mutex
	polls         []domain.GitHubDevicePoll
	pollCalls     int
	refreshCalls  int
	refreshErr    error
	installations int
}

func (f *fakeGitHub) RequestDeviceCode(context.Context, string) (domain.GitHubDeviceCode, error) {
	return domain.GitHubDeviceCode{DeviceCode: "dev-1", UserCode: "ABCD-1234", VerificationURI: "https://github.com/login/device", ExpiresIn: 15 * time.Minute, Interval: 5 * time.Second}, nil
}

func (f *fakeGitHub) PollDeviceToken(context.Context, string, string) (domain.GitHubUserToken, domain.GitHubDevicePoll, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.polls[f.pollCalls]
	f.pollCalls++
	if p.State == domain.GitHubDeviceAuthorized {
		return domain.GitHubUserToken{AccessToken: "ghu_1", RefreshToken: "ghr_1", ExpiresAt: time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)}, p, nil
	}
	return domain.GitHubUserToken{}, p, nil
}

func (f *fakeGitHub) RefreshUserToken(_ context.Context, _, refresh string) (domain.GitHubUserToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls++
	if f.refreshErr != nil {
		return domain.GitHubUserToken{}, f.refreshErr
	}
	return domain.GitHubUserToken{AccessToken: "ghu_refreshed", RefreshToken: "ghr_rotated", ExpiresAt: time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC)}, nil
}

func (f *fakeGitHub) Login(context.Context, string) (string, error) { return "akif", nil }

func (f *fakeGitHub) Installations(context.Context, string) (int, error) { return f.installations, nil }

type fixture struct {
	svc    *Service
	tokens *fakeTokens
	apps   *fakeApps
	gh     *fakeGitHub
	now    time.Time
}

func newFixture() *fixture {
	f := &fixture{
		tokens: &fakeTokens{},
		apps:   &fakeApps{},
		gh:     &fakeGitHub{installations: 1},
		now:    time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	}
	f.svc = New(f.tokens, f.apps, f.gh, domain.GitHubAppConfig{ClientID: "Iv23test", Slug: "tasktrooper"})
	f.svc.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) connect(expiresAt time.Time) {
	f.apps.auth = &domain.GitHubAppAuth{
		Token:    domain.GitHubUserToken{AccessToken: "ghu_old", RefreshToken: "ghr_old", ExpiresAt: expiresAt, RefreshExpiresAt: f.now.Add(180 * 24 * time.Hour)},
		Login:    "akif",
		ClientID: "Iv23test",
	}
}

func TestDeviceFlowConnectsOnceTheUserAuthorizes(t *testing.T) {
	f := newFixture()
	f.tokens.pat = "ghp_old"
	f.gh.polls = []domain.GitHubDevicePoll{{State: domain.GitHubDevicePending}, {State: domain.GitHubDeviceAuthorized}}

	started, err := f.svc.StartDeviceFlow(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ABCD-1234", started.UserCode)

	early, err := f.svc.PollDeviceFlow(context.Background(), started.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.GitHubDevicePending, early.State)
	assert.Zero(t, f.gh.pollCalls, "never faster than GitHub's interval")

	f.now = f.now.Add(5 * time.Second)
	pending, _ := f.svc.PollDeviceFlow(context.Background(), started.ID)
	assert.Equal(t, domain.GitHubDevicePending, pending.State)

	f.now = f.now.Add(5 * time.Second)
	done, err := f.svc.PollDeviceFlow(context.Background(), started.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.GitHubDeviceAuthorized, done.State)
	assert.Equal(t, "akif", done.Login)
	require.NotNil(t, f.apps.auth)
	assert.Equal(t, "Iv23test", f.apps.auth.ClientID)
	assert.Empty(t, f.tokens.pat, "the pasted token is retired once the app connects")

	token, err := f.svc.GitHubToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ghu_1", token)
}

func TestDeviceFlowNeedsAConfiguredApp(t *testing.T) {
	f := newFixture()
	f.svc.app = domain.GitHubAppConfig{}

	_, err := f.svc.StartDeviceFlow(context.Background())

	assert.ErrorIs(t, err, domain.ErrGitHubAppNotConfigured)
}

func TestDeviceFlowSlowsDownWhenGitHubAsks(t *testing.T) {
	f := newFixture()
	f.gh.polls = []domain.GitHubDevicePoll{{State: domain.GitHubDevicePending, Interval: 10 * time.Second}, {State: domain.GitHubDevicePending}}
	started, _ := f.svc.StartDeviceFlow(context.Background())

	f.now = f.now.Add(5 * time.Second)
	slowed, _ := f.svc.PollDeviceFlow(context.Background(), started.ID)
	assert.Equal(t, 10, slowed.IntervalSeconds)

	f.now = f.now.Add(5 * time.Second)
	_, _ = f.svc.PollDeviceFlow(context.Background(), started.ID)
	assert.Equal(t, 1, f.gh.pollCalls, "the slower interval holds")
}

func TestATokenNearExpiryIsRefreshedAndTheRotatedPairStored(t *testing.T) {
	f := newFixture()
	f.connect(f.now.Add(2 * time.Minute))

	token, err := f.svc.GitHubToken(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "ghu_refreshed", token)
	assert.Equal(t, "ghr_rotated", f.apps.auth.Token.RefreshToken)
	assert.Equal(t, "akif", f.apps.auth.Login)
}

func TestAFreshTokenIsUsedAsIs(t *testing.T) {
	f := newFixture()
	f.connect(f.now.Add(time.Hour))

	token, err := f.svc.GitHubToken(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "ghu_old", token)
	assert.Zero(t, f.gh.refreshCalls)
}

func TestConcurrentCallersRefreshOnce(t *testing.T) {
	f := newFixture()
	f.connect(f.now.Add(time.Minute))

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.svc.GitHubToken(context.Background())
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, f.gh.refreshCalls, "a rotated refresh token may be spent once")
}

func TestARefusedRefreshEndsTheConnection(t *testing.T) {
	f := newFixture()
	f.connect(f.now.Add(-time.Minute))
	f.gh.refreshErr = domain.ErrGitHubConnectionExpired

	_, err := f.svc.GitHubToken(context.Background())

	assert.ErrorIs(t, err, domain.ErrGitHubConnectionExpired)
}

func TestANetworkBlipKeepsTheStillValidToken(t *testing.T) {
	f := newFixture()
	f.connect(f.now.Add(2 * time.Minute))
	f.gh.refreshErr = errors.New("dial tcp: timeout")

	token, err := f.svc.GitHubToken(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "ghu_old", token)
}

func TestWithoutAnAppConnectionThePastedTokenIsUsed(t *testing.T) {
	f := newFixture()
	f.tokens.pat = "ghp_pasted"

	token, err := f.svc.GitHubToken(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "ghp_pasted", token)
}

func TestSavingATokenReplacesTheAppConnectionAndDisconnectClearsBoth(t *testing.T) {
	f := newFixture()
	f.connect(f.now.Add(time.Hour))

	require.NoError(t, f.svc.SetGitHubToken(context.Background(), "ghp_new"))
	assert.Nil(t, f.apps.auth)
	token, _ := f.svc.GitHubToken(context.Background())
	assert.Equal(t, "ghp_new", token)

	f.connect(f.now.Add(time.Hour))
	require.NoError(t, f.svc.DeleteGitHubToken(context.Background()))
	assert.Nil(t, f.apps.auth)
	assert.Empty(t, f.tokens.pat)
}

func TestAppStatusSaysWhenTheAppIsInstalledNowhere(t *testing.T) {
	f := newFixture()
	f.connect(f.now.Add(time.Hour))
	f.gh.installations = 0

	st, ok, err := f.svc.AppStatus(context.Background())

	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, st.NeedsInstall)
	assert.Equal(t, "https://github.com/apps/tasktrooper/installations/new", st.InstallURL)
	assert.Equal(t, domain.GitHubModeApp, st.Mode)
}

func TestTaskTroopersOwnAppIsFixedInCode(t *testing.T) {
	assert.Equal(t, "Iv23lilDpzAS8ZYje1MN", App.ClientID)
	assert.Equal(t, "https://github.com/apps/task-trooper/installations/new", App.InstallURL())
}
