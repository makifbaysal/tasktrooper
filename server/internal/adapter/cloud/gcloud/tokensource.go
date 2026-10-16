package gcloud

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// MintAccessToken exchanges a service account key for an access token of
// scope through httpClient: the JWT-bearer grant Client makes, for an adapter
// outside this package that calls a Google API on a connected account (the
// Cloud Monitoring data source). The key's token_uri is dialled through
// httpClient, so its transport decides whether that address may be reached.
func MintAccessToken(ctx context.Context, httpClient *http.Client, serviceAccountJSON, scope string) (string, time.Time, error) {
	sa, err := parseServiceAccountJSON(serviceAccountJSON)
	if err != nil {
		return "", time.Time{}, err
	}
	return fetchAccessToken(ctx, httpClient, sa.TokenURL, sa.ClientEmail, scope, sa.PrivateKey)
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
