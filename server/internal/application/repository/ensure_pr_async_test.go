package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ensurePRGit answers "a workspace with git exists" and hands back a fixed PR
// URL, so the test can drive ensurePullRequestAsync without a real checkout.
type ensurePRGit struct {
	fakeReleaseGit
	prURL string
}

func (g *ensurePRGit) HasGit(string) bool { return true }

func (g *ensurePRGit) EnsurePullRequest(context.Context, string) (string, error) {
	return g.prURL, nil
}

// ensurePRTaskStore records every SetTaskPullRequest call so the test can
// assert the PR still lands on the task row.
type ensurePRTaskStore struct {
	fakeReleaseTaskStore
	setCalls []string
}

func (s *ensurePRTaskStore) SetTaskPullRequest(_ context.Context, _ uuid.UUID, url string, _ int) error {
	s.setCalls = append(s.setCalls, url)
	return nil
}

// A system comment repeating the PR link used to be posted every time a task
// re-entered code_review, PM UAT or done — up to three identical messages per
// task, on top of the link already shown on the card. The task only records
// the PR now.
func TestEnsurePullRequestAsyncRecordsPRWithoutPostingAComment(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New()}
	git := &ensurePRGit{prURL: "https://github.com/acme/app/pull/7"}
	tasks := &ensurePRTaskStore{}
	comments := &fakeReleaseComments{}

	svc := &Service{
		git:           git,
		tasks:         tasks,
		comments:      comments,
		workspaceRoot: t.TempDir(),
		prAsyncRun:    func(fn func()) { fn() },
	}

	svc.ensurePullRequestAsync(context.Background(), task)

	if len(tasks.setCalls) != 1 || tasks.setCalls[0] != git.prURL {
		t.Fatalf("want the PR recorded on the task exactly once, got %+v", tasks.setCalls)
	}
	if len(comments.comments) != 0 {
		t.Fatalf("want no system comment posted for the PR link, got %+v", comments.comments)
	}
}

// Recording the PR no longer depends on a comment store, since nothing here
// writes a comment any more.
func TestEnsurePullRequestAsyncRecordsPRWithoutACommentStore(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New()}
	git := &ensurePRGit{prURL: "https://github.com/acme/app/pull/9"}
	tasks := &ensurePRTaskStore{}

	svc := &Service{
		git:           git,
		tasks:         tasks,
		workspaceRoot: t.TempDir(),
		prAsyncRun:    func(fn func()) { fn() },
	}

	svc.ensurePullRequestAsync(context.Background(), task)

	if len(tasks.setCalls) != 1 || tasks.setCalls[0] != git.prURL {
		t.Fatalf("want the PR recorded on the task even without a comment store, got %+v", tasks.setCalls)
	}
}
