package opencode

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
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID"`
	Part      json.RawMessage `json:"part"`
	Error     *errorPayload   `json:"error"`
}

type errorPayload struct {
	Name string `json:"name"`
	Data struct {
		Message string `json:"message"`
	} `json:"data"`
}

type textPart struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type toolPart struct {
	CallID string `json:"callID"`
	// ID is the call id on 2.x, which dropped callID; on 1.x it is the part id.
	ID    string `json:"id"`
	Tool  string `json:"tool"`
	State struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input"`
		Output string          `json:"output"`
	} `json:"state"`
}

type finishPart struct {
	Reason string  `json:"reason"`
	Cost   float64 `json:"cost"`
	Tokens struct {
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
		Cache     struct {
			Read  int `json:"read"`
			Write int `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

type sink = core.Sink

type outcome = core.Outcome

func parseStream(r io.Reader, s sink) (outcome, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxStreamLine)

	var out outcome
	sessionAnnounced := false
	turnSeen := false
	texts := textAccum{values: map[string]string{}}

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
			if !sessionAnnounced {
				sessionAnnounced = true
				s.OnSession(ev.SessionID, "")
			}
		}
		switch ev.Type {
		case "step_start":
			if !turnSeen {
				s.OnTurn()
				turnSeen = true
			}
		case "text":
			var p textPart
			if json.Unmarshal(ev.Part, &p) == nil {
				if delta := texts.update(p.ID, p.Text); delta != "" {
					s.OnAssistantText(delta)
				}
			}
		case "tool_use":
			var p toolPart
			if json.Unmarshal(ev.Part, &p) == nil {
				if p.CallID == "" {
					p.CallID = p.ID
				}
				switch p.State.Status {
				case "completed", "error":
					out.ToolCalls++
					isErr := p.State.Status == "error"
					if isErr {
						out.ToolFailures++
					}
					s.OnToolUse(p.CallID, p.Tool, string(p.State.Input))
					s.OnToolResult(p.CallID, p.Tool, p.State.Output, isErr)
				default:
					s.OnToolUse(p.CallID, p.Tool, string(p.State.Input))
				}
			}
		case "step_finish":
			var p finishPart
			if json.Unmarshal(ev.Part, &p) == nil {
				out.SawResult = true
				out.Status = p.Reason
				out.CostUSD = p.Cost
				out.Usage = domain.Usage{
					PromptTokens:     p.Tokens.Input,
					CompletionTokens: p.Tokens.Output,
					TotalTokens:      p.Tokens.Input + p.Tokens.Output,
					CacheReadTokens:  p.Tokens.Cache.Read,
				}
			}
		case "error":
			out.SawResult = true
			out.IsError = true
			if ev.Error != nil {
				out.Status = ev.Error.Name
				out.Text = ev.Error.Data.Message
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("read opencode stream: %w", err)
	}
	if strings.TrimSpace(out.Text) == "" {
		out.Text = texts.join()
	}
	return out, nil
}

type textAccum struct {
	order  []string
	values map[string]string
}

func (t *textAccum) update(id, text string) string {
	prev, ok := t.values[id]
	if !ok {
		t.order = append(t.order, id)
	}
	t.values[id] = text
	if ok && strings.HasPrefix(text, prev) {
		return text[len(prev):]
	}
	return text
}

func (t *textAccum) join() string {
	parts := make([]string, 0, len(t.order))
	for _, id := range t.order {
		parts = append(parts, t.values[id])
	}
	return strings.Join(parts, "\n\n")
}
