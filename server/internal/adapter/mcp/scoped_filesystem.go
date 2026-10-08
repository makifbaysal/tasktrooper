package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

type scopedFilesystemSession struct {
	executors map[string]*mcpToolExecutor
	close     func()
}

var (
	scopedFilesystemMu sync.Mutex
	scopedFilesystem   = map[string]*scopedFilesystemSession{}
)

type ScopedFilesystem struct{}

func NewScopedFilesystem() *ScopedFilesystem {
	return &ScopedFilesystem{}
}

func (s *ScopedFilesystem) ExecuteScoped(ctx context.Context, root string, call domain.ToolCall) domain.ToolResult {
	rawName, ok := rawFilesystemToolName(call.Function.Name)
	if !ok {
		return domain.ToolResult{
			Name:    call.Function.Name,
			Content: fmt.Sprintf("unsupported scoped filesystem tool: %s", call.Function.Name),
			IsError: true,
		}
	}

	session, err := getScopedFilesystemSession(ctx, root)
	if err != nil {
		return domain.ToolResult{
			Name:    call.Function.Name,
			Content: fmt.Sprintf("scoped filesystem unavailable: %v", err),
			IsError: true,
		}
	}

	executor, ok := session.executors[rawName]
	if !ok {
		return domain.ToolResult{
			Name:    call.Function.Name,
			Content: fmt.Sprintf("filesystem tool %q not available for repository scope", rawName),
			IsError: true,
		}
	}
	return executor.Execute(ctx, call.Function.Arguments)
}

func rawFilesystemToolName(namespaced string) (string, bool) {
	const prefix = "mcp_filesystem_"
	if !strings.HasPrefix(namespaced, prefix) {
		return "", false
	}
	raw := strings.TrimPrefix(namespaced, prefix)
	if raw == "" {
		return "", false
	}
	return raw, true
}

func getScopedFilesystemSession(ctx context.Context, root string) (*scopedFilesystemSession, error) {
	scopedFilesystemMu.Lock()
	defer scopedFilesystemMu.Unlock()

	if existing, ok := scopedFilesystem[root]; ok {
		return existing, nil
	}

	cfg := domain.MCPServerConfig{
		ID:        "filesystem",
		Enabled:   true,
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", root},
	}

	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// stdio transport, so the URL policy is never consulted; the default is
	// passed rather than a zero Policy so this stays correct if the transport
	// ever changes.
	executors, closeFn, err := connectServer(connectCtx, cfg, urlguard.Default(), nil)
	if err != nil {
		return nil, err
	}

	byRaw := make(map[string]*mcpToolExecutor, len(executors))
	for _, e := range executors {
		byRaw[e.rawName] = e
	}

	session := &scopedFilesystemSession{executors: byRaw, close: closeFn}
	scopedFilesystem[root] = session
	return session, nil
}

func CloseScopedFilesystemSessions() {
	scopedFilesystemMu.Lock()
	defer scopedFilesystemMu.Unlock()
	for root, session := range scopedFilesystem {
		if session.close != nil {
			session.close()
		}
		delete(scopedFilesystem, root)
	}
}
