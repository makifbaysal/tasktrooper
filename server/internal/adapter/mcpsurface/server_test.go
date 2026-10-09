package mcpsurface

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func tool(t *testing.T, name string, params map[string]any) *mocks.ToolExecutor {
	exec := mocks.NewToolExecutor(t)
	exec.On("Name").Return(name).Maybe()
	exec.On("Definition").Return(domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{Name: name, Parameters: params}}).Maybe()
	return exec
}

type workspaceKey struct{}

func TestASurfaceServesItsRegistryUnderTheRunsContext(t *testing.T) {
	screenshot := tool(t, "browser_screenshot", nil)
	png := []byte("\x89PNG fake")
	screenshot.On("Execute", mock.Anything, "{}").Return(domain.ToolResult{
		Content: "captured", Images: []domain.ToolResultImage{{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(png)}},
	}).Once()
	fetch := tool(t, "fetch_url", map[string]any{"properties": map[string]any{"url": map[string]any{"type": "string"}}})
	fetch.On("Execute", mock.Anything, `{"url":"http://x"}`).Run(func(args mock.Arguments) {
		assert.Equal(t, "the run's", args.Get(0).(context.Context).Value(workspaceKey{}))
	}).Return(domain.ToolResult{Content: "nope", IsError: true}).Once()
	hidden := tool(t, "deploy_release", map[string]any{"type": "object"})

	reg := registry.New()
	reg.Register(screenshot)
	reg.Register(fetch)
	reg.Register(hidden)
	runCtx, endRun := context.WithCancel(context.WithValue(context.Background(), workspaceKey{}, "the run's"))
	defer endRun()
	served, err := Server{}.Serve(port.ToolSurface{
		Name: "tasktrooper", Registry: reg, Context: runCtx,
		Policy: domain.ToolPolicy{AllowTools: []string{"browser_*", "fetch_url"}},
	})
	require.NoError(t, err)
	defer served.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "cli", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint: served.URL, HTTPClient: &http.Client{Transport: bearerTransport{served.Token}}, DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err)
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, listed.Tools, 2, "the policy decides what is listed")
	for _, listedTool := range listed.Tools {
		schema, _ := listedTool.InputSchema.(map[string]any)
		assert.Equal(t, "object", schema["type"], listedTool.Name)
	}

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "browser_screenshot"})
	require.NoError(t, err)
	require.Len(t, res.Content, 2)
	assert.Equal(t, "captured", res.Content[0].(*sdkmcp.TextContent).Text)
	assert.Equal(t, png, res.Content[1].(*sdkmcp.ImageContent).Data)

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "fetch_url", Arguments: map[string]any{"url": "http://x"}})
	require.NoError(t, err)
	assert.True(t, res.IsError)

	_, err = session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "deploy_release"})
	assert.Error(t, err, "a tool outside the policy is not on the surface at all")
}

func TestObjectSchemaIsWhatTheSDKAccepts(t *testing.T) {
	empty := map[string]any{"type": "object", "properties": map[string]any{}}
	assert.Equal(t, empty, objectSchema(nil))
	assert.Equal(t, empty, objectSchema(map[string]any{"type": "string"}))
	assert.Equal(t, "object", objectSchema(map[string]any{"properties": map[string]any{}})["type"])
	in := map[string]any{"type": "object", "required": []string{"a"}}
	assert.Equal(t, in, objectSchema(in))
}
