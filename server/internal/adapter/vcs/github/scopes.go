package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// TokenScopes reads a token's scopes from GitHub's X-OAuth-Scopes header.
// classic is false for a fine-grained token, which carries permissions
// instead of scopes and answers no such header — its permissions cannot be
// read back, only exercised.
func TokenScopes(ctx context.Context, token string) (scopes []string, classic bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolveBase("")+"/user", nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, &apiError{Status: resp.StatusCode, Message: fmt.Sprintf("reading token scopes: %s", resp.Status)}
	}
	header, present := resp.Header[http.CanonicalHeaderKey("X-OAuth-Scopes")]
	if !present {
		return nil, false, nil
	}
	for _, s := range strings.Split(strings.Join(header, ","), ",") {
		if s = strings.TrimSpace(s); s != "" {
			scopes = append(scopes, s)
		}
	}
	return scopes, true, nil
}

// MissingScopes is the subset of want a classic token's scopes lack.
func MissingScopes(have, want []string) []string {
	set := make(map[string]bool, len(have))
	for _, s := range have {
		set[s] = true
	}
	var out []string
	for _, w := range want {
		if !set[w] {
			out = append(out, w)
		}
	}
	return out
}
