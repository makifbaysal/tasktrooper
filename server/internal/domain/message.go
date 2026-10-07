package domain

import "strings"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	// Images carries what the model should see: tool-result screenshots on a
	// RoleTool message, the human's image attachments on a RoleUser one. A
	// provider whose TOOL messages are text-only must drop those visibly,
	// never silently.
	Images []ToolResultImage `json:"images,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResultImage is one image attached to a tool result. Data is base64
// without a data: URI prefix; MediaType is the MIME type.
type ToolResultImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type ToolResult struct {
	ToolCallID    string                `json:"tool_call_id"`
	Name          string                `json:"name"`
	Content       string                `json:"content"`
	IsError       bool                  `json:"is_error"`
	Images        []ToolResultImage     `json:"images,omitempty"`
	Clarification *ClarificationRequest `json:"clarification,omitempty"`
	// ResourceBlock stops the turn the way Clarification does, but waits on a
	// contended resource rather than on a human (see resource_block.go).
	ResourceBlock *ResourceBlock `json:"resource_block,omitempty"`
}

type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ResponseFormat asks the provider to constrain output shape: "json_object"
// forces valid JSON, "json_schema" additionally validates against Schema.
// Providers without a native equivalent degrade to a system-prompt instruction.
type ResponseFormat struct {
	Type   string                 `json:"type"`
	Name   string                 `json:"name,omitempty"`
	Schema map[string]interface{} `json:"schema,omitempty"`
}

const (
	ResponseFormatJSONObject = "json_object"
	ResponseFormatJSONSchema = "json_schema"
)

// JSONResponseFormat is the plain JSON mode used by pipeline stages whose
// output is parsed with json.Unmarshal.
func JSONResponseFormat() *ResponseFormat {
	return &ResponseFormat{Type: ResponseFormatJSONObject}
}

// JSONSchemaResponseFormat is strict-schema JSON mode; on a provider that
// supports constrained decoding the response is guaranteed to match schema,
// which lets a pipeline stage retire its parse-repair retry loop. name
// identifies the schema (some providers reject an empty one); schema is a JSON
// Schema object.
func JSONSchemaResponseFormat(name string, schema map[string]interface{}) *ResponseFormat {
	return &ResponseFormat{Type: ResponseFormatJSONSchema, Name: name, Schema: schema}
}

type AgentRequest struct {
	Messages       []Message
	Tools          []ToolDefinition
	ProviderType   LLMProviderType
	Model          string
	ToolPolicy     ToolPolicy
	ResponseFormat *ResponseFormat
	// CacheAnchorIndex marks the end of the byte-stable prefix a run promises
	// not to rewrite; providers with explicit prompt-cache breakpoints put one
	// there. Zero = no anchor, so a caller that tracks no stable head gets the
	// provider's default placement. Never persisted.
	CacheAnchorIndex int
	// MaxTokens caps the model's output; zero means "the adapter's existing
	// default behaviour". JSON-schema pipeline stages deliberately leave it at
	// 0 — that output can legitimately be long.
	MaxTokens int
	// Effort is how hard the model should think, resolved from the agent
	// record; empty means the adapter's own default. Same knob the claude_code
	// executor passes as --effort; an adapter with no equivalent drops it.
	Effort string
	// ClearToolResults asks the provider to drop old tool RESULTS server-side,
	// keeping the tool_use blocks, and is off unless a caller sets it: it
	// rewrites the prefix that application/context/stable.go exists to hold
	// still, which invalidates the prompt cache from the edit point.
	ClearToolResults bool
}

type AgentResponse struct {
	Message       Message               `json:"message"`
	Usage         Usage                 `json:"usage"`
	Clarification *ClarificationRequest `json:"clarification,omitempty"`
	// ResourceBlock means the run produced no deliverable because a shared
	// resource was held by someone else.
	ResourceBlock *ResourceBlock `json:"resource_block,omitempty"`
	// CLISessionID is the host-executed session that produced this response;
	// a follow-up step resumes it instead of replaying the whole context.
	CLISessionID string `json:"cli_session_id,omitempty"`
	// Verification is the verifier's verdict on the run, when one ran; nil
	// must never be read as a pass. It rides on the response because the
	// verdict used to reach nothing outside the plan row, which let a card
	// whose own verification panel read FAILED sit in review.
	Verification *VerificationResult `json:"verification,omitempty"`
	// StopReason is why the provider stopped generating, normalized to the
	// StopReason* constants; empty when the adapter could not tell. Only
	// StopReasonMaxTokens changes what a caller does: the turn was cut off,
	// so any tool call in it may carry truncated arguments.
	StopReason string `json:"stop_reason,omitempty"`
	// Transcript is the whole working history an in-process run ended on —
	// opening context, every tool exchange, the final answer last — so a
	// follow-up continues the run instead of restarting it from the opening
	// context. Nil for host-executed runs, whose transcript lives in the CLI
	// session. Never serialized: it is the size of the run.
	Transcript []Message `json:"-"`
}

const (
	StopReasonEnd       = "end"
	StopReasonToolUse   = "tool_use"
	StopReasonMaxTokens = "max_tokens"
)

// NormalizeStopReason maps every provider's spelling onto the StopReason*
// constants; an unrecognised reason passes through lower-cased.
func NormalizeStopReason(raw string) string {
	switch r := strings.ToLower(strings.TrimSpace(raw)); r {
	case "":
		return ""
	case "end_turn", "stop", "stop_sequence":
		return StopReasonEnd
	case "tool_use", "tool_calls", "function_call":
		return StopReasonToolUse
	case "max_tokens", "length", "max_output_tokens":
		return StopReasonMaxTokens
	default:
		return r
	}
}

// Usage is one call's token accounting, normalized across providers:
// PromptTokens is the total (cached included), CacheReadTokens ⊆ PromptTokens,
// and PromptTokens−CacheReadTokens−CacheWriteTokens is what was processed at
// the plain rate. Providers disagree natively (Anthropic reports input_tokens
// EXCLUDING cache, OpenAI-compatible INCLUDING it), so billing arithmetic
// depends on every adapter converting here.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}
