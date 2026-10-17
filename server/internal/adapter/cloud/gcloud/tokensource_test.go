package gcloud_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/gcloud"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func answering(asked *[]string, status int, body string) *http.Client {
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		*asked = append(*asked, r.URL.String())
		return &http.Response{
			StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: r,
		}, nil
	})}
}

func TestMintAccessTokenAsksOnlyGooglesTokenEndpoint(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		ok   bool
	}{
		{"none named", "", true},
		{"oauth2", "https://oauth2.googleapis.com/token", true},
		{"legacy accounts host", "https://accounts.google.com/o/oauth2/token", true},
		{"legacy v4 endpoint", "https://www.googleapis.com/oauth2/v4/token", true},
		{"another googleapis host", "https://storage.googleapis.com/token", false},
		{"another path on the token host", "https://oauth2.googleapis.com/upload/token", false},
		{"a query", "https://oauth2.googleapis.com/token?x=1", false},
		{"plain http", "http://oauth2.googleapis.com/token", false},
		{"another host", "https://tokens.example.com/token", false},
		{"lookalike host", "https://oauth2.googleapis.com.example.com/token", false},
		{"an address", "https://127.0.0.1/token", false},
		{"another port", "https://oauth2.googleapis.com:8443/token", false},
		{"credentials in the URL", "https://u@oauth2.googleapis.com/token", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked []string
			client := answering(&asked, 200, `{"access_token":"ya29.minted","expires_in":3600}`)

			tok, _, err := gcloud.MintAccessToken(context.Background(), client, serviceAccountJSON(t, tt.uri), "scope")

			if !tt.ok {
				require.ErrorIs(t, err, gcloud.ErrTokenURINotGoogle)
				assert.Empty(t, asked)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "ya29.minted", tok)
			assert.Equal(t, []string{"https://oauth2.googleapis.com/token"}, asked, "a legacy URL is asked at the endpoint it moved to")
		})
	}
}

func TestMintAccessTokenNeverFollowsARedirect(t *testing.T) {
	var asked []string
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		asked = append(asked, r.URL.String())
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"https://collector.example.com/token"}},
			Body: io.NopCloser(strings.NewReader("")), Request: r,
		}, nil
	})}

	_, _, err := gcloud.MintAccessToken(context.Background(), client, serviceAccountJSON(t, ""), "scope")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 307")
	assert.Equal(t, []string{"https://oauth2.googleapis.com/token"}, asked, "the signed assertion goes nowhere else")
	assert.Nil(t, client.CheckRedirect, "the caller's client is left as it was")
}

func TestMintAccessTokenKeepsATokenAnHourAtMost(t *testing.T) {
	var asked []string
	client := answering(&asked, 200, `{"access_token":"ya29.minted","expires_in":864000}`)

	_, exp, err := gcloud.MintAccessToken(context.Background(), client, serviceAccountJSON(t, ""), "scope")

	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), exp, time.Minute)
}

func TestMintAccessTokenNeverReturnsTheEndpointsOwnText(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"an OAuth refusal", 400, `{"error":"invalid_grant","error_description":"Invalid JWT Signature for ops@acme-secret.iam"}`, "(HTTP 400, invalid_grant)"},
		{"an unknown error code", 400, `{"error":"see acme-secret","error_description":"x"}`, "(HTTP 400)"},
		{"not JSON", 502, `<html>upstream acme-secret</html>`, "(HTTP 502)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked []string
			_, _, err := gcloud.MintAccessToken(context.Background(), answering(&asked, tt.status, tt.body),
				serviceAccountJSON(t, "https://oauth2.googleapis.com/token"), "scope")

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.NotContains(t, err.Error(), "acme-secret")
			assert.NotContains(t, err.Error(), "JWT")
		})
	}
}
