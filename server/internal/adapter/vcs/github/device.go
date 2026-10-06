package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// GitHub user access tokens through the device flow — an OAuth App's (no
// expiry, scopes requested here) or a GitHub App's (eight hours, renewed) —
// per
// https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app
// and …/refreshing-user-access-tokens (checked 2026-10-06):
//   - POST https://github.com/login/device/code?client_id=… answers
//     device_code, user_code, verification_uri, expires_in, interval;
//   - POST https://github.com/login/oauth/access_token with client_id,
//     device_code and grant_type=urn:ietf:params:oauth:grant-type:device_code
//     answers the token, or error=authorization_pending|slow_down|
//     expired_token|access_denied|device_flow_disabled;
//   - a device-flow token refreshes with client_id, grant_type=refresh_token
//     and refresh_token alone — no client secret, which is what lets a desktop
//     app that holds none keep the connection alive.
var loginBase = "https://github.com"

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// deviceScopes are what an OAuth App's token is granted; a GitHub App ignores
// them and grants its own fine-grained permissions instead. workflow is here
// because without it GitHub refuses every push that touches
// .github/workflows.
const deviceScopes = "repo workflow admin:repo_hook read:org"

// DeviceClient is the github.com login half TaskTrooper needs for a GitHub
// App connection.
type DeviceClient struct{}

func (DeviceClient) RequestDeviceCode(ctx context.Context, clientID string) (domain.GitHubDeviceCode, error) {
	var out struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
		ErrorDesc       string `json:"error_description"`
	}
	if err := postLoginForm(ctx, "/login/device/code", url.Values{"client_id": {clientID}, "scope": {deviceScopes}}, &out); err != nil {
		return domain.GitHubDeviceCode{}, err
	}
	if out.Error != "" {
		return domain.GitHubDeviceCode{}, fmt.Errorf("github device flow: %s: %s", out.Error, out.ErrorDesc)
	}
	return domain.GitHubDeviceCode{
		DeviceCode:      out.DeviceCode,
		UserCode:        out.UserCode,
		VerificationURI: out.VerificationURI,
		ExpiresIn:       time.Duration(out.ExpiresIn) * time.Second,
		Interval:        time.Duration(out.Interval) * time.Second,
	}, nil
}

type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
	ErrorDesc             string `json:"error_description"`
	Interval              int    `json:"interval"`
}

func (r tokenResponse) token(now time.Time) domain.GitHubUserToken {
	t := domain.GitHubUserToken{AccessToken: r.AccessToken, RefreshToken: r.RefreshToken}
	// An app that opted out of token expiration answers no expires_in: the
	// token lives until revoked.
	if r.ExpiresIn > 0 {
		t.ExpiresAt = now.Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	if r.RefreshTokenExpiresIn > 0 {
		t.RefreshExpiresAt = now.Add(time.Duration(r.RefreshTokenExpiresIn) * time.Second)
	}
	return t
}

// PollDeviceToken asks once whether the user has authorized deviceCode. A
// still-waiting answer is a status, not an error; slow_down carries the
// longer interval GitHub now wants.
func (DeviceClient) PollDeviceToken(ctx context.Context, clientID, deviceCode string) (domain.GitHubUserToken, domain.GitHubDevicePoll, error) {
	var out tokenResponse
	err := postLoginForm(ctx, "/login/oauth/access_token", url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {deviceGrantType},
	}, &out)
	if err != nil {
		return domain.GitHubUserToken{}, domain.GitHubDevicePoll{}, err
	}
	switch out.Error {
	case "":
		if out.AccessToken == "" {
			return domain.GitHubUserToken{}, domain.GitHubDevicePoll{}, fmt.Errorf("github device flow: no token in the response")
		}
		return out.token(time.Now()), domain.GitHubDevicePoll{State: domain.GitHubDeviceAuthorized}, nil
	case "authorization_pending":
		return domain.GitHubUserToken{}, domain.GitHubDevicePoll{State: domain.GitHubDevicePending}, nil
	case "slow_down":
		return domain.GitHubUserToken{}, domain.GitHubDevicePoll{
			State: domain.GitHubDevicePending, Interval: time.Duration(out.Interval) * time.Second,
		}, nil
	case "expired_token":
		return domain.GitHubUserToken{}, domain.GitHubDevicePoll{State: domain.GitHubDeviceExpired}, nil
	case "access_denied":
		return domain.GitHubUserToken{}, domain.GitHubDevicePoll{State: domain.GitHubDeviceDenied}, nil
	default:
		return domain.GitHubUserToken{}, domain.GitHubDevicePoll{}, fmt.Errorf("github device flow: %s: %s", out.Error, out.ErrorDesc)
	}
}

func (DeviceClient) RefreshUserToken(ctx context.Context, clientID, refreshToken string) (domain.GitHubUserToken, error) {
	var out tokenResponse
	if err := postLoginForm(ctx, "/login/oauth/access_token", url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}, &out); err != nil {
		return domain.GitHubUserToken{}, err
	}
	if out.Error != "" {
		if out.Error == "bad_refresh_token" {
			return domain.GitHubUserToken{}, fmt.Errorf("%w: %s", domain.ErrGitHubConnectionExpired, out.ErrorDesc)
		}
		return domain.GitHubUserToken{}, fmt.Errorf("github token refresh: %s: %s", out.Error, out.ErrorDesc)
	}
	return out.token(time.Now()), nil
}

func (DeviceClient) Login(ctx context.Context, token string) (string, error) {
	return User(ctx, token)
}

// Installations counts the app installations the user's token can reach —
// zero means the app was authorized but installed on no account yet, so the
// token reaches no repository.
func (DeviceClient) Installations(ctx context.Context, token string) (int, error) {
	var out struct {
		TotalCount int `json:"total_count"`
	}
	if err := doJSON(ctx, token, http.MethodGet, "/user/installations?per_page=1", nil, &out); err != nil {
		return 0, err
	}
	return out.TotalCount, nil
}

func postLoginForm(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginBase+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{Status: resp.StatusCode, Message: strings.TrimSpace(string(data))}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("github login: unexpected response: %w", err)
	}
	return nil
}
