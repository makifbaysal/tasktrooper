package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeReleaseService scripts kit.Releases for the release tools' tests.
// ForAgent (not ForTask) is what every release tool resolves through — see
// resolveRelease — so it is the one that records what it was asked; every
// other call is addressed by the release id the tool already holds, so the
// fakes for those just return what the test scripted and record what they
// were asked.
type fakeReleaseService struct {
	release       domain.Release
	forTaskErr    error
	gotTaskID     uuid.UUID
	gotRepoID     uuid.UUID
	forAgentCalls int

	deployErr error
	deployRel domain.Release

	watchBlock *domain.ResourceBlock
	watchErr   error
	watchRel   domain.Release

	smokeResults []domain.SmokeResult
	smokeErr     error

	finishErr  error
	finishRel  domain.Release
	gotNote    string
	finishCall int

	rollbackErr  error
	rollbackRel  domain.Release
	gotReason    domain.RollbackReason
	rollbackCall int
	gotReleaseID uuid.UUID
	gotActor     domain.ReleaseActor
}

func (f *fakeReleaseService) ForTask(_ context.Context, repositoryID, taskID uuid.UUID) (domain.Release, error) {
	f.gotRepoID, f.gotTaskID = repositoryID, taskID
	return f.release, f.forTaskErr
}

func (f *fakeReleaseService) ForAgent(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.Release, error) {
	f.forAgentCalls++
	return f.ForTask(ctx, repositoryID, taskID)
}

func (f *fakeReleaseService) Get(context.Context, uuid.UUID) (domain.Release, error) {
	return f.release, nil
}

func (f *fakeReleaseService) Deploy(_ context.Context, releaseID uuid.UUID, actor domain.ReleaseActor) (domain.Release, error) {
	f.gotReleaseID, f.gotActor = releaseID, actor
	return f.deployRel, f.deployErr
}

func (f *fakeReleaseService) Watch(_ context.Context, releaseID uuid.UUID) (domain.Release, *domain.ResourceBlock, error) {
	f.gotReleaseID = releaseID
	return f.watchRel, f.watchBlock, f.watchErr
}

func (f *fakeReleaseService) RunSmoke(_ context.Context, releaseID uuid.UUID) ([]domain.SmokeResult, error) {
	f.gotReleaseID = releaseID
	return f.smokeResults, f.smokeErr
}

func (f *fakeReleaseService) Finish(_ context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, note string) (domain.Release, error) {
	f.finishCall++
	f.gotReleaseID, f.gotActor, f.gotNote = releaseID, actor, note
	return f.finishRel, f.finishErr
}

func (f *fakeReleaseService) Rollback(_ context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, reason domain.RollbackReason, note string) (domain.Release, error) {
	f.rollbackCall++
	f.gotReleaseID, f.gotActor, f.gotReason, f.gotNote = releaseID, actor, reason, note
	return f.rollbackRel, f.rollbackErr
}

// releaseToolKit builds a kit bound to a task in repoID, with the fake
// release service wired.
func releaseToolKit(repoID uuid.UUID, releases *fakeReleaseService) *ToolKit {
	return &ToolKit{Tasks: &fakeTaskManager{taskRepoID: repoID}, Releases: releases}
}

func releaseCtx(repoID, taskID uuid.UUID) context.Context {
	return registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
}

// -------------------------------------------------------------- not configured

func TestReleaseToolsRefuseWhenNotConfigured(t *testing.T) {
	kit := &ToolKit{Tasks: &fakeTaskManager{}}
	tools := []port.ToolExecutor{
		newGetReleaseTool(kit),
		newDeployReleaseTool(kit),
		newWatchReleaseTool(kit),
		newRunSmokeChecksTool(kit),
		newFinishReleaseTool(kit),
		newRollbackReleaseTool(kit),
	}
	for _, tool := range tools {
		res := tool.Execute(context.Background(), `{}`)
		if !res.IsError || !strings.Contains(res.Content, "not configured") {
			t.Fatalf("%s: want a not-configured error, got: %+v", tool.Name(), res)
		}
	}
}

// ------------------------------------------------------------------ get_release

