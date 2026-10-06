package domain

import "testing"

func TestGitHubWorkflowScopeRefusalMatchesGitHubsWording(t *testing.T) {
	out := " ! [remote rejected] HEAD -> feature/t-87 (refusing to allow a Personal Access Token to create or update workflow `.github/workflows/ci.yml` without `workflow` scope)"
	if !GitHubWorkflowScopeRefusal(out) {
		t.Fatal("GitHub's refusal must be recognised")
	}
	if GitHubWorkflowScopeRefusal("! [rejected] HEAD -> main (non-fast-forward)") {
		t.Fatal("an ordinary rejection is not a scope refusal")
	}
}

func TestCIConfigOnly(t *testing.T) {
	for _, tc := range []struct {
		paths []string
		want  bool
	}{
		{[]string{".github/workflows/ci.yml"}, true},
		{[]string{".github/workflows/ci.yml", ".github/actions/setup/action.yml", ".gitlab-ci.yml"}, true},
		{[]string{".github/workflows/ci.yml", "package.json"}, false},
		{[]string{".github/CODEOWNERS"}, false},
		{nil, false},
	} {
		if got := CIConfigOnly(tc.paths); got != tc.want {
			t.Errorf("CIConfigOnly(%v) = %v, want %v", tc.paths, got, tc.want)
		}
	}
}
