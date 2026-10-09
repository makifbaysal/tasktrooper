package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func checkoutService(t *testing.T, git port.CheckoutGit) (*Service, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the verification fixtures are sh scripts")
	}
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, workspaceRel), 0o755))
	return NewService(Deps{WorkspaceRoot: root, Git: git}), filepath.Join(root, workspaceRel)
}

func runVerify(t *testing.T, svc *Service, req VerifyRequest) (*VerifyResult, *Failure, []Event) {
	t.Helper()
	prepared, failure := svc.PrepareVerify(context.Background(), "", req)
	require.Nil(t, failure)
	sink := &recordingSink{}
	result, failure := prepared.Execute(sink)
	return result, failure, sink.snapshot()
}

func TestVerifyStreamsEachStageAndItsOutputThenTheVerdict(t *testing.T) {
	svc, _ := checkoutService(t, nil)

	result, failure, events := runVerify(t, svc, VerifyRequest{
		Workspace: workspaceRel,
		Commands: []VerifyCommand{
			{Argv: []string{"sh", "-c", "echo compiled"}},
			{Argv: []string{"sh", "-c", "echo testing; echo 'FAIL: TestRefresh' >&2; exit 3"}},
		},
	})
	require.Nil(t, failure)

	assert.False(t, result.Passed)
	assert.Contains(t, result.Report, "$ sh -c echo testing")
	assert.Contains(t, result.Report, "FAIL: TestRefresh")
	assert.NotContains(t, result.Report, "compiled", "a passing stage is not part of the failure report")
	require.Len(t, result.Stages, 2)
	assert.Equal(t, "passed", result.Stages[0].Outcome)
	assert.Equal(t, "failed", result.Stages[1].Outcome)
	assert.Equal(t, 3, result.Stages[1].ExitCode)

	var kinds []string
	var output strings.Builder
	for _, ev := range events {
		switch e := ev.(type) {
		case *VerifyStageEvent:
			kinds = append(kinds, e.Kind+":"+e.Phase)
		case *VerifyOutputEvent:
			assert.Equal(t, EventVerifyOutput, e.Kind)
			output.WriteString(e.Data)
		}
	}
	assert.Equal(t, []string{
		"verify_stage:started", "verify_stage:finished",
		"verify_stage:started", "verify_stage:finished",
	}, kinds)
	assert.Contains(t, output.String(), "compiled")
	assert.Contains(t, output.String(), "FAIL: TestRefresh")
	for i, ev := range events {
		assert.Equal(t, int64(i+1), ev.header().Seq, "events are numbered in wire order")
	}
}

func TestVerifyPassesWithTheRunsEnvAndTheRepositorysOwnCommand(t *testing.T) {
	svc, dir := checkoutService(t, nil)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ci.sh"), []byte("test \"$TT_FLAVOUR\" = blue\n"), 0o755))

	result, failure, _ := runVerify(t, svc, VerifyRequest{
		Workspace:     workspaceRel,
		VerifyCommand: "sh ./ci.sh",
		Env:           map[string]string{"TT_FLAVOUR": "blue"},
	})
	require.Nil(t, failure)
	assert.True(t, result.Passed, result.Report)
	require.Len(t, result.Stages, 1)
	assert.Equal(t, "verify", result.Stages[0].Name)
}

func TestVerifyWithNothingDeclaredDetectsNothingAndPasses(t *testing.T) {
	svc, _ := checkoutService(t, nil)

	result, failure, events := runVerify(t, svc, VerifyRequest{Workspace: workspaceRel})
	require.Nil(t, failure)
	assert.True(t, result.Passed)
	assert.Empty(t, result.Stages)
	assert.Empty(t, events)
}

func TestVerifyReportsAMissingToolAsUnverified(t *testing.T) {
	svc, _ := checkoutService(t, nil)

	result, failure, _ := runVerify(t, svc, VerifyRequest{
		Workspace: workspaceRel,
		Commands:  []VerifyCommand{{Argv: []string{"tasktrooper-no-such-tool", "--check"}}},
	})
	require.Nil(t, failure)
	assert.True(t, result.Passed)
	assert.Contains(t, result.Report, "[unverified]")
	assert.Equal(t, "unverified", result.Stages[0].Outcome)
}

func TestVerifyIsCancelledByItsStreamID(t *testing.T) {
	svc, _ := checkoutService(t, nil)
	prepared, failure := svc.PrepareVerify(context.Background(), "v-1", VerifyRequest{
		Workspace: workspaceRel,
		Commands:  []VerifyCommand{{Argv: []string{"sh", "-c", "sleep 30"}}},
	})
	require.Nil(t, failure)

	done := make(chan *Failure, 1)
	go func() {
		_, f := prepared.Execute(&recordingSink{})
		done <- f
	}()
	require.Eventually(t, func() bool { return svc.CancelVerify("v-1") }, 5*time.Second, 20*time.Millisecond)
	select {
	case f := <-done:
		require.NotNil(t, f)
		assert.Equal(t, CodeCancelled, f.Code)
	case <-time.After(10 * time.Second):
		t.Fatal("a cancelled verification kept running")
	}
	assert.False(t, svc.CancelVerify("v-1"), "the id is free once the pass ended")
}

