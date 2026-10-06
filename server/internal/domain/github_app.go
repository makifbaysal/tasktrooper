package domain

import (
	"errors"
	"time"
)

// ErrGitHubConnectionExpired: the GitHub App connection's refresh token is no
// longer valid (unused for six months, or revoked); a human has to connect
// again.
var ErrGitHubConnectionExpired = errors.New("the GitHub connection has expired — connect GitHub again")

// ErrGitHubAppNotConfigured: the service was built without a GitHub App, so
// the device flow cannot start; the token path still works.
var ErrGitHubAppNotConfigured = errors.New("no GitHub App is configured for this build")

// GitHubAppConfig names the GitHub App TaskTrooper connects through. Neither
// field is a secret: the device flow needs no client secret.
type GitHubAppConfig struct {
	ClientID string `json:"client_id"`
	// Slug builds the app's install link (github.com/apps/<slug>/installations/new).
	Slug string `json:"slug,omitempty"`
}

func (c GitHubAppConfig) Configured() bool { return c.ClientID != "" }

func (c GitHubAppConfig) InstallURL() string {
	if c.Slug == "" {
		return ""
	}
	return "https://github.com/apps/" + c.Slug + "/installations/new"
}

// GitHubUserToken is a GitHub App user access token pair. A zero ExpiresAt
// means the app opted out of token expiration.
type GitHubUserToken struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token,omitempty"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at,omitempty"`
}

// Fresh reports a token still good for at least margin.
func (t GitHubUserToken) Fresh(now time.Time, margin time.Duration) bool {
	return t.AccessToken != "" && (t.ExpiresAt.IsZero() || now.Add(margin).Before(t.ExpiresAt))
}

// GitHubAppAuth is the stored connection: the token pair, who it acts as and
// which app issued it (a later client id change must not refresh it).
type GitHubAppAuth struct {
	Token    GitHubUserToken `json:"token"`
	Login    string          `json:"login"`
	ClientID string          `json:"client_id"`
}

// GitHubDeviceCode is the device flow's first answer.
type GitHubDeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresIn       time.Duration
	Interval        time.Duration
}

type GitHubDeviceState string

const (
	GitHubDevicePending    GitHubDeviceState = "pending"
	GitHubDeviceAuthorized GitHubDeviceState = "authorized"
	GitHubDeviceExpired    GitHubDeviceState = "expired"
	GitHubDeviceDenied     GitHubDeviceState = "denied"
)

// GitHubDevicePoll is one poll's answer; Interval is set when GitHub asked to
// slow down.
type GitHubDevicePoll struct {
	State    GitHubDeviceState
	Interval time.Duration
}

// GitHubDeviceFlow is what the UI shows while the user authorizes: the code
// to type and where. The device code itself stays on the server.
type GitHubDeviceFlow struct {
	ID              string            `json:"id"`
	UserCode        string            `json:"user_code"`
	VerificationURI string            `json:"verification_uri"`
	ExpiresAt       time.Time         `json:"expires_at"`
	IntervalSeconds int               `json:"interval_seconds"`
	State           GitHubDeviceState `json:"state"`
	Login           string            `json:"login,omitempty"`
	Error           string            `json:"error,omitempty"`
}

// GitHubConnectionMode is how the stored GitHub credential was obtained.
type GitHubConnectionMode string

const (
	GitHubModeNone  GitHubConnectionMode = ""
	GitHubModeApp   GitHubConnectionMode = "app"
	GitHubModeToken GitHubConnectionMode = "token"
)
