package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type openAICompatClient struct {
	baseURL      string
	model        string
	apiKey       string
	extraHeaders map[string]string
	httpClient   *http.Client
}

const idleConnTimeout = 30 * time.Second

func newIdleSafeTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.IdleConnTimeout = idleConnTimeout
	return t
}

func NewOpenAICompatClient(baseURL, model, apiKey string, timeout time.Duration) port.LLMClient {
	return newOpenAICompatClientExt(baseURL, model, apiKey, timeout, nil)
}

func newOpenAICompatClientExt(baseURL, model, apiKey string, timeout time.Duration, extraHeaders map[string]string) port.LLMClient {
	baseURL = strings.TrimRight(baseURL, "/")
	return &openAICompatClient{
		baseURL:      baseURL,
		model:        model,
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
		httpClient:   &http.Client{Timeout: timeout, Transport: newIdleSafeTransport()},
	}
}

func (c *openAICompatClient) setAuthHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range c.extraHeaders {
		req.Header.Set(k, v)
	}
}

type chatRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	Tools          []toolDef           `json:"tools,omitempty"`
	ToolChoice     string              `json:"tool_choice,omitempty"`
	Stream         bool                `json:"stream"`
	StreamOptions  *chatStreamOptions  `json:"stream_options,omitempty"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
	MaxTokens      int                 `json:"max_tokens,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *chatJSONSchema `json:"json_schema,omitempty"`
}

type chatJSONSchema struct {
	Name   string                 `json:"name"`
	Strict bool                   `json:"strict,omitempty"`
	Schema map[string]interface{} `json:"schema"`
}

func buildResponseFormat(rf *domain.ResponseFormat) *chatResponseFormat {
	if rf == nil {
		return nil
	}
	if rf.Schema == nil {
		return &chatResponseFormat{Type: domain.ResponseFormatJSONObject}
	}
	name := rf.Name
	if name == "" {
		name = "response"
	}
	return &chatResponseFormat{
		Type:       domain.ResponseFormatJSONSchema,
		JSONSchema: &chatJSONSchema{Name: name, Strict: true, Schema: rf.Schema},
	}
}

type chatMessage struct {
	Role         string            `json:"role"`
	Content      string            `json:"content"`
	ContentParts []chatContentPart `json:"-"`
	ToolCalls    []toolCall        `json:"tool_calls,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
	Name         string            `json:"name,omitempty"`
}

type chatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *chatImageURL `json:"image_url,omitempty"`
}

type chatImageURL struct {
	URL string `json:"url"`
}

func (m chatMessage) MarshalJSON() ([]byte, error) {
	type plain chatMessage
	if len(m.ContentParts) == 0 {
		return json.Marshal(plain(m))
	}
	return json.Marshal(struct {
		Role       string            `json:"role"`
		Content    []chatContentPart `json:"content"`
		ToolCalls  []toolCall        `json:"tool_calls,omitempty"`
		ToolCallID string            `json:"tool_call_id,omitempty"`
		Name       string            `json:"name,omitempty"`
	}{
		Role:       m.Role,
		Content:    m.ContentParts,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
		Name:       m.Name,
	})
}

type toolDef struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type usage struct {
	PromptTokens        int                  `json:"prompt_tokens"`
	CompletionTokens    int                  `json:"completion_tokens"`
	TotalTokens         int                  `json:"total_tokens"`
	PromptTokensDetails *promptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

func (u usage) toDomain() domain.Usage {
	total := u.TotalTokens
	if total == 0 {
		total = u.PromptTokens + u.CompletionTokens
	}
	cached := 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	return domain.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      total,
		CacheReadTokens:  cached,

		CacheWriteTokens: 0,
	}
}

type modelsResponse struct {
	Data []modelData `json:"data"`
}

type modelData struct {
	ID string `json:"id"`
}

func (c *openAICompatClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	model := c.model
	if req.Model != "" && req.Model != "local" {
		model = req.Model
	}

	msgs := buildChatMessages(req.Messages)
	tools := buildToolDefs(req.Tools)

	payload := chatRequest{
		Model:          model,
		Messages:       msgs,
		Stream:         false,
		ResponseFormat: buildResponseFormat(req.ResponseFormat),
		MaxTokens:      req.MaxTokens,
	}
	applyTools(&payload, tools)

	body, err := json.Marshal(payload)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setAuthHeaders(httpReq)

	log.Debug().Str("model", model).Int("messages", len(msgs)).Int("tools", len(tools)).Msg("sending chat request")

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

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return domain.AgentResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return domain.AgentResponse{}, fmt.Errorf("llm returned no choices")
	}

	if chatResp.Usage.TotalTokens > 0 {
		log.Debug().
			Int("prompt_tokens", chatResp.Usage.PromptTokens).
			Int("completion_tokens", chatResp.Usage.CompletionTokens).
			Int("total_tokens", chatResp.Usage.TotalTokens).
			Msg("llm usage")
	}

	cm := chatResp.Choices[0].Message
	msg := domain.Message{
		Role:    domain.Role(cm.Role),
		Content: cm.Content,
	}
	msg.ToolCalls = parseToolCalls(cm.ToolCalls)

	return domain.AgentResponse{
		Message:    msg,
		Usage:      chatResp.Usage.toDomain(),
		StopReason: domain.NormalizeStopReason(chatResp.Choices[0].FinishReason),
	}, nil
}

func buildToolDefs(tools []domain.ToolDefinition) []toolDef {
	out := make([]toolDef, 0, len(tools))
	for _, t := range tools {
		out = append(out, toolDef{
			Type: t.Type,
			Function: functionDef{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}
	return out
}

func applyTools(payload *chatRequest, tools []toolDef) {
	if len(tools) == 0 {
		return
	}
	payload.Tools = tools
	// Some local runtimes only emit tool calls when tool_choice is explicit.
	payload.ToolChoice = "auto"
}

type streamChunk struct {
	Choices []streamChoice `json:"choices"`
	Usage   *usage         `json:"usage,omitempty"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type streamDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []streamToolCall `json:"tool_calls,omitempty"`
}

