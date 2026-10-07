package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

const DownloadToolName = "download_file"

// downloadMaxBytes caps one downloaded asset. It is deliberately larger than
// fetch_url's text cap: fetch_url feeds the model context, where a megabyte is
// enormous, while a download goes straight to disk and never enters the
// context — but a "download" that turns out to be a video or an installer is
// not an asset any frontend task needs, so it is still bounded.
const downloadMaxBytes = 10 << 20

type downloadTool struct {
	policy urlguard.Policy
}

type downloadArgs struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

// NewDownloadTool builds the binary download tool. fetch_url cannot do this
// job: it decodes the body as text for the model to read, which mangles any
// binary payload — an agent that needed a real PNG in the repository had no
// way to materialize one, so it committed <img> tags pointing at assets that
// did not exist and the page rendered a broken-image placeholder.
func NewDownloadTool(opts ...Option) port.ToolExecutor {
	// Reuse webTool's Option so tests can inject a loopback-allowing policy the
	// same way they do for fetch_url.
	w := &webTool{policy: urlguard.Default()}
	for _, opt := range opts {
		opt(w)
	}
	return &downloadTool{policy: w.policy}
}

func (t *downloadTool) Name() string {
	return DownloadToolName
}

func (t *downloadTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: DownloadToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type": "string",
					},
					"path": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"url", "path"},
			},
		},
	}
}

func (t *downloadTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a downloadArgs
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return domain.ToolResult{Name: DownloadToolName, Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}
	if a.URL == "" || strings.TrimSpace(a.Path) == "" {
		return domain.ToolResult{Name: DownloadToolName, Content: "url and path are both required", IsError: true}
	}

	root := registry.EffectiveWorkspaceDir(ctx)
	if root == "" {
		return domain.ToolResult{Name: DownloadToolName, Content: "workspace directory not set in context", IsError: true}
	}
	// Same protection the file writers apply: nothing may write into .git, by
	// any spelling the host filesystem resolves to it.
	abs, err := workspace.ResolveEditableWithinRoot(root, a.Path)
	var protected *workspace.ProtectedPathError
	if errors.As(err, &protected) {
		return domain.ToolResult{Name: DownloadToolName, Content: prompt.WebDownloadGitProtectedText(), IsError: true}
	}
	if err != nil {
		return domain.ToolResult{Name: DownloadToolName, Content: err.Error(), IsError: true}
	}

	// The URL is model-chosen off open-internet text, so it gets the same guard
	// as fetch_url: scheme allowlist, resolution, address rules, pinned dial.
	target, err := t.policy.Validate(ctx, a.URL)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogRaw(a.URL)).Msg("download_file destination refused")
		return domain.ToolResult{Name: DownloadToolName, Content: fetchFailed, IsError: true}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL.String(), nil)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("download_file request build failed")
		return domain.ToolResult{Name: DownloadToolName, Content: fetchFailed, IsError: true}
	}
	req.Header.Set("User-Agent", "local-llm-bridge/1.0")

	resp, err := t.policy.ClientFor(target, fetchTimeout).Do(req)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("download_file failed")
		return domain.ToolResult{Name: DownloadToolName, Content: fetchFailed, IsError: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.ToolResult{Name: DownloadToolName,
			Content: fmt.Sprintf("HTTP %d — nothing was saved. The asset URL is wrong or gone; find the correct direct URL.", resp.StatusCode),
			IsError: true}
	}

	contentType := resp.Header.Get("Content-Type")
	// An HTML answer to an asset URL is a landing page, an error page or a
	// consent wall — saving it as .png is exactly the broken-image bug this tool
	// exists to prevent, so it is refused rather than written.
	if strings.Contains(contentType, "text/html") {
		return domain.ToolResult{Name: DownloadToolName, Content: prompt.WebDownloadHTMLNotBinaryText(), IsError: true}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, downloadMaxBytes+1))
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("download_file body read failed")
		return domain.ToolResult{Name: DownloadToolName, Content: fetchFailed, IsError: true}
	}
	if len(body) > downloadMaxBytes {
		return domain.ToolResult{Name: DownloadToolName, Content: prompt.WebDownloadSizeLimitText(downloadMaxBytes >> 20), IsError: true}
	}
	if len(body) == 0 {
		return domain.ToolResult{Name: DownloadToolName, Content: "the URL returned an empty body — nothing was saved.", IsError: true}
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return domain.ToolResult{Name: DownloadToolName, Content: fmt.Sprintf("create directory: %v", err), IsError: true}
	}
	if err := os.WriteFile(abs, body, 0o644); err != nil {
		return domain.ToolResult{Name: DownloadToolName, Content: fmt.Sprintf("write %s: %v", a.Path, err), IsError: true}
	}

	log.Debug().Str("url", urlguard.LogValue(target.URL)).Str("path", a.Path).Int("bytes", len(body)).Msg("download_file saved asset")
	return domain.ToolResult{
		Name:    DownloadToolName,
		Content: prompt.WebDownloadSavedText(a.Path, len(body), contentType),
		IsError: false,
	}
}
