// Package executor runs agent turns and one-shot model calls for a caller on
// another machine: the user's own provider keys and local tools execute here,
// and the coordination tools (board, documents, criteria, memory, ask_user)
// arrive per run from that caller's MCP endpoint. Nothing is persisted; what a
// store would have recorded leaves as events on the run's stream.
package executor

import (
	"encoding/json"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const ProtocolVersion = 1

const (
	KindBoard = "board"
	KindChat  = "chat"
)

const (
	CodeBadRequest      = "bad_request"
	CodeConflict        = "conflict"
	CodeCancelled       = "cancelled"
	CodeTimeout         = "timeout"
	CodeUpstream        = "upstream"
	CodeRateLimited     = "rate_limited"
	CodeBudgetExhausted = "budget_exhausted"
	CodeInternal        = "internal"
	// CodeNotReady is an answer that does not exist yet: no embedding engine
	// on this computer, or no finished index for the repository.
	CodeNotReady = "not_ready"
)

const (
	EventStep       = "step"
	EventToolCall   = "tool_call"
	EventToolResult = "tool_result"
	EventText       = "text"
	EventUsage      = "usage"

	EventIndexProgress = "index_progress"
)

const (
	SourceLocal  = "local"
	SourceRemote = "remote"
)

type AgentSpec struct {
	Name         string            `json:"name"`
	SystemPrompt string            `json:"system_prompt"`
	ProviderID   string            `json:"provider_id"`
	Model        string            `json:"model"`
	ToolPolicy   domain.ToolPolicy `json:"tool_policy"`
	MaxTurns     int               `json:"max_turns"`
	Effort       string            `json:"effort,omitempty"`
}

type RemoteTools struct {
	URL        string `json:"url"`
	Token      string `json:"token"`
	ServerName string `json:"server_name"`
}

type AgentRun struct {
	RunID     string           `json:"run_id"`
	Kind      string           `json:"kind"`
	Agent     AgentSpec        `json:"agent"`
	Prompt    string           `json:"prompt,omitempty"`
	Messages  []domain.Message `json:"messages,omitempty"`
	Workspace string           `json:"workspace"`
	MCP       *RemoteTools     `json:"mcp,omitempty"`
	TimeoutMS int64            `json:"timeout_ms,omitempty"`
	// Index names the local code index the run reads: the run's checkout is
	// indexed first, its context is injected before the first turn, and the
	// index-backed code tools are served here.
	Index *IndexRef `json:"index,omitempty"`
}

type IndexRef struct {
	RepoKey string `json:"repo_key"`
	Branch  string `json:"branch,omitempty"`
	// WaitMS bounds how long the run waits for its index pass before it starts
	// without the index context; the pass goes on and the next run finds the
	// index ready. 0 means three minutes.
	WaitMS int64 `json:"wait_ms,omitempty"`
}

type UsageTotals struct {
	LLMCalls         int64 `json:"llm_calls"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

// RunSummary is what a run did, success or not. ToolUsage lists the tools
// that succeeded at least once: the evidence gates on the caller's side ask
// "did the agent actually do X", and a failed call is no evidence of that.
type RunSummary struct {
	Usage      UsageTotals    `json:"usage"`
	ToolUsage  []string       `json:"tool_usage"`
	ToolCounts map[string]int `json:"tool_counts,omitempty"`
	ToolErrors map[string]int `json:"tool_errors,omitempty"`
	DurationMS int64          `json:"duration_ms"`
}

type RunResult struct {
	FinalText string `json:"final_text"`
	RunSummary
	StopReason    string                       `json:"stop_reason,omitempty"`
	Clarification *domain.ClarificationRequest `json:"clarification,omitempty"`
	ResourceBlock *domain.ResourceBlock        `json:"resource_block,omitempty"`
}

// Failure is the error half of a run's terminal frame or of a one-shot call.
// Run is set once the run got as far as executing, so a caller can still
// account for the tokens and tools it spent.
type Failure struct {
	Code         string      `json:"code"`
	Message      string      `json:"message"`
	Run          *RunSummary `json:"run,omitempty"`
	Partial      string      `json:"partial,omitempty"`
	RetryAfterMS int64       `json:"retry_after_ms,omitempty"`
}

func (f *Failure) Error() string { return f.Code + ": " + f.Message }

type CompletionRequest struct {
	ProviderID     string                 `json:"provider_id"`
	Model          string                 `json:"model"`
	System         string                 `json:"system,omitempty"`
	Messages       []domain.Message       `json:"messages"`
	MaxTokens      int                    `json:"max_tokens,omitempty"`
	ResponseFormat *domain.ResponseFormat `json:"response_format,omitempty"`
	TimeoutMS      int64                  `json:"timeout_ms,omitempty"`
}

type Completion struct {
	Text       string       `json:"text"`
	Usage      domain.Usage `json:"usage"`
	StopReason string       `json:"stop_reason,omitempty"`
}

// EventHeader is stamped by the run, under one lock, in the order the events
// are handed to the Sink: seq is the order on the wire.
type EventHeader struct {
	Seq  int64     `json:"seq"`
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
}

func (h *EventHeader) header() *EventHeader { return h }

type Event interface {
	header() *EventHeader
}

// StepEvent carries one activity step exactly as the loop records it for a
// local run (type and JSON payload), so the caller can store it as its own.
type StepEvent struct {
	EventHeader
	Step string          `json:"step"`
	Data json.RawMessage `json:"data"`
}

type ToolCallEvent struct {
	EventHeader
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Source    string `json:"source"`
	Arguments string `json:"arguments"`
}

type ToolResultEvent struct {
	EventHeader
	CallID     string `json:"call_id"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	IsError    bool   `json:"is_error"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated,omitempty"`
	Images     int    `json:"images,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type TextEvent struct {
	EventHeader
	Delta        string `json:"delta,omitempty"`
	SegmentBreak bool   `json:"segment_break,omitempty"`
}

type UsageEvent struct {
	EventHeader
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
	domain.Usage
}

// Sink delivers a run's events to its caller. An error means the caller is
// gone, and the run is cancelled rather than left working for nobody.
type Sink interface {
	Send(event Event) error
}

type IndexEnsureRequest struct {
	Workspace string `json:"workspace"`
	RepoKey   string `json:"repo_key"`
	Branch    string `json:"branch,omitempty"`
}

type IndexSearchRequest struct {
	RepoKey string `json:"repo_key"`
	Branch  string `json:"branch,omitempty"`
	Query   string `json:"query"`
	K       int    `json:"k,omitempty"`
}

type IndexState struct {
	RepoKey        string `json:"repo_key"`
	Branch         string `json:"branch,omitempty"`
	Status         string `json:"status"`
	Files          int    `json:"files"`
	Chunks         int    `json:"chunks"`
	Symbols        int    `json:"symbols"`
	CommitSHA      string `json:"commit_sha,omitempty"`
	EmbeddingModel string `json:"embedding_model,omitempty"`
	EmbeddingDims  int    `json:"embedding_dims,omitempty"`
	SeededFrom     string `json:"seeded_from,omitempty"`
}

type IndexEnsureResult struct {
	IndexState
	DurationMS int64 `json:"duration_ms"`
}

type IndexHit struct {
	Path      string  `json:"path"`
	Symbol    string  `json:"symbol,omitempty"`
	Kind      string  `json:"kind,omitempty"`
	Language  string  `json:"language,omitempty"`
	Signature string  `json:"signature,omitempty"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float64 `json:"score"`
	Snippet   string  `json:"snippet"`
}

type IndexSearchResult struct {
	Index   IndexState `json:"index"`
	Results []IndexHit `json:"results"`
}

type IndexProgressEvent struct {
	EventHeader
	Phase          string `json:"phase"`
	FilesProcessed int    `json:"files_processed,omitempty"`
	FilesTotal     int    `json:"files_total,omitempty"`
	SeededFrom     string `json:"seeded_from,omitempty"`
}

type EmbeddingsRequest struct {
	BaseURL string `json:"embeddings_base_url"`
	Source  string `json:"embeddings_source,omitempty"`
}

type EmbeddingsState struct {
	V       int    `json:"v"`
	BaseURL string `json:"embeddings_base_url"`
	Source  string `json:"embeddings_source"`
}