func TestGetReleaseToolReportsNextStepPerStatus(t *testing.T) {
	cases := []struct {
		status domain.ReleaseStatus
		want   string
	}{
		{domain.ReleaseAwaitingVerdict, "finish_release"},
		{domain.ReleaseFailed, "rollback_release"},
		{domain.ReleasePending, "deploy_release"},
		{domain.ReleaseDeploying, "watch_release"},
		{domain.ReleaseVerifying, "watch_release"},
		{domain.ReleaseRollingBack, "watch_release"},
		{domain.ReleaseReleased, "Nothing to do"},
		{domain.ReleaseRolledBack, "Nothing to do"},
		{domain.ReleaseSuperseded, "Nothing to do"},
	}
	repoID, taskID := uuid.New(), uuid.New()
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			fake := &fakeReleaseService{release: domain.Release{ID: uuid.New(), Status: tc.status}}
			tool := newGetReleaseTool(releaseToolKit(repoID, fake))
			res := tool.Execute(releaseCtx(repoID, taskID), `{}`)
			if res.IsError {
				t.Fatalf("unexpected error: %s", res.Content)
			}
			var payload struct {
				Next string `json:"next"`
			}
			if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !strings.Contains(payload.Next, tc.want) {
				t.Fatalf("status %s: next = %q, want it to mention %q", tc.status, payload.Next, tc.want)
			}
			if fake.gotTaskID != taskID || fake.gotRepoID != repoID {
				t.Fatalf("release was not resolved from the task in context: got task=%s repo=%s", fake.gotTaskID, fake.gotRepoID)
			}
		})
	}
}

// N5: every release tool must resolve its release through ForAgent (which
// stamps AgentSeenAt) rather than ForTask directly — the hand-back
// watchdog's AgentSeenAt gate depends on a live tool call making that
// distinction, and only ForAgent does.
func TestReleaseToolsResolveTheReleaseThroughForAgent(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeReleaseService{release: domain.Release{ID: uuid.New(), Status: domain.ReleaseAwaitingVerdict}}
	kit := releaseToolKit(repoID, fake)
	ctx := releaseCtx(repoID, taskID)

	tools := []port.ToolExecutor{
		newGetReleaseTool(kit), newWatchReleaseTool(kit), newRunSmokeChecksTool(kit),
	}
	for _, tool := range tools {
		fake.forAgentCalls = 0
		tool.Execute(ctx, `{}`)
		if fake.forAgentCalls != 1 {
			t.Fatalf("%s: resolveRelease must call ForAgent exactly once, got %d calls", tool.Name(), fake.forAgentCalls)
		}
	}
}

func TestGetReleaseToolReportsNextStepForBatch(t *testing.T) {
	cases := []struct {
		name   string
		status domain.ReleaseStatus
		want   string
	}{
		{"draft", domain.ReleaseDraft, "a human cuts this release"},
		{"pending", domain.ReleasePending, "A human has cut this release"},
		{"awaiting_verdict", domain.ReleaseAwaitingVerdict, "bound runtime environment"},
		{"failed", domain.ReleaseFailed, "local_run.tail"},
	}
	repoID, taskID := uuid.New(), uuid.New()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeReleaseService{release: domain.Release{ID: uuid.New(), Status: tc.status, Mode: domain.DeliveryBatch}}
			tool := newGetReleaseTool(releaseToolKit(repoID, fake))
			res := tool.Execute(releaseCtx(repoID, taskID), `{}`)
			if res.IsError {
				t.Fatalf("unexpected error: %s", res.Content)
			}
			var payload struct {
				Next string `json:"next"`
			}
			if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !strings.Contains(payload.Next, tc.want) {
				t.Fatalf("status %s (batch): next = %q, want it to mention %q", tc.status, payload.Next, tc.want)
			}
		})
	}
}

