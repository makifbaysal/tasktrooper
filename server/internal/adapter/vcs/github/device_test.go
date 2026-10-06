package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func withLoginBase(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	prev := loginBase
	loginBase = srv.URL
	t.Cleanup(func() { loginBase = prev })
}

func TestRequestDeviceCodeSendsOnlyTheClientID(t *testing.T) {
	withLoginBase(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "/login/device/code", r.URL.Path)
		assert.Equal(t, "Iv23x", r.PostForm.Get("client_id"))
		assert.Empty(t, r.PostForm.Get("client_secret"))
		assert.Contains(t, r.PostForm.Get("scope"), "workflow", "an OAuth App's token must be able to push CI files")
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"WDJB-MJHT","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`))
	})

	code, err := DeviceClient{}.RequestDeviceCode(context.Background(), "Iv23x")

	require.NoError(t, err)
	assert.Equal(t, "WDJB-MJHT", code.UserCode)
	assert.Equal(t, 15*time.Minute, code.ExpiresIn)
	assert.Equal(t, 5*time.Second, code.Interval)
}

func TestPollDeviceTokenReadsEveryAnswer(t *testing.T) {
	answers := map[string]domain.GitHubDeviceState{
		`{"error":"authorization_pending"}`: domain.GitHubDevicePending,
		`{"error":"expired_token"}`:         domain.GitHubDeviceExpired,
		`{"error":"access_denied"}`:         domain.GitHubDeviceDenied,
		`{"access_token":"ghu_x","expires_in":28800,"refresh_token":"ghr_x","refresh_token_expires_in":15897600,"token_type":"bearer"}`: domain.GitHubDeviceAuthorized,
	}
	for body, want := range answers {
		withLoginBase(t, func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.PostForm.Get("grant_type"))
			_, _ = w.Write([]byte(body))
		})
		tok, poll, err := DeviceClient{}.PollDeviceToken(context.Background(), "Iv23x", "dc")
		require.NoError(t, err, body)
		assert.Equal(t, want, poll.State, body)
		if want == domain.GitHubDeviceAuthorized {
			assert.Equal(t, "ghu_x", tok.AccessToken)
			assert.Equal(t, "ghr_x", tok.RefreshToken)
			assert.False(t, tok.ExpiresAt.IsZero())
		}
	}
}

func TestPollDeviceTokenCarriesTheSlowerInterval(t *testing.T) {
	withLoginBase(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":"slow_down","interval":10}`))
	})

	_, poll, err := DeviceClient{}.PollDeviceToken(context.Background(), "Iv23x", "dc")

	require.NoError(t, err)
	assert.Equal(t, domain.GitHubDevicePending, poll.State)
	assert.Equal(t, 10*time.Second, poll.Interval)
}

func TestRefreshUserTokenNeedsNoSecretAndReadsABadRefreshAsExpired(t *testing.T) {
	withLoginBase(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "refresh_token", r.PostForm.Get("grant_type"))
		assert.Empty(t, r.PostForm.Get("client_secret"))
		_, _ = w.Write([]byte(`{"error":"bad_refresh_token","error_description":"The refresh token passed is incorrect or expired."}`))
	})

	_, err := DeviceClient{}.RefreshUserToken(context.Background(), "Iv23x", "ghr_old")

	assert.ErrorIs(t, err, domain.ErrGitHubConnectionExpired)
}

func TestATokenWithoutExpiryNeverAsksToRefresh(t *testing.T) {
	tok := tokenResponse{AccessToken: "ghu_forever"}.token(time.Now())

	assert.True(t, tok.ExpiresAt.IsZero())
	assert.True(t, tok.Fresh(time.Now().Add(365*24*time.Hour), time.Hour))
}
