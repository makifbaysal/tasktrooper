package git

import (
	"errors"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestPushErrorRecognisesTheWorkflowScopeRefusal(t *testing.T) {
	out := "To https://github.com/o/r.git\n ! [remote rejected] HEAD -> feature/t-87 (refusing to allow a Personal Access Token to create or update workflow `.github/workflows/ci.yml` without `workflow` scope)\nerror: failed to push some refs"
	err := pushError(errors.New("exit status 1"), out)
	if !errors.Is(err, domain.ErrGitHubWorkflowScope) {
		t.Fatalf("want ErrGitHubWorkflowScope, got %v", err)
	}

	other := pushError(errors.New("exit status 1"), "! [rejected] HEAD -> main (fetch first)")
	if errors.Is(other, domain.ErrGitHubWorkflowScope) {
		t.Fatal("an ordinary rejection must not read as a scope refusal")
	}
}
