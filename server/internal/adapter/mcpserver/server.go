package mcpserver

import (
	"encoding/json"
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const Path = "/mcp"

const PublicPath = "/api" + Path

func PublicURL(publicBaseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if base == "" {
		return ""
	}
	return base + PublicPath
}


const serverName = "tasktrooper"

const serverVersion = "1.0.0"

const defaultProtocolVersion = "2025-03-26"

var knownProtocolVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
	"2025-11-25": true,
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeUnauthorized   = -32001
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type Server struct {
	registry port.ToolRegistry
	tokens   *RunTokenRegistry
	loopbackOnly bool
}

func New(registry port.ToolRegistry, tokens *RunTokenRegistry) *Server {
	return &Server{registry: registry, tokens: tokens}
}


func (s *Server) SetLoopbackOnly(enabled bool) { s.loopbackOnly = enabled }

func (s *Server) Register(router fiber.Router) {
	router.Post(Path, s.handlePost)
	router.Get(Path, s.methodNotAllowed)
	router.Delete(Path, s.methodNotAllowed)
}

func (s *Server) methodNotAllowed(c *fiber.Ctx) error {
	return c.Status(fiber.StatusMethodNotAllowed).JSON(rpcResponse{
		JSONRPC: "2.0",
		ID:      json.RawMessage("null"),
		Error:   &rpcError{Code: codeInvalidRequest, Message: "this MCP endpoint only accepts POST"},
	})
}

func (s *Server) handlePost(c *fiber.Ctx) error {
	if !s.callerAllowed(c) {
		log.Warn().Str("remote_ip", c.IP()).
			Msg("mcp: refused a call from outside this host; the only legitimate client is a local agent cli session")
		return c.Status(fiber.StatusForbidden).JSON(rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: codeInvalidRequest, Message: "this MCP endpoint only serves processes on the server host"},
		})
	}

	run, ok := s.authenticate(c)
	if !ok {
		log.Warn().
			Str("token_prefix", tokenPrefix(c.Get(fiber.HeaderAuthorization))).
			Int("live_runs", s.liveRuns()).
			Str("method", peekMethod(c.Body())).
			Msg("mcp: rejected a call with an unknown or revoked run token; that session has lost its tasktrooper tools for the rest of its run")
		c.Set(fiber.HeaderWWWAuthenticate, `Bearer realm="tasktrooper-mcp"`)
		return c.Status(fiber.StatusUnauthorized).JSON(rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: codeUnauthorized, Message: "unknown or expired run token"},
		})
	}

	var req rpcRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: codeParseError, Message: "request body is not a JSON-RPC 2.0 message"},
		})
	}

	if len(req.ID) == 0 {
		return c.Status(fiber.StatusAccepted).Send(nil)
	}

	result, rpcErr := s.dispatch(run, req)
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
	return c.JSON(resp)
}

func (s *Server) callerAllowed(c *fiber.Ctx) bool {
	if !s.loopbackOnly {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(c.IP()))

	return ip != nil && ip.IsLoopback()
}

func tokenPrefix(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "<no bearer>"
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "<empty>"
	}
	if len(token) > 8 {
		return token[:8] + "…"
	}
	return token
}

func peekMethod(body []byte) string {
	var req struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Method == "" {
		return "?"
	}
	return req.Method
}

func (s *Server) liveRuns() int {
	if s.tokens == nil {
		return 0
	}
	return s.tokens.Live()
}

func (s *Server) authenticate(c *fiber.Ctx) (Run, bool) {
	if s.tokens == nil {
		return Run{}, false
	}
	const prefix = "Bearer "
	header := c.Get(fiber.HeaderAuthorization)
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return Run{}, false
	}
	return s.tokens.Lookup(strings.TrimSpace(header[len(prefix):]))
}

func (s *Server) dispatch(run Run, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params), nil
	case "ping":
		// The spec's keepalive: an empty result object, nothing else.
		return struct{}{}, nil
	case "tools/list":
		tools := servedTools(s.registry, run)
		s.logExposedTools(run, tools)
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		return s.callTool(run, req.Params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "unsupported method: " + req.Method}
	}
}

func (s *Server) logExposedTools(run Run, tools []toolInfo) {
	if len(tools) == 0 && !run.Policy.IsZero() {

		log.Warn().
			Str("task_key", run.TaskKey).
			Int("policy_tools", len(run.Policy.AllowTools)).
			Msg("mcp: this agent cli session is being served NO tasktrooper tools even though its run has a tool policy; it cannot move its card or tick a criterion")
		return
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	log.Info().
		Str("task_key", run.TaskKey).
		Int("tools", len(tools)).
		Msg("mcp: serving tasktrooper tools to an agent cli session")
	log.Debug().
		Str("task_key", run.TaskKey).
		Strs("tool_names", names).
		Msg("mcp: tasktrooper tools served to this session")
}

func (s *Server) initialize(params json.RawMessage) map[string]any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}

	_ = json.Unmarshal(params, &p)

	version := defaultProtocolVersion
	if knownProtocolVersions[p.ProtocolVersion] {
		version = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,

		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{"name": serverName, "version": serverVersion},
	}
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) callTool(run Run, params json.RawMessage) (any, *rpcError) {
	var p callToolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call params are not an object"}
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call needs a tool name"}
	}

	args := strings.TrimSpace(string(p.Arguments))
	if args == "" || args == "null" {

		args = "{}"
	}

	ctx := run.Ctx

	id := callID()
	traceStart(ctx, id, name, args)

	if !s.available(name, run) {
		refusal := s.unavailable(name, run)
		traceResult(ctx, id, name, refusalText(refusal), true)
		return refusal, nil
	}

	result := s.registry.ExecuteWithPolicy(ctx, domain.ToolCall{
		ID:       id,
		Type:     "function",
		Function: domain.FunctionCall{Name: name, Arguments: args},
	}, run.Policy)
	traceResult(ctx, id, name, result.Content, result.IsError)

	log.Debug().
		Str("task_key", run.TaskKey).
		Str("tool", name).
		Bool("is_error", result.IsError).
		Msg("mcp: agent cli session called a tasktrooper tool")

	if result.Clarification != nil && run.canAsk() {
		domain.ClarificationSinkFrom(ctx).Record(*result.Clarification)
		return textResult(clarificationRecordedKey.Render(toolNameInput{Name: name}), false), nil
	}
	return resultToMCP(name, result), nil
}

func (s *Server) available(name string, run Run) bool {
	for _, tool := range servedTools(s.registry, run) {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func (s *Server) unavailable(name string, run Run) callToolResult {
	switch {
	case name == domain.AskUserToolName && !run.canAsk():
		return textResult(askUserUnavailableKey.Render(toolNameInput{Name: name}), true)
	case run.SkillsOnDisk && name == skillLoadTool:
		return textResult(skillLoadUnavailableKey.Render(toolNameInput{Name: name}), true)
	case !exposed(name, run):
		return textResult(toolNotExposedKey.Render(toolNameInput{Name: name}), true)
	case !s.registered(name):
		return textResult(toolNotRegisteredKey.Render(toolNameInput{Name: name}), true)
	default:
		return textResult(toolPolicyDeniedKey.Render(toolNameInput{Name: name}), true)
	}
}

func (s *Server) registered(name string) bool {
	for _, known := range s.registry.AllToolNames() {
		if known == name {
			return true
		}
	}
	return false
}
