//go:build unix

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

func TestConnectServerReportsWhyStdioServerDied(t *testing.T) {
	cfg := domain.MCPServerConfig{
		ID:        "playwright-mcp",
		Transport: "stdio",
		Command:   "sh",
		Args:      []string{"-c", "echo 'env: node: No such file or directory' >&2; exit 127"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, err := connectServer(ctx, cfg, urlguard.Default(), nil)

	require.Error(t, err)
	// Whether the handshake sees EOF or a broken pipe depends on when the
	// process dies; what matters is that the reason reaches the error.
	require.Regexp(t, `EOF|broken pipe`, err.Error())
	require.Contains(t, err.Error(), "env: node: No such file or directory")
	require.Contains(t, err.Error(), "exit status 127")
}

func TestTailBufferKeepsTheEnd(t *testing.T) {
	b := &tailBuffer{limit: 5}
	_, _ = b.Write([]byte("abc"))
	_, _ = b.Write([]byte("defgh"))
	require.Equal(t, "defgh", b.String())
}

func TestConnectTimeoutIsLongerForStdio(t *testing.T) {
	require.Greater(t, connectTimeout(domain.MCPServerConfig{Transport: "stdio"}), connectTimeout(domain.MCPServerConfig{Transport: "http"}))
}
