package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/executorapi"
)

const fakeMemberServerEnv = "TT_FAKE_MEMBER_MCP"

const memberSecret = "member-env-secret-8841"

func TestMain(m *testing.M) {
	if os.Getenv(fakeMemberServerEnv) == "1" {
		runFakeMemberServer()
		return
	}
	os.Exit(m.Run())
}

type echoArgs struct {
	Text string `json:"text"`
}

func runFakeMemberServer() {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "notes", Version: "1"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "echoes"}, func(_ context.Context, _ *sdkmcp.CallToolRequest, in echoArgs) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong:" + in.Text}}}, nil, nil
	})
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "env", Description: "reports the secret it was given"}, func(context.Context, *sdkmcp.CallToolRequest, echoArgs) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: os.Getenv("NOTES_TOKEN") + "|" + strings.Join(os.Args, " ")}}}, nil, nil
	})
	_ = server.Run(context.Background(), &sdkmcp.StdioTransport{})
}

func startWithMember(t *testing.T) (*Server, string) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	var stdout bytes.Buffer
	srv, err := Start(Config{
		Listen: defaultListen, Token: validToken, WorkspaceRoot: t.TempDir(),
		MCPServers: []MCPServerConfig{{
			Name: "notes", Command: exe, Args: []string{"-test.run=^$"},
			Env: map[string]string{fakeMemberServerEnv: "1", "NOTES_TOKEN": memberSecret},
		}},
	}, &stdout, Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv, strings.TrimPrefix(strings.TrimSpace(stdout.String()), ListeningPrefix)
}

func postExec(t *testing.T, base, path string, body any) (int, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+validToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(out)
}

func TestAMembersStdioServerIsServedOnTheSurfaceAsMcpNameTool(t *testing.T) {
	_, base := startWithMember(t)

	var opened struct {
		URL, Token string
		Tools      []string
	}
	status, raw := postExec(t, base, executorapi.PathMCPOpen, map[string]any{
		"run_id": "member-1", "tool_policy": map[string]any{"allow_mcp_servers": []string{"notes"}},
	})
	require.Equal(t, http.StatusOK, status, raw)
	require.NoError(t, json.Unmarshal([]byte(strings.NewReplacer(`"url"`, `"URL"`, `"token"`, `"Token"`, `"tools"`, `"Tools"`).Replace(raw)), &opened))
	assert.Contains(t, opened.Tools, "mcp_notes_echo")
	assert.Contains(t, opened.Tools, "mcp_notes_env")
	assert.NotContains(t, raw, memberSecret)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := connect(ctx, opened.URL, opened.Token)
	require.NoError(t, err)
	defer session.Close()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "mcp_notes_echo", Arguments: map[string]any{"text": "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "pong:hi", text(res))

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "mcp_notes_env", Arguments: map[string]any{"text": ""}})
	require.NoError(t, err)
	got := text(res)
	assert.True(t, strings.HasPrefix(got, memberSecret+"|"), "the secret reaches the child through its environment")
	assert.Equal(t, 1, strings.Count(got, memberSecret), "the child's argv carries no secret")

	status, raw = postExec(t, base, executorapi.PathMCPCalls, map[string]any{"run_id": "member-1"})
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, raw, `"name":"mcp_notes_echo"`)
	assert.NotContains(t, raw, memberSecret)
}

func TestAMembersServerReachesOnlyARunThatNamesIt(t *testing.T) {
	_, base := startWithMember(t)
	status, raw := postExec(t, base, executorapi.PathMCPOpen, map[string]any{"run_id": "member-2"})
	require.Equal(t, http.StatusOK, status, raw)
	assert.NotContains(t, raw, "mcp_notes_")
	status, raw = postExec(t, base, executorapi.PathMCPOpen, map[string]any{
		"run_id": "member-3", "tool_policy": map[string]any{"allow_mcp_servers": []string{"other"}},
	})
	require.Equal(t, http.StatusOK, status, raw)
	assert.NotContains(t, raw, "mcp_notes_")
}

func TestHealthListsAMembersMcpToolsAmongTheLocalOnes(t *testing.T) {
	_, base := startWithMember(t)
	req, err := http.NewRequest(http.MethodGet, base+executorapi.PathHealth, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+validToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var health struct {
		LocalTools []string `json:"local_tools"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&health))
	assert.Contains(t, health.LocalTools, "mcp_notes_echo")
	assert.Contains(t, health.LocalTools, "http_request")
}

func TestMCPServersInTheConfig(t *testing.T) {
	cfg, err := ParseConfig([]byte(configLine(`,"mcp_servers":[
		{"name":"notes","command":"/bin/notes","args":["--x"],"env":{"TOKEN":"tok-secret-1"}},
		{"name":"wiki","url":"https://wiki.example/mcp","headers":{"Authorization":"Bearer hdr-secret-2"}}]`)))
	require.NoError(t, err)
	require.Len(t, cfg.MCPServers, 2)
	assert.NotContains(t, cfg.String(), "tok-secret-1")
	assert.NotContains(t, cfg.String(), "hdr-secret-2")
	assert.NotContains(t, fmtSprint(cfg.MCPServers), "tok-secret-1")
	assert.ElementsMatch(t, []string{"tok-secret-1", "Bearer hdr-secret-2"}, memberSecrets(cfg.MCPServers))

	for name, servers := range map[string]string{
		"both":      `[{"name":"a","command":"x","url":"https://a.example"}]`,
		"neither":   `[{"name":"a"}]`,
		"bad name":  `[{"name":"a_b","command":"x"}]`,
		"duplicate": `[{"name":"a","command":"x"},{"name":"a","command":"y"}]`,
		"bad url":   `[{"name":"a","url":"ftp://a.example"}]`,
		"userinfo":  `[{"name":"a","url":"https://u:p@a.example"}]`,
		"bad env":   `[{"name":"a","command":"x","env":{"1X":"v"}}]`,
	} {
		_, err := ParseConfig([]byte(configLine(`,"mcp_servers":` + servers)))
		assert.Error(t, err, name)
	}
}

func fmtSprint(v any) string {
	return strings.TrimSpace(strings.Join([]string{fmt.Sprintf("%v", v), fmt.Sprintf("%#v", v)}, " "))
}
