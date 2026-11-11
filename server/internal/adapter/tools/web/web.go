package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

const ToolName = "fetch_url"

const fetchTimeout = 30 * time.Second

// fetchFailed is the only thing a rejected or failed fetch ever says.
//
// It used to be fmt.Sprintf("http error: %v", err), which handed the model Go's
// *url.Error verbatim — and "connection refused" reads differently from "i/o
// timeout", which is a port scanner. The model picks the URL and the model reads
// the answer, so the two halves of the scanner were both inside the loop. A
// blocked destination, a refused connection, a timeout and an unreadable body
// are now indistinguishable from outside; the reason goes to the log.
const fetchFailed = "could not fetch that URL"

type webTool struct {
	maxBytes int64
	policy   urlguard.Policy
}

type args struct {
	URL string `json:"url"`
}

// Option customises the tool at construction.
type Option func(*webTool)

// WithURLPolicy replaces the destination policy. Production takes the default
// (public internet only, loopback per ALLOW_LOOPBACK_TOOL_URLS); tests use this
// to reach an httptest server on loopback.
func WithURLPolicy(p urlguard.Policy) Option {
	return func(w *webTool) { w.policy = p }
}

func New(maxBytes int64, opts ...Option) port.ToolExecutor {
	t := &webTool{maxBytes: maxBytes, policy: urlguard.Default()}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (w *webTool) Name() string {
	return ToolName
}

func (w *webTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: ToolName,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (w *webTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a args
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return domain.ToolResult{
			Name:    ToolName,
			Content: fmt.Sprintf("invalid arguments: %v", err),
			IsError: true,
		}
	}

	if a.URL == "" {
		return domain.ToolResult{
			Name:    ToolName,
			Content: "url is required",
			IsError: true,
		}
	}

	status, contentType, content, ok := w.fetch(ctx, a.URL)
	if !ok {
		return domain.ToolResult{Name: ToolName, Content: fetchFailed, IsError: true}
	}

	return domain.ToolResult{
		Name:    ToolName,
		Content: fmt.Sprintf("HTTP %d\nContent-Type: %s\n\n%s", status, contentType, content),
		IsError: false,
	}
}

// fetch is the guarded GET behind both the tool and the Reader; ok is false
// for any failure, and the reason only goes to the log.
func (w *webTool) fetch(ctx context.Context, rawURL string) (status int, contentType, content string, ok bool) {
	// The model chose this URL, and the text that made it choose came off the
	// open internet — so the destination is attacker-controlled and gets the
	// full guard: scheme allowlist, resolution, address rules, and a dial pinned
	// to the address that was checked. Without it, "url":"http://127.0.0.1:8080/
	// metrics" reaches this pod's own listener, where everything outside /v1 and
	// /admin is served unauthenticated (adapter/http/handler_ui.go).
	target, err := w.policy.Validate(ctx, rawURL)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogRaw(rawURL)).Msg("fetch_url destination refused")
		return 0, "", "", false
	}

	// Log the destination without its query string: an agent-supplied URL
	// routinely carries a token there, and a log line is the wrong place to keep
	// one. Same reason the model no longer gets the URL echoed back in errors.
	log.Debug().Str("url", urlguard.LogValue(target.URL)).Msg("fetching url")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL.String(), nil)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("fetch_url request build failed")
		return 0, "", "", false
	}
	req.Header.Set("User-Agent", "local-llm-bridge/1.0")

	resp, err := w.policy.ClientFor(target, fetchTimeout).Do(req)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("fetch_url failed")
		return 0, "", "", false
	}
	defer resp.Body.Close()

	maxBytes := w.maxBytes
	if maxBytes <= 0 {
		maxBytes = 1048576
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("fetch_url body read failed")
		return 0, "", "", false
	}

	content = string(body)
	contentType = resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		content = extractText(content)
	}
	return resp.StatusCode, contentType, content, true
}

// Reader reads a page's text through the same guarded fetch the fetch_url
// tool uses, for code that is not an agent (a study reading a landing page).
type Reader struct{ w *webTool }

// NewReader takes the same options as New.
func NewReader(maxBytes int64, opts ...Option) *Reader {
	t := &webTool{maxBytes: maxBytes, policy: urlguard.Default()}
	for _, opt := range opts {
		opt(t)
	}
	return &Reader{w: t}
}

// Read answers the page's text, or an error that says nothing about why
// (see fetchFailed). A non-2xx status is an error.
func (r *Reader) Read(ctx context.Context, rawURL string) (string, error) {
	status, _, content, ok := r.w.fetch(ctx, rawURL)
	if !ok || status < 200 || status > 299 {
		return "", errors.New(fetchFailed)
	}
	return content, nil
}

func extractText(html string) string {
	html = dropBlocks(html)
	inTag := false
	var sb strings.Builder

	for i := 0; i < len(html); i++ {
		ch := html[i]
		switch {
		case ch == '<':
			inTag = true
		case ch == '>':
			inTag = false
			sb.WriteByte(' ')
		case !inTag:
			sb.WriteByte(ch)
		}
	}

	text := sb.String()
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return strings.Join(result, "\n")
}

var blockRe = regexp.MustCompile(`(?is)<(script|style|noscript|svg|template)\b[^>]*>.*?</(script|style|noscript|svg|template)\s*>|<!--.*?-->`)

// dropBlocks removes what a reader never sees: scripts, styles, comments.
func dropBlocks(html string) string { return blockRe.ReplaceAllString(html, " ") }
