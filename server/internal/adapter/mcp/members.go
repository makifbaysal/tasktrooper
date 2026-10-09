package mcp

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// memberConnectTimeout bounds one server's connect and tool listing; servers
// connect side by side.
const memberConnectTimeout = 10 * time.Second

// Members are the MCP servers a member runs on their own computer, connected
// once and held for as long as the executor lives. Their tools are the
// member's own to reach, so the destinations a server-side fetch must refuse
// (this machine, the local network) are allowed here.
type Members struct {
	tools   []port.ToolExecutor
	closers []func()
}

func memberPolicy() urlguard.Policy {
	p := urlguard.PublicOnly()
	p.AllowLoopback = true
	p.AllowPrivate = true
	return p
}

// ConnectMembers connects every server, in parallel, and keeps those that
// answered; a server that does not is logged by name and skipped.
func ConnectMembers(ctx context.Context, servers []domain.MCPServerConfig) *Members {
	type outcome struct {
		tools []port.ToolExecutor
		close func()
	}
	outcomes := make([]outcome, len(servers))
	var wg sync.WaitGroup
	for i, cfg := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			connectCtx, cancel := context.WithTimeout(ctx, memberConnectTimeout)
			defer cancel()
			executors, closeFunc, err := connectServer(connectCtx, cfg, memberPolicy(), &httpAuth{})
			if err != nil {
				log.Warn().Str("server", cfg.ID).Err(err).Msg("a member's MCP server did not connect; its tools are not served")
				return
			}
			tools := make([]port.ToolExecutor, 0, len(executors))
			for _, e := range executors {
				tools = append(tools, e)
			}
			outcomes[i] = outcome{tools: tools, close: closeFunc}
			log.Info().Str("server", cfg.ID).Int("tools", len(tools)).Msg("a member's MCP server connected")
		}()
	}
	wg.Wait()

	m := &Members{}
	for _, o := range outcomes {
		m.tools = append(m.tools, o.tools...)
		if o.close != nil {
			m.closers = append(m.closers, o.close)
		}
	}
	sort.SliceStable(m.tools, func(i, j int) bool { return m.tools[i].Name() < m.tools[j].Name() })
	return m
}

func (m *Members) Tools() []port.ToolExecutor {
	if m == nil {
		return nil
	}
	return append([]port.ToolExecutor(nil), m.tools...)
}

func (m *Members) Close() {
	if m == nil {
		return
	}
	for _, closeFunc := range m.closers {
		closeFunc()
	}
	m.closers = nil
}