type streamToolCall struct {
	Index    *int                `json:"index,omitempty"`
	ID       string              `json:"id,omitempty"`
	Type     string              `json:"type,omitempty"`
	Function *streamFunctionCall `json:"function,omitempty"`
}

type streamFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type toolCallAccum struct {
	call toolCall
	args strings.Builder
}

type toolCallStream struct {
	order []*toolCallAccum
	byKey map[int]*toolCallAccum
}

func newToolCallStream() *toolCallStream {
	return &toolCallStream{byKey: make(map[int]*toolCallAccum)}
}

func (s *toolCallStream) add(pos int, frag streamToolCall) {
	key := pos
	if frag.Index != nil {
		key = *frag.Index
	}

	acc, known := s.byKey[key]
	if known && frag.ID != "" && acc.call.ID != "" && acc.call.ID != frag.ID {
		known = false
	}
	if !known {
		acc = &toolCallAccum{call: toolCall{Type: "function"}}
		s.order = append(s.order, acc)
		s.byKey[key] = acc
	}

	if frag.ID != "" {
		acc.call.ID = frag.ID
	}
	if frag.Type != "" {
		acc.call.Type = frag.Type
	}
	if frag.Function != nil {
		if frag.Function.Name != "" {
			acc.call.Function.Name = frag.Function.Name
		}
		acc.args.WriteString(frag.Function.Arguments)
	}
}

func (s *toolCallStream) calls() []toolCall {
	out := make([]toolCall, 0, len(s.order))
	for _, acc := range s.order {
		c := acc.call
		c.Function.Arguments = acc.args.String()
		out = append(out, c)
	}
	return out
}

func (c *openAICompatClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	model := c.model
	if req.Model != "" && req.Model != "local" {
		model = req.Model
	}

	msgs := buildChatMessages(req.Messages)
	tools := buildToolDefs(req.Tools)

	payload := chatRequest{
		Model:          model,
		Messages:       msgs,
		Stream:         true,
		StreamOptions:  &chatStreamOptions{IncludeUsage: true},
		ResponseFormat: buildResponseFormat(req.ResponseFormat),
		MaxTokens:      req.MaxTokens,
	}
	applyTools(&payload, tools)

	body, err := json.Marshal(payload)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	c.setAuthHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return domain.AgentResponse{}, httpError(resp, body)
	}

	var fullContent strings.Builder
	toolCalls := newToolCallStream()
	var streamUsage usage
	var finishReason string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil {
			streamUsage = *chunk.Usage
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		if fr := chunk.Choices[0].FinishReason; fr != "" {
			finishReason = fr
		}
		delta := chunk.Choices[0].Delta

		if delta.Content != "" {
			fullContent.WriteString(delta.Content)
			onToken(delta.Content)
		}
		for pos, frag := range delta.ToolCalls {
			toolCalls.add(pos, frag)
		}
	}

	if err := scanner.Err(); err != nil {
		return domain.AgentResponse{}, fmt.Errorf("stream read: %w", err)
	}

	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   fullContent.String(),
			ToolCalls: parseToolCalls(toolCalls.calls()),
		},
		Usage:      streamUsage.toDomain(),
		StopReason: domain.NormalizeStopReason(finishReason),
	}, nil
}

func (c *openAICompatClient) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp, respBody)
	}

	var modelsResp modelsResponse
	if err := json.Unmarshal(respBody, &modelsResp); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]string, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

const llmErrorBodyMax = 2000

type EmbeddingUnavailableError struct {
	StatusCode int
	Body       string
	Cause      error
}

func (e *EmbeddingUnavailableError) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("embeddings unreachable: %v", e.Cause)
	}

	return fmt.Sprintf("embeddings returned %d: %s", e.StatusCode, e.Body)
}

func (e *EmbeddingUnavailableError) Unwrap() error { return e.Cause }

var embeddingRejectionStatuses = map[int]bool{
	http.StatusUnauthorized:    true,
	http.StatusPaymentRequired: true,
	http.StatusForbidden:       true,
}

func newEmbeddingUnavailableError(statusCode int, body string) *EmbeddingUnavailableError {
	if !embeddingRejectionStatuses[statusCode] {
		return nil
	}
	return &EmbeddingUnavailableError{
		StatusCode: statusCode,
		Body:       domain.TruncateHead(body, llmErrorBodyMax),
	}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (c *openAICompatClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	embedModel := model
	if embedModel == "" {
		embedModel = c.model
	}

	payload := embedRequest{Model: embedModel, Input: input}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setAuthHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("http request: %w", err)
		}
		return nil, &EmbeddingUnavailableError{Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if rle := newRateLimitError("embeddings", resp, string(respBody)); rle != nil {
			return nil, rle
		}

		if eue := newEmbeddingUnavailableError(resp.StatusCode, string(respBody)); eue != nil {
			return nil, eue
		}
		return nil, fmt.Errorf("embeddings returned %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp embedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal embed response: %w", err)
	}

	if len(embedResp.Data) == 0 || len(embedResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embeddings returned empty data")
	}

	return embedResp.Data[0].Embedding, nil
}
