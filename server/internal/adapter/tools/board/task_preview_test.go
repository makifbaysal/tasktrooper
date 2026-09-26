package board

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeTaskPreviews struct {
	out                  domain.TaskPreviews
	gotRepoID, gotTaskID uuid.UUID
}

func (f *fakeTaskPreviews) TaskPreviews(_ context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPreviews, error) {
	f.gotRepoID, f.gotTaskID = repositoryID, taskID
	return f.out, nil
}

type previewToolOutput struct {
	Branch    string `json:"branch"`
	PRHeadSHA string `json:"pr_head_sha"`
	Note      string `json:"note"`
	Previews  []struct {
		Status          string            `json:"status"`
		BranchURL       string            `json:"branch_url"`
		BuiltFromPRHead *bool             `json:"built_from_pr_head"`
		OpenURL         string            `json:"open_url"`
		RequestHeaders  map[string]string `json:"request_headers"`
		Notes           []string          `json:"notes"`
	} `json:"previews"`
}

func runPreviewTool(t *testing.T, previews domain.TaskPreviews) (previewToolOutput, string) {
	t.Helper()
	repoID, taskID := uuid.New(), uuid.New()
	fake := &fakeTaskPreviews{out: previews}
	tool := newGetTaskPreviewTool(&ToolKit{Tasks: &fakeTaskManager{taskRepoID: repoID}, Previews: fake})

	ctx := registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
	res := tool.Execute(ctx, `{}`)
	require.False(t, res.IsError, res.Content)
	assert.Equal(t, taskID, fake.gotTaskID)
	assert.Equal(t, repoID, fake.gotRepoID)

	var out previewToolOutput
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	return out, res.Content
}

func readyPreview(protected bool, secret string) domain.TaskPreview {
	return domain.TaskPreview{
		ComponentName: "web", Status: domain.TaskPreviewReady, Provider: domain.CloudVercel,
		URL: "https://web-abc.vercel.app", BranchURL: "https://web-git-feature-login-acme.vercel.app",
		CommitSHA: "abc1234def", Protected: protected, BypassConfigured: secret != "", BypassSecret: secret,
	}
}

func TestGetTaskPreviewOpensAProtectedPreviewWithTheBypass(t *testing.T) {
	out, _ := runPreviewTool(t, domain.TaskPreviews{
		Branch: "feature/login", HeadSHA: "abc1234def",
		Previews: []domain.TaskPreview{readyPreview(true, "s3cret")},
	})

	require.Len(t, out.Previews, 1)
	p := out.Previews[0]
	assert.Equal(t, "https://web-git-feature-login-acme.vercel.app?x-vercel-protection-bypass=s3cret&x-vercel-set-bypass-cookie=true", p.OpenURL)
	assert.Equal(t, map[string]string{"x-vercel-protection-bypass": "s3cret"}, p.RequestHeaders)
	require.NotNil(t, p.BuiltFromPRHead)
	assert.True(t, *p.BuiltFromPRHead)
	assert.Empty(t, p.Notes)
}

func TestGetTaskPreviewProtectedWithoutBypassTellsTheHumanWhatToCreate(t *testing.T) {
	out, raw := runPreviewTool(t, domain.TaskPreviews{
		Branch: "feature/login", HeadSHA: "fff9999000",
		Previews: []domain.TaskPreview{readyPreview(true, "")},
	})

	require.Len(t, out.Previews, 1)
	p := out.Previews[0]
	assert.Empty(t, p.OpenURL)
	assert.Nil(t, p.RequestHeaders)
	require.NotNil(t, p.BuiltFromPRHead)
	assert.False(t, *p.BuiltFromPRHead)
	joined := strings.Join(p.Notes, " ")
	assert.Contains(t, joined, "Protection Bypass for Automation")
	assert.Contains(t, joined, "not the pull request head")
	assert.NotContains(t, raw, "x-vercel-protection-bypass=")
}

func TestGetTaskPreviewUnprotectedAndNoneAndEmpty(t *testing.T) {
	out, raw := runPreviewTool(t, domain.TaskPreviews{
		Branch: "feature/login",
		Previews: []domain.TaskPreview{
			readyPreview(false, ""),
			{ComponentName: "api", Status: domain.TaskPreviewNone},
		},
	})
	require.Len(t, out.Previews, 2)
	assert.Equal(t, "https://web-git-feature-login-acme.vercel.app", out.Previews[0].OpenURL)
	assert.Nil(t, out.Previews[0].BuiltFromPRHead, "no PR head known, no claim either way")
	assert.Empty(t, out.Previews[1].OpenURL)
	assert.Contains(t, strings.Join(out.Previews[1].Notes, " "), `"feature/login"`)
	assert.NotContains(t, raw, "bypass_secret")

	empty, _ := runPreviewTool(t, domain.TaskPreviews{Branch: "feature/login", Previews: []domain.TaskPreview{}})
	assert.Empty(t, empty.Previews)
	assert.NotEmpty(t, empty.Note)
}

func TestGetTaskPreviewNotConfigured(t *testing.T) {
	tool := newGetTaskPreviewTool(&ToolKit{Tasks: &fakeTaskManager{}})
	res := tool.Execute(context.Background(), `{"task_id":"`+uuid.New().String()+`"}`)
	assert.True(t, res.IsError)
}