func TestGetReleaseToolExplainsAMissingRelease(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeReleaseService{forTaskErr: fmt.Errorf("%w", domain.ErrReleaseNotFound)}
	tool := newGetReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{}`)
	if !res.IsError {
		t.Fatalf("expected an error result, got: %+v", res)
	}
	for _, want := range []string{"may not have merged", "delivery profile"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("want %q in the explanation, got: %s", want, res.Content)
		}
	}
}

// --------------------------------------------------------------- deploy_release

func TestDeployReleaseToolSuccess(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	releaseID := uuid.New()
	fake := &fakeReleaseService{
		release:   domain.Release{ID: releaseID, Status: domain.ReleasePending},
		deployRel: domain.Release{ID: releaseID, Status: domain.ReleaseDeploying},
	}
	tool := newDeployReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if fake.gotReleaseID != releaseID || fake.gotActor != domain.ReleaseActorAgent {
		t.Fatalf("want Deploy called with the resolved release as the agent, got id=%s actor=%s", fake.gotReleaseID, fake.gotActor)
	}
	if !strings.Contains(res.Content, "watch_release") {
		t.Errorf("want the next step to mention watch_release, got: %s", res.Content)
	}
}

func TestDeployReleaseToolRefusalTellsTheModelNotToRetry(t *testing.T) {
	for _, refusalErr := range []error{domain.ErrReleaseWrongStatus, domain.ErrReleaseNoDeploy} {
		t.Run(refusalErr.Error(), func(t *testing.T) {
			repoID, taskID := uuid.New(), uuid.New()
			fake := &fakeReleaseService{
				release:   domain.Release{ID: uuid.New()},
				deployErr: fmt.Errorf("%w: details", refusalErr),
			}
			tool := newDeployReleaseTool(releaseToolKit(repoID, fake))

			res := tool.Execute(releaseCtx(repoID, taskID), `{}`)
			if !res.IsError {
				t.Fatalf("want an error result, got: %+v", res)
			}
			if !strings.Contains(res.Content, "Do not retry") {
				t.Errorf("want the refusal to say not to retry, got: %s", res.Content)
			}
			if !strings.Contains(res.Content, "Nothing was deployed") {
				t.Errorf("want the refusal to say nothing was deployed, got: %s", res.Content)
			}
		})
	}
}

// deploy_release's description is the only place the agent learns what each
// batch executor actually does and that a tag-exists refusal for a cut batch
// release is a real failure, unlike dispatch's silent success — pin the key
// phrases so an edit cannot drop them unnoticed.
func TestDeployReleaseToolDescriptionCoversBatchExecutors(t *testing.T) {
	desc := newDeployReleaseTool(releaseToolKit(uuid.New(), &fakeReleaseService{})).Definition().Function.Description
	for _, want := range []string{"batch", "github_actions creates the release tag", "local runs the profile's command", "store starts a store build", "never re-used"} {
		if !strings.Contains(desc, want) {
			t.Errorf("deploy_release description missing %q: %s", want, desc)
		}
	}
}

// ---------------------------------------------------------------- watch_release

// The precondition this test pins: watch_release must surface a parked
// release EXACTLY the way the old get_task_deploy_status surfaced a pending
// deploy — a non-error ToolResult carrying ResourceBlock, not a JSON payload,
// so the agent loop parks the card on the resource named there.
func TestWatchReleaseToolParksOnAnUnsettledRelease(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	block := &domain.ResourceBlock{Resource: domain.ResourceReleaseWatch, Detail: "waiting for the deploy"}
	fake := &fakeReleaseService{
		release:    domain.Release{ID: uuid.New(), Status: domain.ReleasePending},
		watchRel:   domain.Release{ID: uuid.New(), Version: "abc1234", Mode: domain.DeliveryOnMerge, Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseDeploying},
		watchBlock: block,
	}
	tool := newWatchReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{}`)
	if res.IsError {
		t.Fatalf("a park must not be a tool error, got: %s", res.Content)
	}
	if res.ResourceBlock == nil {
		t.Fatalf("expected a ResourceBlock, got none: %+v", res)
	}
	if res.ResourceBlock.Resource != domain.ResourceReleaseWatch {
		t.Errorf("resource = %q, want %q", res.ResourceBlock.Resource, domain.ResourceReleaseWatch)
	}
	if res.Content == "" {
		t.Errorf("want a human-readable park message, got empty content")
	}
	// A park is a text explanation, not a JSON result — get_task_deploy_status
	// never wrapped its wait message in JSON either.
	var probe map[string]any
	if err := json.Unmarshal([]byte(res.Content), &probe); err == nil {
		t.Errorf("park content looks like JSON, want a plain sentence: %s", res.Content)
	}
}

