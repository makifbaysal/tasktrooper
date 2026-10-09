// Package mcpsurface serves a run's tool registry to an agent CLI on this
// computer: a streamable-HTTP MCP server on loopback, one per run, behind a
// bearer of its own.
package mcpsurface

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	Path              = "/mcp"
	tokenBytes        = 32
	readHeaderTimeout = 30 * time.Second
	serverVersion     = "1.0.0"
)

type Server struct{}

var _ port.ToolSurfaceServer = Server{}

func (Server) Serve(surface port.ToolSurface) (port.ServedToolSurface, error) {
	if surface.Registry == nil || surface.Context == nil {
		return port.ServedToolSurface{}, errors.New("a tool surface needs a registry and a context")
	}
	token, err := newToken()
	if err != nil {
		return port.ServedToolSurface{}, fmt.Errorf("minting the surface's bearer: %w", err)
	}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: surface.Name, Version: serverVersion}, nil)
	for _, def := range surface.Registry.DefinitionsForPolicy(surface.Policy) {
		name := def.Function.Name
		if name == "" {
			continue
		}
		server.AddTool(&sdkmcp.Tool{
			Name:        name,
			Description: def.Function.Description,
			InputSchema: objectSchema(def.Function.Parameters),
		}, callTool(surface, name))
	}
	// Stateless: a surface lives as long as one CLI run and holds no session a
	// reconnecting client would need back.
	mcpHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server },
		&sdkmcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	mux := http.NewServeMux()
	mux.Handle(Path, bearer(token, mcpHandler))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return port.ServedToolSurface{}, fmt.Errorf("listening on loopback: %w", err)
	}
	httpServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return surface.Context },
	}
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn().Err(err).Str("server_name", surface.Name).Msg("tool surface listener stopped")
		}
	}()
	var once sync.Once
	return port.ServedToolSurface{
		URL:   "http://" + listener.Addr().String() + Path,
		Token: token,
		Close: func() { once.Do(func() { _ = httpServer.Close() }) },
	}, nil
}

func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func bearer(token string, next http.Handler) http.Handler {
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimSpace(header[len(prefix):])), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tasktrooper-surface"`)
			http.Error(w, "a valid bearer token is required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// objectSchema is a tool's parameters as the SDK insists on them: an object
// schema, which it panics without.
func objectSchema(params map[string]any) map[string]any {
	if len(params) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	out := make(map[string]any, len(params)+1)
	for k, v := range params {
		out[k] = v
	}
	switch out["type"] {
	case "object":
	case nil:
		out["type"] = "object"
	default:
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return out
}

// callTool runs a call under the run's context — its workspace, environment,
// process scope and index — and ends it early when its own request ends.
func callTool(surface port.ToolSurface, name string) sdkmcp.ToolHandler {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		callCtx, cancel := context.WithCancel(surface.Context)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()

		args := "{}"
		if req != nil && req.Params != nil {
			if raw := strings.TrimSpace(string(req.Params.Arguments)); raw != "" && raw != "null" {
				args = raw
			}
		}
		result := surface.Registry.ExecuteWithPolicy(callCtx, domain.ToolCall{
			ID:       "mcp-" + uuid.NewString(),
			Type:     "function",
			Function: domain.FunctionCall{Name: name, Arguments: args},
		}, surface.Policy)
		return toolResult(result), nil
	}
}

func toolResult(result domain.ToolResult) *sdkmcp.CallToolResult {
	content := make([]sdkmcp.Content, 0, len(result.Images)+1)
	if result.Content != "" {
		content = append(content, &sdkmcp.TextContent{Text: result.Content})
	}
	for _, img := range result.Images {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil || len(data) == 0 {
			continue
		}
		mime := img.MediaType
		if mime == "" {
			mime = "image/png"
		}
		content = append(content, &sdkmcp.ImageContent{Data: data, MIMEType: mime})
	}
	return &sdkmcp.CallToolResult{Content: content, IsError: result.IsError}
}
