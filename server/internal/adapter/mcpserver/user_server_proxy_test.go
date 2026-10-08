package mcpserver

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/mcp"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

var framePNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 9, 9, 9}

func TestUserServerImagesAreProxiedAsImageContent(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "figma", Version: "1.0.0"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "get_screenshot", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: "node 1:2"},
				&sdkmcp.ImageContent{MIMEType: "image/png", Data: framePNG},
			}}, nil
		})
	upstream := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil))
	t.Cleanup(upstream.Close)

	policy := urlguard.PublicOnly()
	policy.AllowLoopback = true
	manager := &mcpadapter.Manager{}
	manager.SetURLPolicy(policy)
	t.Cleanup(manager.Close)

	reg := registry.New()
	manager.LoadAndRegister(context.Background(), []domain.MCPServerConfig{
		{ID: "figma", Enabled: true, Transport: "http", URL: upstream.URL + "/mcp", Access: domain.MCPAccessListed},
	}, reg)
	require.Contains(t, reg.AllToolNames(), "mcp_figma_get_screenshot")

	tokens := NewRunTokenRegistry()
	app := fiber.New()
	New(reg, tokens).Register(app)

	designer, err := tokens.Mint(Run{Ctx: context.Background(), TaskKey: "tt-1",
		Policy: domain.ToolPolicy{AllowMCPServers: []string{"figma"}}})
	require.NoError(t, err)
	other, err := tokens.Mint(Run{Ctx: context.Background(), TaskKey: "tt-2"})
	require.NoError(t, err)

	_, listed := call(t, app, designer, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.Contains(t, toolNames(t, listed), "mcp_figma_get_screenshot")
	_, hidden := call(t, app, other, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.NotContains(t, toolNames(t, hidden), "mcp_figma_get_screenshot", "a listed server is not served to a run that does not name it")

	_, body := call(t, app, designer,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcp_figma_get_screenshot","arguments":{}}}`)
	blocks, isError := callResultOf(t, body)
	assert.False(t, isError)
	require.Len(t, blocks, 2)
	assert.Equal(t, "text", blocks[0].(map[string]any)["type"])
	assert.Equal(t, "node 1:2", blocks[0].(map[string]any)["text"])
	img := blocks[1].(map[string]any)
	assert.Equal(t, "image", img["type"])
	assert.Equal(t, "image/png", img["mimeType"])
	assert.Equal(t, base64.StdEncoding.EncodeToString(framePNG), img["data"])

	_, refused := call(t, app, other,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcp_figma_get_screenshot","arguments":{}}}`)
	_, refusedIsError := callResultOf(t, refused)
	assert.True(t, refusedIsError)
}
