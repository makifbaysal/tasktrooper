package gcloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// ErrTokenURINotGoogle is a key file whose token_uri is not Google's own
// token endpoint.
var ErrTokenURINotGoogle = errors.New("gcloud: the key's token_uri is not Google's token endpoint")

// oauthErrorCodes are RFC 6749's token error codes: a fixed vocabulary, so
// one may be shown where the endpoint's own text may not.
var oauthErrorCodes = map[string]bool{
	"invalid_request": true, "invalid_client": true, "invalid_grant": true,
	"unauthorized_client": true, "unsupported_grant_type": true, "invalid_scope": true,
}

// MintAccessToken exchanges a service account key for an access token of
// scope through httpClient: the JWT-bearer grant Client makes, for an adapter
// outside this package that calls a Google API on a connected account (the
// Cloud Monitoring data source). The key's token_uri is uploaded with the key,
// so it must name Google's token endpoint, https://oauth2.googleapis.com/token,
// or one of the legacy URLs older keys carry (accounts.google.com/o/oauth2/token,
// www.googleapis.com/oauth2/v4/token), which are asked at that endpoint;
// anything else is ErrTokenURINotGoogle, refused before any request. It is
// dialled through httpClient, whose transport still decides whether that
// address may be reached, and a redirect is never followed: it would carry the
// signed assertion elsewhere. A refusal names the HTTP status and the OAuth
// error code only; the endpoint's own text is logged, never returned, since it
// ends up in a data source's status_detail.
func MintAccessToken(ctx context.Context, httpClient *http.Client, serviceAccountJSON, scope string) (string, time.Time, error) {
	sa, err := parseServiceAccountJSON(serviceAccountJSON)
	if err != nil {
		return "", time.Time{}, err
	}
	tokenURL, ok := googleTokenURL(sa.TokenURL)
	if !ok {
		return "", time.Time{}, ErrTokenURINotGoogle
	}
	client := *httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	tok, exp, err := fetchAccessToken(ctx, &client, tokenURL, sa.ClientEmail, scope, sa.PrivateKey)
	var ae *apiError
	if errors.As(err, &ae) {
		log.Warn().Int("status", ae.Status).Str("body", ae.Body).Msg("gcloud: the token endpoint refused a service account key")
		return "", time.Time{}, tokenRefusal(ae)
	}
	return tok, exp, err
}

// googleTokenURL answers the endpoint a key's token_uri is asked at.
func googleTokenURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" ||
		(u.Port() != "" && u.Port() != "443") {
		return "", false
	}
	switch strings.ToLower(u.Hostname()) + u.EscapedPath() {
	case "oauth2.googleapis.com/token", "accounts.google.com/o/oauth2/token", "www.googleapis.com/oauth2/v4/token":
		return defaultTokenURL, true
	}
	return "", false
}

func tokenRefusal(ae *apiError) error {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(ae.Body), &body) == nil && oauthErrorCodes[body.Error] {
		return fmt.Errorf("gcloud: the token endpoint refused the key (HTTP %d, %s)", ae.Status, body.Error)
	}
	return fmt.Errorf("gcloud: the token endpoint refused the key (HTTP %d)", ae.Status)
}

// ServiceAccountProjectID is the project a key file names; "" when it names
// none or does not parse.
func ServiceAccountProjectID(serviceAccountJSON string) string {
	var sa serviceAccount
	if json.Unmarshal([]byte(serviceAccountJSON), &sa) != nil {
		return ""
	}
	return sa.ProjectID
}
