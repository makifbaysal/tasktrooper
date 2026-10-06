// Package githubauth is where every GitHub credential comes from: a GitHub
// App connection made through the device flow (a code typed on github.com,
// renewed in the background), or a pasted personal access token. It satisfies
// port.GitHubTokenStore, so everything that pushes, merges or reads GitHub
// gets a live token without knowing which kind it is.
package githubauth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// App is the GitHub App "Connect with GitHub" signs in through:
// github.com/apps/task-trooper. Fixed in code on purpose — its client id is
// public by design (the device flow takes no secret), and it is not something
// an install chooses.
var App = domain.GitHubAppConfig{ClientID: "Iv23lilDpzAS8ZYje1MN", Slug: "task-trooper"}

type AppStore interface {
	GitHubAppAuth(ctx context.Context) (domain.GitHubAppAuth, bool, error)
	SetGitHubAppAuth(ctx context.Context, auth domain.GitHubAppAuth) error
	DeleteGitHubAppAuth(ctx context.Context) error
}

// GitHub is the github.com login surface (githubapi.DeviceClient).
type GitHub interface {
	RequestDeviceCode(ctx context.Context, clientID string) (domain.GitHubDeviceCode, error)
	PollDeviceToken(ctx context.Context, clientID, deviceCode string) (domain.GitHubUserToken, domain.GitHubDevicePoll, error)
	RefreshUserToken(ctx context.Context, clientID, refreshToken string) (domain.GitHubUserToken, error)
	Login(ctx context.Context, token string) (string, error)
	Installations(ctx context.Context, token string) (int, error)
}

// refreshMargin renews a token this long before it expires, so a push that
// starts on a nearly-expired token does not die halfway.
const refreshMargin = 5 * time.Minute

// flowRetention keeps a finished flow readable for the UI's last poll.
const flowRetention = 15 * time.Minute

type Service struct {
	tokens port.GitHubTokenStore
	apps   AppStore
	gh     GitHub
	app    domain.GitHubAppConfig
	now    func() time.Time

	// refreshMu serializes refreshes: GitHub rotates the refresh token on
	// every use, so two concurrent refreshes would leave one holding a token
	// GitHub has already retired.
	refreshMu sync.Mutex

	flowsMu sync.Mutex
	flows   map[string]*flow
}

type flow struct {
	public     domain.GitHubDeviceFlow
	clientID   string
	deviceCode string
	interval   time.Duration
	nextPoll   time.Time
}

var _ port.GitHubTokenStore = (*Service)(nil)

func New(tokens port.GitHubTokenStore, apps AppStore, gh GitHub, app domain.GitHubAppConfig) *Service {
	return &Service{tokens: tokens, apps: apps, gh: gh, app: app, now: time.Now, flows: map[string]*flow{}}
}

// GitHubToken is the token every GitHub call uses: the app connection's,
// renewed when it is about to expire, else the pasted token.
func (s *Service) GitHubToken(ctx context.Context) (string, error) {
	auth, ok, err := s.apps.GitHubAppAuth(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		return s.tokens.GitHubToken(ctx)
	}
	if auth.Token.Fresh(s.now(), refreshMargin) {
		return auth.Token.AccessToken, nil
	}
	return s.refresh(ctx)
}

func (s *Service) refresh(ctx context.Context) (string, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	auth, ok, err := s.apps.GitHubAppAuth(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		return s.tokens.GitHubToken(ctx)
	}
	now := s.now()
	if auth.Token.Fresh(now, refreshMargin) {
		return auth.Token.AccessToken, nil
	}
	if auth.Token.RefreshToken == "" || (!auth.Token.RefreshExpiresAt.IsZero() && now.After(auth.Token.RefreshExpiresAt)) {
		return "", domain.ErrGitHubConnectionExpired
	}
	fresh, err := s.gh.RefreshUserToken(ctx, auth.ClientID, auth.Token.RefreshToken)
	if err != nil {
		// A network blip must not stop work while the current token still
		// holds; only a refused refresh ends the connection.
		if !errors.Is(err, domain.ErrGitHubConnectionExpired) && auth.Token.Fresh(now, 0) {
			log.Warn().Err(err).Msg("github: refreshing the app token failed, using the current one until it expires")
			return auth.Token.AccessToken, nil
		}
		return "", err
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken, fresh.RefreshExpiresAt = auth.Token.RefreshToken, auth.Token.RefreshExpiresAt
	}
	auth.Token = fresh
	if err := s.apps.SetGitHubAppAuth(ctx, auth); err != nil {
		return "", fmt.Errorf("store the refreshed github token: %w", err)
	}
	return fresh.AccessToken, nil
}

// SetGitHubToken switches to a pasted token: an app connection left in place
// would keep winning over it.
func (s *Service) SetGitHubToken(ctx context.Context, token string) error {
	if err := s.apps.DeleteGitHubAppAuth(ctx); err != nil {
		return err
	}
	return s.tokens.SetGitHubToken(ctx, token)
}

