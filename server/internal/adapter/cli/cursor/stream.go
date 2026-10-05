package cursor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxStreamLine = 8 << 20

type event struct {
	Type      string                     `json:"type"`
	Subtype   string                     `json:"subtype"`
	SessionID string                     `json:"session_id"`
	Model     string                     `json:"model"`
	Message   *assistantMessage          `json:"message"`
	CallID    string                     `json:"call_id"`
	ToolCall  map[string]json.RawMessage `json:"tool_call"`
	IsError   bool                       `json:"is_error"`
	Result    string                     `json:"result"`
	Usage     *cliUsage                  `json:"usage"`
}

type cliUsage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens"`
	CacheWriteTokens int `json:"cacheWriteTokens"`
}

// inputTokens already excludes the cache buckets, so the prompt total adds
// them back to match the other CLI adapters.
func (u *cliUsage) toDomain() domain.Usage {
	if u == nil {
		return domain.Usage{}
	}
	prompt := u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens
	return domain.Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
}

type assistantMessage struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type conversationStep struct {
	AssistantMessage *struct {
		Text string `json:"text"`
	} `json:"assistantMessage"`
}

type toolCallDetail struct {
	Args   json.RawMessage `json:"args"`
	Result *struct {
		Success *struct {
			Content           string             `json:"content"`
			ResultSuffix      string             `json:"resultSuffix"`
			ConversationSteps []conversationStep `json:"conversationSteps"`
		} `json:"success"`
		Error *toolError `json:"error"`
	} `json:"result"`
}

// toolError reads both shapes: most tools send {message}, the task tool sends
// a bare string field named error.
type toolError struct {
	Message string
	Text    string
}

func (e *toolError) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		e.Text = s
		return nil
	}
	var o struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return nil
	}
	e.Message = o.Message
	e.Text = o.Error
	return nil
}

type sink = core.Sink

type outcome = core.Outcome

func parseStream(r io.Reader, s sink) (outcome, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxStreamLine)

	var out outcome
	turnSeen := false

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
				out.Model = ev.Model
				s.OnSession(ev.SessionID, ev.Model)
			}
		case "assistant":
			if !turnSeen {
				s.OnTurn()
				turnSeen = true
			}
			if ev.Message != nil {
				for _, c := range ev.Message.Content {
					if c.Type == "text" && c.Text != "" {
						out.Text = c.Text
						s.OnAssistantText(c.Text)
					}
				}
			}
		case "tool_call":
			name, detail := firstToolCall(ev.ToolCall)
			switch ev.Subtype {
			case "started":
				s.OnToolUse(ev.CallID, name, string(detail.Args))
			case "completed":
				out.ToolCalls++
				isErr := detail.Result != nil && detail.Result.Error != nil
				content := ""
				if detail.Result != nil {
					if detail.Result.Success != nil {
						content = detail.Result.Success.Content
						if name == taskToolName {
							content = subagentReply(detail.Result.Success.ConversationSteps, detail.Result.Success.ResultSuffix)
						}
					} else if detail.Result.Error != nil {
						content = detail.Result.Error.Message
						if content == "" {
							content = detail.Result.Error.Text
						}
					}
				}
				if isErr {
					out.ToolFailures++
				}
				s.OnToolResult(ev.CallID, name, content, isErr)
			}
		case "result":
			out.SawResult = true
			out.IsError = ev.IsError
			out.Status = ev.Subtype
			if strings.TrimSpace(ev.Result) != "" {
				out.Text = ev.Result
			}
			out.Usage = ev.Usage.toDomain()
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("read cursor-agent stream: %w", err)
	}
	return out, nil
}

func firstToolCall(m map[string]json.RawMessage) (string, toolCallDetail) {
	for k, raw := range m {
		var d toolCallDetail
		_ = json.Unmarshal(raw, &d)
		return strings.TrimSuffix(k, "ToolCall"), d
	}
	return "", toolCallDetail{}
}

const taskToolName = "task"

func subagentReply(steps []conversationStep, suffix string) string {
	for i := len(steps) - 1; i >= 0; i-- {
		if m := steps[i].AssistantMessage; m != nil && m.Text != "" {
			return m.Text
		}
	}
	return suffix
}
