package domain

import (
	"errors"
	"strings"
)

// ErrGitHubWorkflowScope is GitHub refusing a push that creates or changes a
// file under .github/workflows: the token lacks the `workflow` scope (classic
// token) or the Workflows write permission (fine-grained token). No retry
// helps until a human saves a token that has it.
var ErrGitHubWorkflowScope = errors.New("github refused the push: the token lacks the workflow scope needed to change .github/workflows")

// GitHubWorkflowScopeRefusal recognises GitHub's own wording for that refusal
// in git push output ("... to create or update workflow `<path>` without
// `workflow` scope").
func GitHubWorkflowScopeRefusal(output string) bool {
	return strings.Contains(output, "without `workflow` scope")
}

// GitHubWorkflowScope is the classic-token scope that allows changing workflow files.
const GitHubWorkflowScope = "workflow"

// CIConfigOnly reports whether every changed path is CI configuration — a
// change whose real check is the pipeline run on its pull request, not a
// command the agent can run locally.
func CIConfigOnly(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if !isCIConfigPath(p) {
			return false
		}
	}
	return true
}

func isCIConfigPath(p string) bool {
	p = strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "./"))
	switch {
	case strings.HasPrefix(p, ".github/workflows/"), strings.HasPrefix(p, ".github/actions/"),
		strings.HasPrefix(p, ".circleci/"), strings.HasPrefix(p, ".buildkite/"):
		return true
	}
	switch p {
	case ".gitlab-ci.yml", "azure-pipelines.yml", "bitbucket-pipelines.yml", "jenkinsfile",
		".github/dependabot.yml", ".github/dependabot.yaml":
		return true
	}
	return false
}