// DeleteGitHubToken disconnects GitHub altogether.
func (s *Service) DeleteGitHubToken(ctx context.Context) error {
	if err := s.apps.DeleteGitHubAppAuth(ctx); err != nil {
		return err
	}
	return s.tokens.DeleteGitHubToken(ctx)
}

// Status is the connection as the settings card shows it.
type Status struct {
	Mode         domain.GitHubConnectionMode `json:"mode,omitempty"`
	Login        string                      `json:"login,omitempty"`
	AppAvailable bool                        `json:"app_available"`
	InstallURL   string                      `json:"install_url,omitempty"`
	// NeedsInstall: the app is authorized but installed nowhere, so the
	// token reaches no repository yet.
	NeedsInstall bool `json:"needs_install,omitempty"`
	Expired      bool `json:"expired,omitempty"`
}

// AppStatus answers for an app connection only; ok is false when GitHub is
// connected by a pasted token (or not at all), which the caller reports its
// own way.
func (s *Service) AppStatus(ctx context.Context) (Status, bool, error) {
	st := Status{AppAvailable: s.app.Configured(), InstallURL: s.app.InstallURL()}
	auth, ok, err := s.apps.GitHubAppAuth(ctx)
	if err != nil || !ok {
		return st, false, err
	}
	st.Mode, st.Login = domain.GitHubModeApp, auth.Login
	token, err := s.GitHubToken(ctx)
	if errors.Is(err, domain.ErrGitHubConnectionExpired) {
		st.Expired = true
		return st, true, nil
	}
	if err != nil {
		return st, true, err
	}
	if n, ierr := s.gh.Installations(ctx, token); ierr == nil && n == 0 {
		st.NeedsInstall = true
	}
	return st, true, nil
}

// StartDeviceFlow asks GitHub for a code the user types at verification_uri.
func (s *Service) StartDeviceFlow(ctx context.Context) (domain.GitHubDeviceFlow, error) {
	if !s.app.Configured() {
		return domain.GitHubDeviceFlow{}, domain.ErrGitHubAppNotConfigured
	}
	code, err := s.gh.RequestDeviceCode(ctx, s.app.ClientID)
	if err != nil {
		return domain.GitHubDeviceFlow{}, err
	}
	interval := code.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	now := s.now()
	f := &flow{
		public: domain.GitHubDeviceFlow{
			ID:              uuid.NewString(),
			UserCode:        code.UserCode,
			VerificationURI: code.VerificationURI,
			ExpiresAt:       now.Add(code.ExpiresIn),
			IntervalSeconds: int(interval / time.Second),
			State:           domain.GitHubDevicePending,
		},
		clientID:   s.app.ClientID,
		deviceCode: code.DeviceCode,
		interval:   interval,
		nextPoll:   now.Add(interval),
	}
	s.flowsMu.Lock()
	s.pruneFlows(now)
	s.flows[f.public.ID] = f
	s.flowsMu.Unlock()
	return f.public, nil
}

var ErrDeviceFlowNotFound = errors.New("no such GitHub sign-in in progress")

// PollDeviceFlow checks once whether the user has authorized the code — never
// faster than GitHub's interval, however often the UI asks — and stores the
// connection the moment they have.
func (s *Service) PollDeviceFlow(ctx context.Context, id string) (domain.GitHubDeviceFlow, error) {
	s.flowsMu.Lock()
	defer s.flowsMu.Unlock()
	f, ok := s.flows[id]
	if !ok {
		return domain.GitHubDeviceFlow{}, ErrDeviceFlowNotFound
	}
	now := s.now()
	if f.public.State != domain.GitHubDevicePending || now.Before(f.nextPoll) {
		return f.public, nil
	}
	if now.After(f.public.ExpiresAt) {
		f.public.State = domain.GitHubDeviceExpired
		return f.public, nil
	}
	token, poll, err := s.gh.PollDeviceToken(ctx, f.clientID, f.deviceCode)
	if err != nil {
		f.public.Error = err.Error()
		f.nextPoll = now.Add(f.interval)
		return f.public, nil
	}
	if poll.Interval > 0 {
		f.interval = poll.Interval
		f.public.IntervalSeconds = int(poll.Interval / time.Second)
	}
	f.nextPoll = now.Add(f.interval)
	f.public.State, f.public.Error = poll.State, ""
	if poll.State != domain.GitHubDeviceAuthorized {
		return f.public, nil
	}
	login, err := s.gh.Login(ctx, token.AccessToken)
	if err != nil {
		f.public.State, f.public.Error = domain.GitHubDevicePending, err.Error()
		return f.public, nil
	}
	if err := s.apps.SetGitHubAppAuth(ctx, domain.GitHubAppAuth{Token: token, Login: login, ClientID: f.clientID}); err != nil {
		return f.public, err
	}
	if err := s.tokens.DeleteGitHubToken(ctx); err != nil {
		log.Warn().Err(err).Msg("github: removing the pasted token after the app connected failed")
	}
	f.public.Login = login
	return f.public, nil
}

func (s *Service) pruneFlows(now time.Time) {
	for id, f := range s.flows {
		if now.After(f.public.ExpiresAt.Add(flowRetention)) {
			delete(s.flows, id)
		}
	}
}
