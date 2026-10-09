package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGitEnvForAddsTheGitHubHeaderOnlyForGitHub(t *testing.T) {
	env := gitEnvFor(prepareParams{RepoURL: "https://github.com/acme/a.git", GitHubToken: "ghs_secret"})
	want := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:ghs_secret"))
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GIT_CONFIG_KEY_") || !strings.Contains(joined, "=http.https://github.com/.extraheader") {
		t.Fatalf("extraheader key missing: %v", env)
	}
	if !strings.Contains(joined, "="+want) {
		t.Fatalf("extraheader value missing")
	}
	if !strings.Contains(joined, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("the no-prompt base env must stay")
	}

	for _, p := range []prepareParams{
		{RepoURL: "https://gitlab.com/acme/a.git", GitHubToken: "ghs_secret"},
		{RepoURL: "git@github.com:acme/a.git", GitHubToken: "ghs_secret"},
		{RepoURL: "https://github.com/acme/a.git"},
	} {
		if strings.Contains(strings.Join(gitEnvFor(p), "\n"), "extraheader") {
			t.Fatalf("no header expected for %+v", p)
		}
	}
}
