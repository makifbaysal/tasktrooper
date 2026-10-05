package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const maxStreamLine = 8 << 20

type event struct {
	Type    string `json:"type"`
	ParentToolUseID string `json:"parent_tool_use_id"`
	Subtype string `json:"subtype"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	Tools []string `json:"tools"`
	MCPServers *[]mcpServerState `json:"mcp_servers"`
	Message *cliMessage `json:"message"`
	Result       string    `json:"result"`
	IsError      bool      `json:"is_error"`
	NumTurns     int       `json:"num_turns"`
	TotalCostUSD float64   `json:"total_cost_usd"`
	Usage        *cliUsage `json:"usage"`
	Error string `json:"error"`
	APIErrorStatus int `json:"api_error_status"`
	RateLimit *rateLimitInfo `json:"rate_limit_info"`
}

type rateLimitInfo struct {
	Status         string  `json:"status"`
	ResetsAt       int64   `json:"resetsAt"`
	RateLimitType  string  `json:"rateLimitType"`
	Utilization    float64 `json:"utilization"`
	UnifiedWindows struct {
		FiveHour *rateLimitWindow `json:"five_hour"`
		SevenDay *rateLimitWindow `json:"seven_day"`
	} `json:"unifiedWindows"`
}

type rateLimitWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    int64   `json:"resetsAt"`
}

func (r *rateLimitInfo) resetTime(now time.Time) time.Time {
	if r == nil {
		return time.Time{}
	}
	if t := epochIfFuture(r.ResetsAt, now); !t.IsZero() {
		return t
	}
	if r.UnifiedWindows.FiveHour != nil {
		if t := epochIfFuture(r.UnifiedWindows.FiveHour.ResetsAt, now); !t.IsZero() {
			return t
		}
	}
	if r.UnifiedWindows.SevenDay != nil {
		if t := epochIfFuture(r.UnifiedWindows.SevenDay.ResetsAt, now); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func epochIfFuture(epoch int64, now time.Time) time.Time {
	if epoch <= 0 {
		return time.Time{}
	}
	t := time.Unix(epoch, 0)
	if !t.After(now) {
		return time.Time{}
	}
	return t
}

type mcpServerState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type sessionInit struct {
	SessionID string
	Model     string
	Tools     []string
	Servers   []mcpServerState
	ServersReported bool
}

func (i sessionInit) server(name string) (mcpServerState, bool) {
	for _, s := range i.Servers {
		if s.Name == name {
			return s, true
		}
	}
	return mcpServerState{}, false
}

func (i sessionInit) serverNames() []string {
	names := make([]string, 0, len(i.Servers))
	for _, s := range i.Servers {
		names = append(names, s.Name+"="+s.Status)
	}
	return names
}

type initReporter interface {
	OnInit(init sessionInit)
}

func reportInit(s sink, init sessionInit) {
	if r, ok := s.(initReporter); ok {
		r.OnInit(init)
	}
}

type cliMessage struct {
	Role string `json:"role"`
	Content json.RawMessage `json:"content"`
	Usage   *cliUsage       `json:"usage"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

type cliUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func (u *cliUsage) toDomain() domain.Usage {
	if u == nil {
		return domain.Usage{}
	}
	prompt := u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	return domain.Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}

type sink interface {
	OnSession(sessionID, model string)
	OnTurn()
	OnAssistantText(text string)
	OnToolUse(callID, name, arguments string)
	OnToolResult(callID, name, content string, isError bool)
}

type streamingSink struct {
	inner sink
	out   port.ChatStream
	streamed bool
	parent   string
}

func (s *streamingSink) SetParentCall(callID string) {
	s.parent = callID
	if ps, ok := s.inner.(core.ParentScoper); ok {
		ps.SetParentCall(callID)
	}
}

func newStreamingSink(inner sink, out port.ChatStream) sink {
	if out.OnText == nil && out.OnSegmentBreak == nil {
		return inner
	}
	return &streamingSink{inner: inner, out: out}
}

func (s *streamingSink) OnSession(sessionID, model string) { s.inner.OnSession(sessionID, model) }

func (s *streamingSink) OnTurn() { s.inner.OnTurn() }

func (s *streamingSink) OnAssistantText(text string) {
	s.inner.OnAssistantText(text)
	if s.parent != "" {
		return
	}
	if s.streamed {
		s.out.Text("\n\n")
	}
	s.out.Text(text)
	s.streamed = true
}

func (s *streamingSink) OnToolUse(callID, name, arguments string) {
	s.inner.OnToolUse(callID, name, arguments)
	if s.parent == "" && s.streamed {
		s.out.SegmentBreak()
		s.streamed = false
	}
}

func (s *streamingSink) OnToolResult(callID, name, content string, isError bool) {
	s.inner.OnToolResult(callID, name, content, isError)
}

type outcome struct {
	SessionID string
	Init sessionInit
	Text         string
	Usage        domain.Usage
	CostUSD      float64
	NumTurns     int
	Subtype      string
	IsError      bool
	ToolCalls    int
	ToolFailures int
	SawResult bool
	RateLimit *rateLimitInfo
	APIErrorStatus int
}

func parseStream(r io.Reader, s sink) (outcome, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxStreamLine)

	var out outcome
	lastAssistantText := ""
	toolNames := map[string]string{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.SessionID != "" {
			out.SessionID = ev.SessionID
		}

		switch ev.Type {
		case "system":
			if ev.Subtype == "init" {
				out.Init = sessionInit{
					SessionID:       ev.SessionID,
					Model:           ev.Model,
					Tools:           ev.Tools,
					ServersReported: ev.MCPServers != nil,
				}
				if ev.MCPServers != nil {
					out.Init.Servers = *ev.MCPServers
				}
				s.OnSession(ev.SessionID, ev.Model)
				reportInit(s, out.Init)
			}
		case "assistant":
			scopeParent(s, ev.ParentToolUseID)
			blocks := decodeBlocks(ev.Message)
			if turnHasContent(blocks) {
				s.OnTurn()
			}
			for _, block := range blocks {
				switch block.Type {
				case "text":
					if strings.TrimSpace(block.Text) == "" {
						continue
					}
					if ev.ParentToolUseID == "" {
						lastAssistantText = block.Text
					}
					s.OnAssistantText(block.Text)
				case "tool_use":
					toolNames[block.ID] = block.Name
					s.OnToolUse(block.ID, block.Name, string(block.Input))
				}
			}
		case "user":
			scopeParent(s, ev.ParentToolUseID)
			for _, block := range decodeBlocks(ev.Message) {
				if block.Type != "tool_result" {
					continue
				}
				name := toolNames[block.ToolUseID]
				out.ToolCalls++
				if block.IsError {
					out.ToolFailures++
				}
				s.OnToolResult(block.ToolUseID, name, blockText(block.Content), block.IsError)
			}
		case "result":
			out.SawResult = true
			out.Subtype = ev.Subtype
			out.IsError = ev.IsError
			out.NumTurns = ev.NumTurns
			out.CostUSD = ev.TotalCostUSD
			out.Usage = ev.Usage.toDomain()
			out.Text = firstNonEmpty(ev.Result, ev.Error, lastAssistantText)
			out.APIErrorStatus = ev.APIErrorStatus
		case "rate_limit_event":
			if ev.RateLimit != nil {
				out.RateLimit = ev.RateLimit
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("read agent cli stream: %w", err)
	}
	if out.Text == "" {
		out.Text = lastAssistantText
	}
	return out, nil
}

func scopeParent(s sink, parentCallID string) {
	if ps, ok := s.(core.ParentScoper); ok {
		ps.SetParentCall(parentCallID)
	}
}

func turnHasContent(blocks []contentBlock) bool {
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				return true
			}
		case "tool_use":
			return true
		}
	}
	return false
}

func decodeBlocks(msg *cliMessage) []contentBlock {
	if msg == nil || len(msg.Content) == 0 {
		return nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		return blocks
	}
	return nil
}

func blockText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
