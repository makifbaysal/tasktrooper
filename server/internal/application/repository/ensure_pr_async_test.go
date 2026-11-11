package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ensurePRGit struct {
	fakeReleaseGit
	prURL string
}

func (g *ensurePRGit) HasGit(string) bool { return true }

func (g *ensurePRGit) EnsurePullRequest(context.Context, string) (string, error) {
	return g.prURL, nil
}

type ensurePRTaskStore struct {
	fakeReleaseTaskStore
	setCalls []string
}

func (s *ensurePRTaskStore) SetTaskPullRequest(_ context.Context, _ uuid.UUID, url string, _ int) error {
	s.setCalls = append(s.setCalls, url)
	return nil
}

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

func TestEnsurePullRequestAsyncSkipsWhileOneIsAlreadyRunningForTheTask(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New()}
	git := &ensurePRGit{prURL: "https://github.com/acme/app/pull/7"}
	tasks := &ensurePRTaskStore{}
	var queued []func()

	svc := &Service{
		git:           git,
		tasks:         tasks,
		workspaceRoot: t.TempDir(),
		prAsyncRun:    func(fn func()) { queued = append(queued, fn) },
	}

	svc.ensurePullRequestAsync(context.Background(), task)
	svc.ensurePullRequestAsync(context.Background(), task)
	if len(queued) != 1 {
		t.Fatalf("a second trigger while the first is pending must not start another PR attempt, got %d", len(queued))
	}
	queued[0]()
	svc.ensurePullRequestAsync(context.Background(), task)
	if len(queued) != 2 {
		t.Fatalf("after the first finished a later trigger must run again, got %d", len(queued))
	}
}