func TestWatchReleaseToolReturnsTheReleaseOnceSettled(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeReleaseService{
		release:  domain.Release{ID: uuid.New()},
		watchRel: domain.Release{ID: uuid.New(), Status: domain.ReleaseAwaitingVerdict},
	}
	tool := newWatchReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if res.ResourceBlock != nil {
		t.Fatalf("a settled release must not park: %+v", res.ResourceBlock)
	}
	if !strings.Contains(res.Content, "finish_release") {
		t.Errorf("want the awaiting_verdict next step, got: %s", res.Content)
	}
}

// ------------------------------------------------------------ run_smoke_checks

func TestRunSmokeChecksToolSummarizesResults(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeReleaseService{
		release: domain.Release{ID: uuid.New()},
		smokeResults: []domain.SmokeResult{
			{OK: true}, {OK: true}, {OK: false, Error: "500"},
		},
	}
	tool := newRunSmokeChecksTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	var payload struct {
		Summary string               `json:"summary"`
		Results []domain.SmokeResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Summary != "2/3 smoke checks passed" {
		t.Errorf("summary = %q, want %q", payload.Summary, "2/3 smoke checks passed")
	}
	if len(payload.Results) != 3 {
		t.Errorf("want all 3 results carried, got %d", len(payload.Results))
	}
}

// --------------------------------------------------------------- finish_release

func TestFinishReleaseToolRequiresANote(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeReleaseService{release: domain.Release{ID: uuid.New()}}
	tool := newFinishReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{}`)
	if !res.IsError || !strings.Contains(res.Content, "note is required") {
		t.Fatalf("want a note-required error, got: %+v", res)
	}
	if fake.finishCall != 0 {
		t.Errorf("Finish must not be called without a note")
	}
}

func TestFinishReleaseToolSuccess(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	releaseID := uuid.New()
	fake := &fakeReleaseService{
		release:   domain.Release{ID: releaseID, Status: domain.ReleaseAwaitingVerdict},
		finishRel: domain.Release{ID: releaseID, Status: domain.ReleaseReleased},
	}
	tool := newFinishReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{"note":"checked logs since deployed_at, no new errors, smoke green"}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if fake.gotActor != domain.ReleaseActorAgent {
		t.Errorf("want Finish called as the agent, got %s", fake.gotActor)
	}
	if fake.gotNote == "" {
		t.Errorf("want the note threaded through to Finish")
	}
}

func TestFinishReleaseToolWrongStatusRefusal(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeReleaseService{
		release:   domain.Release{ID: uuid.New(), Status: domain.ReleaseFailed},
		finishErr: fmt.Errorf("%w: release is failed, only a human may finish it", domain.ErrReleaseWrongStatus),
	}
	tool := newFinishReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{"note":"trying anyway"}`)
	if !res.IsError {
		t.Fatalf("want an error result, got: %+v", res)
	}
	if !strings.Contains(res.Content, "Do not retry") {
		t.Errorf("want the refusal to say not to retry, got: %s", res.Content)
	}
}

// ------------------------------------------------------------- rollback_release

func TestRollbackReleaseToolValidatesReasonAndNote(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeReleaseService{release: domain.Release{ID: uuid.New()}}
	tool := newRollbackReleaseTool(releaseToolKit(repoID, fake))

	cases := []struct {
		name string
		body string
		want string
	}{
		{"missing reason", `{"note":"broke"}`, "reason must be one of"},
		{"invalid reason", `{"reason":"because","note":"broke"}`, "reason must be one of"},
		{"missing note", `{"reason":"deploy_failed"}`, "note is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := tool.Execute(releaseCtx(repoID, taskID), tc.body)
			if !res.IsError || !strings.Contains(res.Content, tc.want) {
				t.Fatalf("want error containing %q, got: %+v", tc.want, res)
			}
		})
	}
	if fake.rollbackCall != 0 {
		t.Errorf("Rollback must not be called on invalid arguments")
	}
}

