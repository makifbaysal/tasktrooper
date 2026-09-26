package board

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const getTaskPreviewToolName = "get_task_preview"

// TaskPreviewReader is application/cloud.Service.TaskPreviews. It is the one
// reader of the preview bypass secret outside the server, on purpose: the
// agent needs it to open a protected preview, the HTTP API never returns it.
type TaskPreviewReader interface {
	TaskPreviews(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPreviews, error)
}

type taskPreviewView struct {
	domain.TaskPreview
	BuiltFromPRHead *bool             `json:"built_from_pr_head,omitempty"`
	OpenURL         string            `json:"open_url,omitempty"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	Notes           []string          `json:"notes,omitempty"`
}

type taskPreviewResult struct {
	Branch    string            `json:"branch"`
	PRHeadSHA string            `json:"pr_head_sha,omitempty"`
	Previews  []taskPreviewView `json:"previews"`
	Note      string            `json:"note,omitempty"`
}

type getTaskPreviewTool struct{ kit *ToolKit }

func newGetTaskPreviewTool(kit *ToolKit) port.ToolExecutor { return &getTaskPreviewTool{kit: kit} }

func (t *getTaskPreviewTool) Name() string { return getTaskPreviewToolName }

func (t *getTaskPreviewTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: getTaskPreviewToolName,
			Description: "Find the task branch's per-branch preview deployment (Vercel builds every pushed branch / pull request as its own preview). " +
				"Per component: status (ready, building, error, canceled, none), the stable branch_url and this build's url, the commit it was built from and built_from_pr_head, and — for a preview behind Vercel Deployment Protection — open_url (for the browser; it sets a bypass cookie) and request_headers (send them on every HTTP request). " +
				"A preview is the task's own code only when status is ready AND built_from_pr_head is true. Read-only; it never deploys anything.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
				},
			},
		},
	}
}

func (t *getTaskPreviewTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(getTaskPreviewToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.Previews == nil {
		return toolError(getTaskPreviewToolName, "preview lookup is not configured on this deployment")
	}
	taskID, repositoryID, res := t.kit.resolveDeployTask(ctx, getTaskPreviewToolName, args.TaskID)
	if res != nil {
		return *res
	}
	previews, err := t.kit.Previews.TaskPreviews(ctx, repositoryID, taskID)
	if err != nil {
		return toolError(getTaskPreviewToolName, err.Error())
	}
	return toolJSON(getTaskPreviewToolName, taskPreviewOutput(previews))
}

func taskPreviewOutput(in domain.TaskPreviews) taskPreviewResult {
	out := taskPreviewResult{Branch: in.Branch, PRHeadSHA: in.HeadSHA, Previews: make([]taskPreviewView, 0, len(in.Previews))}
	if len(in.Previews) == 0 {
		out.Note = "No component of this repository has a per-branch preview environment (a `preview` environment bound to a Vercel project). Test locally or on stage."
	}
	for _, p := range in.Previews {
		out.Previews = append(out.Previews, previewView(p, in))
	}
	return out
}

func previewView(p domain.TaskPreview, in domain.TaskPreviews) taskPreviewView {
	v := taskPreviewView{TaskPreview: p}
	switch p.Status {
	case domain.TaskPreviewNone:
		v.Notes = append(v.Notes, fmt.Sprintf("Vercel has no deployment of branch %q yet; it builds one when the branch is pushed. Call again later, or test locally.", in.Branch))
		return v
	case domain.TaskPreviewBuilding:
		v.Notes = append(v.Notes, "Still building: call get_task_preview again before testing on it.")
	case domain.TaskPreviewError, domain.TaskPreviewCanceled:
		v.Notes = append(v.Notes, "This build did not finish; inspect_url shows why. Do not test on it.")
	}
	if in.HeadSHA != "" {
		built := domain.SameCommit(p.CommitSHA, in.HeadSHA)
		v.BuiltFromPRHead = &built
		if !built {
			v.Notes = append(v.Notes, fmt.Sprintf("Built from %s, not the pull request head %s: the head's build has not appeared yet.", domain.ShortSHA(p.CommitSHA), domain.ShortSHA(in.HeadSHA)))
		}
	}

	base := p.BranchURL
	if base == "" {
		base = p.URL
	}
	switch {
	case base == "":
	case !p.Protected:
		v.OpenURL = base
	case p.BypassSecret != "":
		v.OpenURL = bypassURL(base, p.BypassSecret)
		v.RequestHeaders = map[string]string{domain.VercelProtectionBypassHeader: p.BypassSecret}
	default:
		v.Notes = append(v.Notes, "This preview is behind Vercel Deployment Protection and the project has no Protection Bypass for Automation, so every automated request gets a login page. "+
			"Ask the human to create one in the Vercel project (Settings → Deployment Protection → Protection Bypass for Automation), then call get_task_preview again.")
	}
	return v
}

func bypassURL(base, secret string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return base
	}
	q := u.Query()
	q.Set(domain.VercelProtectionBypassHeader, secret)
	q.Set(domain.VercelProtectionBypassSetCookie, "true")
	u.RawQuery = q.Encode()
	return u.String()
}
