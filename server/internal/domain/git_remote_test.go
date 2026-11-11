package domain

import "testing"

func TestIsNonGitHubRemote(t *testing.T) {
	cases := map[string]bool{
		"https://gitlab.com/acme/app.git":  true,
		"git@gitlab.com:acme/app.git":      true,
		"ssh://git@bitbucket.org/acme/app": true,
		"https://github.com/acme/app.git":  false,
		"git@github.com:acme/app.git":      false,
		"https://user:tok@github.com/a/b":  false,
		"":                                 false,
		"/srv/git/app.git":                 false,
	}
	for in, want := range cases {
		if got := IsNonGitHubRemote(in); got != want {
			t.Errorf("IsNonGitHubRemote(%q) = %v, want %v", in, got, want)
		}
	}
}