func TestVerifyRunsOnePassPerCheckout(t *testing.T) {
	svc, _ := checkoutService(t, nil)
	first, failure := svc.PrepareVerify(context.Background(), "v-1", VerifyRequest{Workspace: workspaceRel})
	require.Nil(t, failure)
	defer first.Release()

	_, failure = svc.PrepareVerify(context.Background(), "v-2", VerifyRequest{Workspace: workspaceRel})
	require.NotNil(t, failure)
	assert.Equal(t, CodeConflict, failure.Code)
}

func TestVerifyRefusals(t *testing.T) {
	svc, _ := checkoutService(t, nil)
	tests := map[string]VerifyRequest{
		"no workspace":          {},
		"a workspace elsewhere": {Workspace: "../outside"},
		"a missing workspace":   {Workspace: "repos/demo/task-404"},
		"an empty command":      {Workspace: workspaceRel, Commands: []VerifyCommand{{}}},
		"a dir outside":         {Workspace: workspaceRel, Commands: []VerifyCommand{{Dir: "../..", Argv: []string{"ls"}}}},
		"PATH in env":           {Workspace: workspaceRel, Env: map[string]string{"PATH": "/elsewhere"}},
		"a negative timeout":    {Workspace: workspaceRel, TimeoutMS: -1},
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			_, failure := svc.PrepareVerify(context.Background(), "", req)
			require.NotNil(t, failure)
			assert.Equal(t, CodeBadRequest, failure.Code)
		})
	}
}

type fakeCheckoutGit struct {
	dirs   []string
	push   port.CheckoutCommitPush
	err    error
	result port.CheckoutPushResult
}

func (f *fakeCheckoutGit) Status(_ context.Context, dir string) (port.CheckoutStatus, error) {
	f.dirs = append(f.dirs, dir)
	return port.CheckoutStatus{Branch: "feature/tt-1", Clean: true, Files: []port.CheckoutFileStatus{}}, f.err
}

func (f *fakeCheckoutGit) Diff(_ context.Context, dir string, q port.CheckoutDiffQuery) (port.CheckoutDiff, error) {
	f.dirs = append(f.dirs, dir)
	return port.CheckoutDiff{Base: "abc", Files: []string{"a.go"}, Patch: "+x"}, f.err
}

func (f *fakeCheckoutGit) Log(_ context.Context, dir string, _ port.CheckoutLogQuery) ([]port.CheckoutCommit, error) {
	f.dirs = append(f.dirs, dir)
	return []port.CheckoutCommit{{SHA: "abc", Subject: "s"}}, f.err
}

func (f *fakeCheckoutGit) CommitPush(_ context.Context, dir string, req port.CheckoutCommitPush) (port.CheckoutPushResult, error) {
	f.dirs = append(f.dirs, dir)
	f.push = req
	return f.result, f.err
}

func TestGitRoutesResolveTheWorkspaceUnderTheRoot(t *testing.T) {
	git := &fakeCheckoutGit{result: port.CheckoutPushResult{Branch: "feature/tt-1", Committed: true}}
	svc, dir := checkoutService(t, git)

	status, failure := svc.GitStatus(context.Background(), GitStatusRequest{Workspace: workspaceRel})
	require.Nil(t, failure)
	assert.Equal(t, "feature/tt-1", status.Branch)
	diff, failure := svc.GitDiff(context.Background(), GitDiffRequest{Workspace: workspaceRel})
	require.Nil(t, failure)
	assert.Equal(t, []string{"a.go"}, diff.Files)
	commits, failure := svc.GitLog(context.Background(), GitLogRequest{Workspace: workspaceRel})
	require.Nil(t, failure)
	assert.Len(t, commits.Commits, 1)
	pushed, failure := svc.CommitPush(context.Background(), CommitPushRequest{
		Workspace: workspaceRel, Message: "feat: x", Branch: "feature/tt-1", GitHubToken: "ghs_token",
	})
	require.Nil(t, failure)
	assert.True(t, pushed.Committed)
	assert.Equal(t, port.CheckoutCommitPush{Message: "feat: x", Branch: "feature/tt-1", Token: "ghs_token"}, git.push)
	for _, d := range git.dirs {
		assert.Equal(t, dir, d)
	}
}

func TestGitFailuresKeepTheirMeaning(t *testing.T) {
	tests := []struct {
		err    error
		code   string
		reason string
	}{
		{err: fmt.Errorf("x: %w", port.ErrCheckoutConflict), code: CodeConflict},
		{err: fmt.Errorf("x: %w", port.ErrCheckoutInvalid), code: CodeBadRequest},
		{err: fmt.Errorf("x: %w", port.ErrCheckoutNotRepository), code: CodeBadRequest},
		{err: fmt.Errorf("git push: %w", domain.ErrGitHubWorkflowScope), code: CodeUpstream, reason: ReasonWorkflowScope},
		{err: errors.New("git push: rejected"), code: CodeUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			svc, _ := checkoutService(t, &fakeCheckoutGit{err: tt.err})
			_, failure := svc.CommitPush(context.Background(), CommitPushRequest{Workspace: workspaceRel, Message: "m", Branch: "b"})
			require.NotNil(t, failure)
			assert.Equal(t, tt.code, failure.Code)
			assert.Equal(t, tt.reason, failure.Reason)
		})
	}
}

func TestGitRoutesWithoutGitAreNotReady(t *testing.T) {
	svc, _ := checkoutService(t, nil)
	_, failure := svc.GitStatus(context.Background(), GitStatusRequest{Workspace: workspaceRel})
	require.NotNil(t, failure)
	assert.Equal(t, CodeNotReady, failure.Code)
}