func TestRollbackReleaseToolSuccessCarriesManualSteps(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	releaseID := uuid.New()
	fake := &fakeReleaseService{
		release: domain.Release{ID: releaseID, Status: domain.ReleaseFailed},
		rollbackRel: domain.Release{
			ID:     releaseID,
			Status: domain.ReleaseRollingBack,
			Rollback: &domain.ReleaseRollback{
				Reason:      domain.RollbackDeployFailed,
				ManualSteps: []string{"T-1: reverse the migration"},
			},
		},
	}
	tool := newRollbackReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{"reason":"deploy_failed","note":"the deploy job failed at the build step"}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if fake.gotReason != domain.RollbackDeployFailed || fake.gotActor != domain.ReleaseActorAgent {
		t.Errorf("want Rollback called with reason=deploy_failed actor=agent, got reason=%s actor=%s", fake.gotReason, fake.gotActor)
	}
	if !strings.Contains(res.Content, "reverse the migration") {
		t.Errorf("want the manual steps carried in the result, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "watch_release") {
		t.Errorf("want the next step to mention watch_release, got: %s", res.Content)
	}
}

// ErrRollbackNeedsHuman is a SUCCESSFUL, non-error result: an agent reading an
// error here would treat a working system as broken and go looking for
// another way to force the rollback through.
func TestRollbackReleaseToolNeedsHumanIsNotAnError(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeReleaseService{
		release:     domain.Release{ID: uuid.New(), Status: domain.ReleaseAwaitingVerdict},
		rollbackErr: fmt.Errorf("%w: written up for a human", domain.ErrRollbackNeedsHuman),
	}
	tool := newRollbackReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{"reason":"health_incident","note":"errors spiked after the deploy"}`)
	if res.IsError {
		t.Fatalf("a needs-human proposal must not be a tool error, got: %s", res.Content)
	}
	var payload struct {
		Proposed bool   `json:"proposed"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !payload.Proposed {
		t.Errorf("want proposed=true, got %+v", payload)
	}
	if payload.Message == "" {
		t.Errorf("want the proposal message carried through")
	}
}

func TestRollbackReleaseToolWrongStatusRefusal(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeReleaseService{
		release:     domain.Release{ID: uuid.New(), Status: domain.ReleaseDeploying},
		rollbackErr: fmt.Errorf("%w: release is deploying, not yet rollback-eligible", domain.ErrReleaseWrongStatus),
	}
	tool := newRollbackReleaseTool(releaseToolKit(repoID, fake))

	res := tool.Execute(releaseCtx(repoID, taskID), `{"reason":"deploy_failed","note":"looks bad"}`)
	if !res.IsError {
		t.Fatalf("want an error result, got: %+v", res)
	}
	if !strings.Contains(res.Content, "Do not retry") || !strings.Contains(res.Content, "Nothing was rolled back") {
		t.Errorf("want a no-retry refusal, got: %s", res.Content)
	}
}

// rollback_release's description is where the agent learns a batch rollback
// never redeploys and leads with unpublishing/halting the artifact — pin it.
func TestRollbackReleaseToolDescriptionCoversBatch(t *testing.T) {
	desc := newRollbackReleaseTool(releaseToolKit(uuid.New(), &fakeReleaseService{})).Definition().Function.Description
	for _, want := range []string{"nothing is redeployed", "cannot be unpublished by a revert", "unpublishing or halting"} {
		if !strings.Contains(desc, want) {
			t.Errorf("rollback_release description missing %q: %s", want, desc)
		}
	}
}

// ------------------------------------------------------------------ registration

func TestReleaseToolsAreRegisteredWithThePullRequestDependency(t *testing.T) {
	without := toolNames(NewExecutors(&ToolKit{Tasks: &fakeTaskManager{}}))
	with := toolNames(NewExecutors(&ToolKit{Tasks: &fakeTaskManager{}, PullRequests: &fakeTaskPullRequests{}, Releases: &fakeReleaseService{}}))

	for _, name := range []string{
		getReleaseToolName, deployReleaseToolName, watchReleaseToolName,
		runSmokeChecksToolName, finishReleaseToolName, rollbackReleaseToolName,
	} {
		if hasName(without, name) {
			t.Errorf("%s must not be registered without the PR dependency", name)
		}
		if !hasName(with, name) {
			t.Errorf("%s is not registered", name)
		}
	}
}

func TestTriggerReleaseToolNoLongerExists(t *testing.T) {
	execs := NewExecutors(&ToolKit{Tasks: &fakeTaskManager{}, PullRequests: &fakeTaskPullRequests{}, Releases: &fakeReleaseService{}})
	if hasName(toolNames(execs), "trigger_release") {
		t.Fatalf("trigger_release must have been removed, not just unregistered by default")
	}
}
