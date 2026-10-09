package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// The local tool surface. A CLI run that carries a cloud `mcp` block is given
// the executor's loopback MCP server for that run instead of the cloud's URL:
// the same coordination tools, proxied by the executor with the cloud's
// bearer, plus the tools that have to act on THIS computer — browser_*, the
// local code index, download_file, http_request to the member's own
// localhost. The CLI then holds only the surface's bearer; the cloud's never
// reaches it.
//
// Anything short of a surface falls back to the cloud URL, as before: no
// executor, an executor that does not know mcp.open (404 unsupported_method),
// or one that could not open it. The run still has its coordination tools.

const (
	surfaceOpenTimeout  = 60 * time.Second
	surfaceCloseTimeout = 3 * time.Second
	mcpLocalTokenLabel  = "MCP_LOCAL_TOKEN"
	callsPollEvery      = time.Second
	callsReadTimeout    = 3 * time.Second
	maxSurfaceExtra     = 64 * 1024
)

type surfaceCloud struct {
	URL        string `json:"url"`
	Token      string `json:"token"`
	ServerName string `json:"server_name"`
}

type surfaceRequest struct {
	RunID      string            `json:"run_id"`
	Workspace  string            `json:"workspace,omitempty"`
	ToolPolicy json.RawMessage   `json:"tool_policy,omitempty"`
	CloudMCP   surfaceCloud      `json:"cloud_mcp"`
	Index      json.RawMessage   `json:"index,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	TimeoutMS  int64             `json:"timeout_ms,omitempty"`
}

type surfaceAnswer struct {
	URL        string `json:"url"`
	Token      string `json:"token"`
	ServerName string `json:"server_name"`
}

// cliSurface is the MCP configuration a CLI run is handed — nil for a run
// with none, the cloud's own on a fallback — and the tokens its output is
// scrubbed of: both, whichever the CLI was given.
type cliSurface struct {
	mcp        *mcpConfig
	cloudToken string
	localToken string
	close      func()
	calls      *surfaceCalls
}

// watchCalls emits the run's surface tool calls into its stream as they
// happen, and once more when the returned func is called — before the run's
// done — so none is left behind. A run with no surface gets a func that does
// nothing.
func (c cliSurface) watchCalls(ctx context.Context, into *call) func() {
	if c.calls == nil {
		return func() {}
	}
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(callsPollEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.calls.drain(ctx, into)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-finished
			c.calls.drain(context.Background(), into)
		})
	}
}

// surfaceCalls reads the executor's record of a surface's local tool calls by
// cursor, so each call is emitted exactly once however often it is asked.
type surfaceCalls struct {
	ex    *executorSupervisor
	inst  *executorProcess
	runID string

	mu    sync.Mutex
	after int
}

type surfaceCallsAnswer struct {
	Calls []struct {
		N          int    `json:"n"`
		Name       string `json:"name"`
		IsError    bool   `json:"is_error"`
		DurationMS int64  `json:"duration_ms"`
	} `json:"calls"`
	Next int `json:"next"`
}

type toolCallPayload struct {
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	Name       string `json:"name"`
	IsError    bool   `json:"is_error"`
	DurationMS int64  `json:"duration_ms"`
}

type toolCallEvent struct {
	V       int             `json:"v"`
	ID      string          `json:"id"`
	Event   string          `json:"event"`
	Payload toolCallPayload `json:"payload"`
}

func (s *surfaceCalls) drain(ctx context.Context, into *call) {
	s.mu.Lock()
	defer s.mu.Unlock()
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callsReadTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"run_id": s.runID, "after": s.after})
	req, err := http.NewRequestWithContext(readCtx, http.MethodPost, s.inst.base+"/exec/mcp.calls", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+s.inst.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.ex.client.Do(req)
	if err != nil {
		log.Debug().Err(err).Str("call", s.runID).Msg("could not read the run's surface tool calls")
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		return
	}
	var answer surfaceCallsAnswer
	if err := json.NewDecoder(io.LimitReader(resp.Body, executorResponseLimit)).Decode(&answer); err != nil {
		return
	}
	for _, call := range answer.Calls {
		if call.N <= s.after {
			continue
		}
		s.after = call.N
		_ = into.emit(toolCallEvent{V: protocolVersion, ID: into.id, Event: "event", Payload: toolCallPayload{
			Kind: "tool_call", Source: "local", Name: call.Name, IsError: call.IsError, DurationMS: call.DurationMS,
		}})
	}
	if answer.Next > s.after {
		s.after = answer.Next
	}
}

func (c cliSurface) held() map[string]string {
	return map[string]string{mcpRunTokenLabel: c.cloudToken, mcpLocalTokenLabel: c.localToken}
}

// openCLISurface asks the executor for the run's surface. cloud is the run's
// validated `mcp`; workspace is the run's own, relative to the workspace root
// the executor shares.
func (s *runnerServer) openCLISurface(ctx context.Context, callID, workspace string, cloud *mcpConfig, policy json.RawMessage, env map[string]string, timeout time.Duration) cliSurface {
	fallback := cliSurface{mcp: cloud, close: func() {}}
	if cloud == nil {
		return fallback
	}
	fallback.cloudToken = cloud.token
	ex := s.state.executorSupervisor()
	if ex == nil {
		return fallback
	}
	inst, rpcErr := ex.ready(ctx)
	if rpcErr != nil {
		log.Warn().Str("call", callID).Str("reason", rpcErr.Message).Msg("the run uses the cloud's MCP: the local executor is not ready")
		return fallback
	}
	req := surfaceRequest{
		RunID:      callID,
		Workspace:  workspace,
		ToolPolicy: policy,
		CloudMCP:   surfaceCloud{URL: cloud.url, Token: cloud.token, ServerName: cloud.serverName},
		Index:      cloud.index,
		Env:        env,
	}
	if timeout > 0 {
		req.TimeoutMS = (timeout + drainBudget).Milliseconds()
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fallback
	}
	openCtx, cancel := context.WithTimeout(ctx, surfaceOpenTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(openCtx, http.MethodPost, inst.base+"/exec/mcp.open", bytes.NewReader(body))
	if err != nil {
		return fallback
	}
	httpReq.Header.Set("Authorization", "Bearer "+inst.token)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := ex.client.Do(httpReq)
	if err != nil {
		log.Warn().Str("call", callID).Err(err).Msg("the run uses the cloud's MCP: the local executor could not be reached")
		return fallback
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, executorResponseLimit))
	if resp.StatusCode == http.StatusNotFound {
		log.Info().Str("call", callID).Msg("the run uses the cloud's MCP: this executor has no mcp.open")
		return fallback
	}
	if resp.StatusCode != http.StatusOK {
		if scrub := heldSecretRedactor(s.cfg.policy, map[string]string{mcpRunTokenLabel: cloud.token}); scrub != nil {
			raw = scrub(raw)
		}
		log.Warn().Str("call", callID).Int("status", resp.StatusCode).Str("answer", clipLine(string(raw), 400)).
			Msg("the run uses the cloud's MCP: the local executor did not open a surface")
		return fallback
	}
	var answer surfaceAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		log.Warn().Str("call", callID).Err(err).Msg("the run uses the cloud's MCP: the surface answer is not the expected document")
		return fallback
	}
	local, rpcErr := checkMCP(&mcpParams{URL: answer.URL, Token: answer.Token, ServerName: answer.ServerName})
	if rpcErr == nil && checkLoopbackURL(answer.URL) != nil {
		rpcErr = failure(codeUpstream, "the surface is not on loopback")
	}
	closeSurface := func() { ex.closeSurface(callID, inst) }
	if rpcErr != nil {
		closeSurface()
		log.Warn().Str("call", callID).Str("reason", rpcErr.Message).Msg("the run uses the cloud's MCP: the surface answer was refused")
		return fallback
	}
	log.Info().Str("call", callID).Str("mcp_server", local.serverName).Str("surface", local.host).Msg("the run uses this computer's tool surface")
	return cliSurface{
		mcp: local, cloudToken: cloud.token, localToken: local.token, close: closeSurface,
		calls: &surfaceCalls{ex: ex, inst: inst, runID: callID},
	}
}

// closeSurface is best effort: the surface also ends at its own timeout and
// with the executor.
func (e *executorSupervisor) closeSurface(runID string, inst *executorProcess) {
	ctx, cancel := context.WithTimeout(context.Background(), surfaceCloseTimeout)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{"run_id": runID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.base+"/exec/mcp.close", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		log.Debug().Err(err).Str("call", runID).Msg("could not close the run's tool surface")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	_ = resp.Body.Close()
}

// surfaceToolPolicy is the tool policy a CLI run's surface is opened with:
// the cloud's own `mcp.tool_policy` when it sent one, else the MCP half of
// claude.run's `tools`, server prefix removed, since those are the names the
// surface serves. A run whose tools name no MCP tool sends none.
func surfaceToolPolicy(cloud *mcpConfig, tools []string) json.RawMessage {
	if cloud == nil {
		return nil
	}
	if len(cloud.toolPolicy) > 0 {
		return cloud.toolPolicy
	}
	prefix := mcpToolPrefix + cloud.serverName + "__"
	var allow []string
	for _, tool := range tools {
		if name, ok := strings.CutPrefix(tool, prefix); ok && name != "" {
			allow = append(allow, name)
		}
	}
	if len(allow) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(map[string][]string{"allow_tools": allow})
	return encoded
}
