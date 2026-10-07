package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const anthropicVersion = "2023-06-01"
const anthropicDefaultMaxTokens = 8192

const anthropicLegacyMaxTokens = 4096

func anthropicMaxTokens(model string) int {
	if strings.HasPrefix(model, "claude-3-opus") || strings.HasPrefix(model, "claude-3-haiku") {
		return anthropicLegacyMaxTokens
	}
	return anthropicDefaultMaxTokens
}
func resolveAnthropicMaxTokens(model string, override int) int {
	if override > 0 {
		return override
	}
	return anthropicMaxTokens(model)
}

var knownAnthropicModels = []string{
	"claude-opus-5",
	"claude-sonnet-5",
	"claude-haiku-4-5",
	"claude-opus-4-8",
	"claude-opus-4-7",
	"claude-opus-4-6",
	"claude-sonnet-4-6",
}

type anthropicClient struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
}

func NewAnthropicClient(baseURL, model, apiKey string, timeout time.Duration) port.LLMClient {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &anthropicClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *anthropicClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

type anthropicRequest struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`

	System            interface{}                 `json:"system,omitempty"`
	Messages          []anthropicMessage          `json:"messages"`
	Tools             []anthropicTool             `json:"tools,omitempty"`
	Stream            bool                        `json:"stream,omitempty"`
	OutputConfig      *anthropicOutputConfig      `json:"output_config,omitempty"`
	ContextManagement *anthropicContextManagement `json:"context_management,omitempty"`
}

type anthropicContextManagement struct {
	Edits []anthropicContextEdit `json:"edits"`
}

type anthropicContextEdit struct {
	Type string `json:"type"`
}

const (
	clearToolUsesEdit     = "clear_tool_uses_20250919"
	contextManagementBeta = "context-management-2025-06-27"
)

type anthropicCacheControl struct {
	Type string `json:"type"`
}

func ephemeralCache() *anthropicCacheControl {
	return &anthropicCacheControl{Type: "ephemeral"}
}

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicOutputConfig struct {
	Format *anthropicOutputFormat `json:"format,omitempty"`
	Effort string                 `json:"effort,omitempty"`
}

type anthropicOutputFormat struct {
	Type   string                 `json:"type"`
	Schema map[string]interface{} `json:"schema"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Input        map[string]interface{} `json:"input,omitempty"`
	ToolUseID    string                 `json:"tool_use_id,omitempty"`
	Content      interface{}            `json:"content,omitempty"`
	Source       *anthropicImageSource  `json:"source,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicResponse struct {
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

func (u anthropicUsage) toDomain() domain.Usage {
	prompt := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	return domain.Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}

func anthropicImageBlocks(images []domain.ToolResultImage) []anthropicContent {
	blocks := make([]anthropicContent, 0, len(images))
	for _, img := range images {
		blocks = append(blocks, anthropicContent{
			Type: "image",
			Source: &anthropicImageSource{
				Type:      "base64",
				MediaType: img.MediaType,
				Data:      img.Data,
			},
		})
	}
	return blocks
}

const maxAnthropicBreakpoints = 4

func markCacheable(msg *anthropicMessage) bool {
	if msg == nil || len(msg.Content) == 0 {
		return false
	}
	last := &msg.Content[len(msg.Content)-1]
	if last.Type == "text" && last.Text == "" {
		return false
	}
	last.CacheControl = ephemeralCache()
	return true
}

func buildAnthropicRequest(model string, msgs []domain.Message, tools []domain.ToolDefinition, stream bool, respFormat *domain.ResponseFormat, cacheAnchor int) anthropicRequest {
	var systemParts []string
	var anthMsgs []anthropicMessage
	var pendingToolResults []anthropicContent
	seenTurn := false

	msgs = normalizeToolPairing(msgs)

	flush := func() {
		if len(pendingToolResults) > 0 {
			anthMsgs = append(anthMsgs, anthropicMessage{Role: "user", Content: pendingToolResults})
			pendingToolResults = nil
		}
	}

	if cacheAnchor < 0 {
		cacheAnchor = 0
	}
	if cacheAnchor > len(msgs) {
		cacheAnchor = len(msgs)
	}
	anchorMark := -1
	markAnchor := func() {
		anchorMark = len(anthMsgs) - 1
		if len(pendingToolResults) > 0 {
			anchorMark++
		}
	}

	for i, m := range msgs {
		if i == cacheAnchor {
			markAnchor()
		}
		switch m.Role {
		case domain.RoleSystem:
			if strings.TrimSpace(m.Content) == "" {
				break
			}
			if !seenTurn {
				systemParts = append(systemParts, m.Content)
				break
			}
			flush()
			anthMsgs = append(anthMsgs, anthropicMessage{
				Role:    "user",
				Content: []anthropicContent{{Type: "text", Text: systemReminder(m.Content)}},
			})
		case domain.RoleUser:
			seenTurn = true
			flush()
			if len(m.Images) == 0 {
				anthMsgs = append(anthMsgs, anthropicMessage{
					Role:    "user",
					Content: []anthropicContent{{Type: "text", Text: m.Content}},
				})
				continue
			}
			var content []anthropicContent
			if m.Content != "" {
				content = append(content, anthropicContent{Type: "text", Text: m.Content})
			}
			content = append(content, anthropicImageBlocks(m.Images)...)
			anthMsgs = append(anthMsgs, anthropicMessage{Role: "user", Content: content})
		case domain.RoleAssistant:
			seenTurn = true
			flush()
			var content []anthropicContent
			if m.Content != "" {
				content = append(content, anthropicContent{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				inputMap := map[string]interface{}{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &inputMap)
				content = append(content, anthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: inputMap,
				})
			}
			if len(content) > 0 {
				anthMsgs = append(anthMsgs, anthropicMessage{Role: "assistant", Content: content})
			}
		case domain.RoleTool:
			seenTurn = true

			var resultContent interface{} = m.Content
			if len(m.Images) > 0 {
				var blocks []anthropicContent
				if m.Content != "" {
					blocks = append(blocks, anthropicContent{Type: "text", Text: m.Content})
				}
				resultContent = append(blocks, anthropicImageBlocks(m.Images)...)
			}
			pendingToolResults = append(pendingToolResults, anthropicContent{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   resultContent,
			})
		}
	}
	if cacheAnchor == len(msgs) {
		markAnchor()
	}
	flush()

	var anthTools []anthropicTool
	for _, t := range tools {
		params := t.Function.Parameters
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		anthTools = append(anthTools, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: params,
		})
	}

	var outputConfig *anthropicOutputConfig
	if respFormat != nil {
		if respFormat.Schema != nil {
			outputConfig = &anthropicOutputConfig{
				Format: &anthropicOutputFormat{Type: "json_schema", Schema: respFormat.Schema},
			}
		} else {
			systemParts = append(systemParts, prompt.Text(jsonOnlyInstructionKey))
		}
	}

	budget := maxAnthropicBreakpoints

	if budget > 0 && len(anthTools) > 0 {
		anthTools[len(anthTools)-1].CacheControl = ephemeralCache()
		budget--
	}

	var systemField interface{}
	switch {
	case len(systemParts) == 0:
		// Leave it nil so `omitempty` drops the field, as before.
	case budget > 0:
		systemField = anthropicSystemBlocks(systemParts)
		budget--
	default:
		systemField = strings.Join(systemParts, "\n\n")
	}

	rolling := len(anthMsgs) - 1
	if budget > 0 && anchorMark >= 0 && anchorMark < rolling && markCacheable(&anthMsgs[anchorMark]) {
		budget--
	}

	if budget > 0 && rolling >= 0 {
		if markCacheable(&anthMsgs[rolling]) {
			budget--
		}
	}

	return anthropicRequest{
		Model:        model,
		MaxTokens:    anthropicMaxTokens(model),
		System:       systemField,
		Messages:     anthMsgs,
		Tools:        anthTools,
		Stream:       stream,
		OutputConfig: outputConfig,
	}
}

// anthropicSystemBlocks caches only the first leading system message — the
// agent's persona, skills and rules, identical across every task that agent
// runs. The rest (workspace path, score, KPIs, memories) changes per task; in
// the same block it made the whole persona a cache miss on every new task.
// The anchor and rolling breakpoints still cover those notes within a run.
// The leading "\n\n" keeps the concatenated text identical to the old single
// joined block.
func anthropicSystemBlocks(parts []string) []anthropicSystemBlock {
	blocks := []anthropicSystemBlock{{Type: "text", Text: parts[0], CacheControl: ephemeralCache()}}
	if len(parts) > 1 {
		blocks = append(blocks, anthropicSystemBlock{Type: "text", Text: "\n\n" + strings.Join(parts[1:], "\n\n")})
	}
	return blocks
}

func applyRequestTuning(payload *anthropicRequest, req domain.AgentRequest) (needsContextManagementBeta bool) {
	if req.Effort != "" {
		// output_config may not exist yet: effort and the structured-output
		// schema share the object but neither implies the other.
		if payload.OutputConfig == nil {
			payload.OutputConfig = &anthropicOutputConfig{}
		}
		payload.OutputConfig.Effort = req.Effort
	}
	if req.ClearToolResults {
		payload.ContextManagement = &anthropicContextManagement{
			Edits: []anthropicContextEdit{{Type: clearToolUsesEdit}},
		}
		return true
	}
	return false
}

func parseAnthropicContent(content []anthropicContent) (string, []domain.ToolCall) {
	var text strings.Builder
	var toolCalls []domain.ToolCall
	for _, block := range content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, domain.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: domain.FunctionCall{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}
	return text.String(), toolCalls
}

func (c *anthropicClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	model := c.model
	if req.Model != "" && req.Model != "local" {
		model = req.Model
	}

	payload := buildAnthropicRequest(model, req.Messages, req.Tools, false, req.ResponseFormat, req.CacheAnchorIndex)
	payload.MaxTokens = resolveAnthropicMaxTokens(model, req.MaxTokens)
	needsBeta := applyRequestTuning(&payload, req)
	body, err := json.Marshal(payload)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(httpReq)
	if needsBeta {
		httpReq.Header.Set("anthropic-beta", contextManagementBeta)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return domain.AgentResponse{}, httpError(resp, respBody)
	}

	var anthResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthResp); err != nil {
		return domain.AgentResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	text, toolCalls := parseAnthropicContent(anthResp.Content)
	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   text,
			ToolCalls: toolCalls,
		},
		Usage:      anthResp.Usage.toDomain(),
		StopReason: domain.NormalizeStopReason(anthResp.StopReason),
	}, nil
}

type anthropicSSEEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	Usage anthropicUsage `json:"usage"`
}

type streamToolBlock struct {
	id   string
	name string
	args strings.Builder
}

func (c *anthropicClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	model := c.model
	if req.Model != "" && req.Model != "local" {
		model = req.Model
	}

	payload := buildAnthropicRequest(model, req.Messages, req.Tools, true, req.ResponseFormat, req.CacheAnchorIndex)
	payload.MaxTokens = resolveAnthropicMaxTokens(model, req.MaxTokens)
	needsBeta := applyRequestTuning(&payload, req)
	body, err := json.Marshal(payload)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(httpReq)
	if needsBeta {
		httpReq.Header.Set("anthropic-beta", contextManagementBeta)
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return domain.AgentResponse{}, httpError(resp, errBody)
	}

	var fullText strings.Builder
	toolBlocks := make(map[int]*streamToolBlock)
	var streamUsage anthropicUsage
	var stopReason string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var evt anthropicSSEEvent
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}

		switch evt.Type {
		case "message_start":
			streamUsage = evt.Message.Usage
		case "content_block_start":
			if evt.ContentBlock.Type == "tool_use" {
				toolBlocks[evt.Index] = &streamToolBlock{id: evt.ContentBlock.ID, name: evt.ContentBlock.Name}
			}
		case "content_block_delta":
			switch evt.Delta.Type {
			case "text_delta":
				fullText.WriteString(evt.Delta.Text)
				onToken(evt.Delta.Text)
			case "input_json_delta":
				if tb, ok := toolBlocks[evt.Index]; ok {
					tb.args.WriteString(evt.Delta.PartialJSON)
				}
			}
		case "message_delta":
			if evt.Delta.StopReason != "" {
				stopReason = evt.Delta.StopReason
			}
			streamUsage.OutputTokens = evt.Usage.OutputTokens
			if evt.Usage.InputTokens > 0 {
				streamUsage.InputTokens = evt.Usage.InputTokens
			}
			if evt.Usage.CacheReadInputTokens > 0 {
				streamUsage.CacheReadInputTokens = evt.Usage.CacheReadInputTokens
			}
			if evt.Usage.CacheCreationInputTokens > 0 {
				streamUsage.CacheCreationInputTokens = evt.Usage.CacheCreationInputTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return domain.AgentResponse{}, fmt.Errorf("stream read: %w", err)
	}

	indices := make([]int, 0, len(toolBlocks))
	for idx := range toolBlocks {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	var toolCalls []domain.ToolCall
	for _, idx := range indices {
		tb := toolBlocks[idx]
		toolCalls = append(toolCalls, domain.ToolCall{
			ID:   tb.id,
			Type: "function",
			Function: domain.FunctionCall{
				Name:      tb.name,
				Arguments: tb.args.String(),
			},
		})
	}

	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   fullText.String(),
			ToolCalls: toolCalls,
		},
		Usage:      streamUsage.toDomain(),
		StopReason: domain.NormalizeStopReason(stopReason),
	}, nil
}

func (c *anthropicClient) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return knownAnthropicModels, nil
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return knownAnthropicModels, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return knownAnthropicModels, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, httpError(resp, respBody)
	}
	if resp.StatusCode != http.StatusOK {
		return knownAnthropicModels, nil
	}

	var modelsResp modelsResponse
	if err := json.Unmarshal(respBody, &modelsResp); err != nil {
		return knownAnthropicModels, nil
	}
	if len(modelsResp.Data) == 0 {
		return knownAnthropicModels, nil
	}

	models := make([]string, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

func (c *anthropicClient) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return nil, fmt.Errorf("anthropic does not support embeddings")
}
