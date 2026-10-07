package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var knownGeminiModels = []string{
	"gemini-2.5-flash",
	"gemini-2.5-pro",
	"gemini-2.0-flash",
	"gemini-2.0-flash-lite",
	"gemini-1.5-pro",
	"gemini-1.5-flash",
}

type geminiVertexClient struct {
	client *genai.Client
	model  string
}

func NewGeminiVertexClient(project, location, model, apiKey string, _ time.Duration) (port.LLMClient, error) {
	var cfg *genai.ClientConfig
	if apiKey != "" {
		cfg = &genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		}
	} else {
		cfg = &genai.ClientConfig{
			Project:  project,
			Location: location,
			Backend:  genai.BackendVertexAI,
		}
	}
	client, err := genai.NewClient(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("create vertex ai client: %w", err)
	}
	return &geminiVertexClient{client: client, model: model}, nil
}

func (c *geminiVertexClient) modelID(req domain.AgentRequest) string {
	if req.Model != "" && req.Model != "local" {
		return req.Model
	}
	return c.model
}

func geminiInlineImageParts(images []domain.ToolResultImage) []*genai.Part {
	if len(images) == 0 {
		return nil
	}
	parts := make([]*genai.Part, 0, len(images))
	for _, img := range images {
		raw, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			continue
		}
		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{MIMEType: img.MediaType, Data: raw},
		})
	}
	return parts
}

func buildGeminiContents(messages []domain.Message) ([]*genai.Content, *genai.Content) {
	var contents []*genai.Content
	var systemParts []string
	seenTurn := false

	for i := 0; i < len(messages); {
		m := messages[i]
		switch m.Role {
		case domain.RoleSystem:
			if strings.TrimSpace(m.Content) != "" {
				if seenTurn {
					contents = append(contents, &genai.Content{
						Role:  genai.RoleUser,
						Parts: []*genai.Part{{Text: systemReminder(m.Content)}},
					})
				} else {
					systemParts = append(systemParts, m.Content)
				}
			}
			i++
		case domain.RoleUser:
			seenTurn = true
			parts := []*genai.Part{{Text: m.Content}}
			parts = append(parts, geminiInlineImageParts(m.Images)...)
			contents = append(contents, &genai.Content{
				Role:  genai.RoleUser,
				Parts: parts,
			})
			i++
		case domain.RoleAssistant:
			seenTurn = true
			var parts []*genai.Part
			if m.Content != "" {
				parts = append(parts, &genai.Part{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						ID:   tc.ID,
						Name: tc.Function.Name,
						Args: args,
					},
				})
			}
			if len(parts) > 0 {
				contents = append(contents, &genai.Content{
					Role:  genai.RoleModel,
					Parts: parts,
				})
			}
			i++
		case domain.RoleTool:
			seenTurn = true
			var parts []*genai.Part
			var carried []domain.ToolResultImage
			for i < len(messages) && messages[i].Role == domain.RoleTool {
				tm := messages[i]
				var response map[string]any
				if err := json.Unmarshal([]byte(tm.Content), &response); err != nil {
					response = map[string]any{"result": tm.Content}
				}
				if len(tm.Images) > 0 {
					if response == nil {
						response = map[string]any{}
					}
					response["images_note"] = strings.TrimSpace(carriedImagesNote(len(tm.Images)))
					carried = append(carried, tm.Images...)
				}
				parts = append(parts, &genai.Part{
					FunctionResponse: &genai.FunctionResponse{
						ID:       tm.ToolCallID,
						Name:     tm.Name,
						Response: response,
					},
				})
				i++
			}
			contents = append(contents, &genai.Content{
				Role:  genai.RoleUser,
				Parts: parts,
			})
			if imageParts := geminiInlineImageParts(carried); len(imageParts) > 0 {
				contents = append(contents, &genai.Content{
					Role: genai.RoleUser,
					Parts: append([]*genai.Part{{Text: toolImagePreamble(len(imageParts))}},
						imageParts...),
				})
			}
		default:
			i++
		}
	}

	if len(systemParts) == 0 {
		return contents, nil
	}
	return contents, &genai.Content{
		Parts: []*genai.Part{{Text: strings.Join(systemParts, "\n\n")}},
	}
}

func buildGeminiConfig(systemInstruction *genai.Content, tools []domain.ToolDefinition, respFormat *domain.ResponseFormat, maxOutputTokens int32) *genai.GenerateContentConfig {
	config := &genai.GenerateContentConfig{
		SystemInstruction: systemInstruction,
	}
	if maxOutputTokens > 0 {
		config.MaxOutputTokens = maxOutputTokens
	}
	if len(tools) > 0 {
		decls := make([]*genai.FunctionDeclaration, 0, len(tools))
		for _, t := range tools {
			decls = append(decls, &genai.FunctionDeclaration{
				Name:                 t.Function.Name,
				Description:          t.Function.Description,
				ParametersJsonSchema: t.Function.Parameters,
			})
		}
		config.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
	} else if respFormat != nil {

		config.ResponseMIMEType = "application/json"
		if respFormat.Schema != nil {
			config.ResponseJsonSchema = respFormat.Schema
		}
	}
	return config
}

func geminiToolCalls(fcs []*genai.FunctionCall) []domain.ToolCall {
	toolCalls := make([]domain.ToolCall, 0, len(fcs))
	for _, fc := range fcs {
		argsJSON, _ := json.Marshal(fc.Args)
		id := fc.ID
		if id == "" {
			id = fc.Name
		}
		toolCalls = append(toolCalls, domain.ToolCall{
			ID:   id,
			Type: "function",
			Function: domain.FunctionCall{
				Name:      fc.Name,
				Arguments: string(argsJSON),
			},
		})
	}
	if len(toolCalls) == 0 {
		return nil
	}
	return toolCalls
}

func geminiUsage(m *genai.GenerateContentResponseUsageMetadata) domain.Usage {
	return domain.Usage{
		PromptTokens:     int(m.PromptTokenCount),
		CompletionTokens: int(m.CandidatesTokenCount),
		TotalTokens:      int(m.TotalTokenCount),
		CacheReadTokens:  int(m.CachedContentTokenCount),
	}
}

func geminiStopReason(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0] == nil {
		return ""
	}
	return domain.NormalizeStopReason(string(resp.Candidates[0].FinishReason))
}

func parseGeminiResponse(resp *genai.GenerateContentResponse) domain.AgentResponse {
	toolCalls := geminiToolCalls(resp.FunctionCalls())

	var usage domain.Usage
	if m := resp.UsageMetadata; m != nil {
		usage = geminiUsage(m)
	}

	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   resp.Text(),
			ToolCalls: toolCalls,
		},
		Usage:      usage,
		StopReason: geminiStopReason(resp),
	}
}

func (c *geminiVertexClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	contents, sysInstr := buildGeminiContents(req.Messages)
	config := buildGeminiConfig(sysInstr, req.Tools, req.ResponseFormat, int32(req.MaxTokens))

	resp, err := c.client.Models.GenerateContent(ctx, c.modelID(req), contents, config)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("gemini generate content: %w", err)
	}

	return parseGeminiResponse(resp), nil
}

func (c *geminiVertexClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	contents, sysInstr := buildGeminiContents(req.Messages)
	config := buildGeminiConfig(sysInstr, req.Tools, req.ResponseFormat, int32(req.MaxTokens))

	var fullText strings.Builder
	var toolCalls []domain.ToolCall
	var finalUsage domain.Usage
	var stopReason string

	for resp, err := range c.client.Models.GenerateContentStream(ctx, c.modelID(req), contents, config) {
		if err != nil {
			return domain.AgentResponse{}, fmt.Errorf("gemini stream: %w", err)
		}

		if text := resp.Text(); text != "" {
			fullText.WriteString(text)
			onToken(text)
		}

		toolCalls = append(toolCalls, geminiToolCalls(resp.FunctionCalls())...)
		if m := resp.UsageMetadata; m != nil && m.TotalTokenCount > 0 {
			finalUsage = geminiUsage(m)
		}
		if sr := geminiStopReason(resp); sr != "" {
			stopReason = sr
		}
	}

	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   fullText.String(),
			ToolCalls: toolCalls,
		},
		Usage:      finalUsage,
		StopReason: stopReason,
	}, nil
}

func (c *geminiVertexClient) Models(_ context.Context) ([]string, error) {
	return knownGeminiModels, nil
}

func (c *geminiVertexClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	embedModel := strings.TrimPrefix(strings.TrimSpace(model), "models/")

	if embedModel == "" || !strings.Contains(embedModel, "embedding") || embedModel == "text-embedding-004" {
		embedModel = "gemini-embedding-001"
	}
	contents := []*genai.Content{{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: input}},
	}}
	resp, err := c.client.Models.EmbedContent(ctx, embedModel, contents, nil)
	if err != nil {
		return nil, geminiEmbedError(ctx, err)
	}
	if len(resp.Embeddings) == 0 || len(resp.Embeddings[0].Values) == 0 {
		return nil, fmt.Errorf("gemini embed returned empty result")
	}
	return resp.Embeddings[0].Values, nil
}

func geminiEmbedError(ctx context.Context, err error) error {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		body := strings.TrimSpace(apiErr.Message)
		if body == "" {
			body = apiErr.Status
		}

		if apiErr.Code == http.StatusTooManyRequests || apiErr.Code == http.StatusServiceUnavailable {
			return &RateLimitError{
				Endpoint:   "embeddings",
				StatusCode: apiErr.Code,
				RetryAfter: geminiRetryDelay(apiErr.Details),
				Body:       domain.TruncateHead(body, llmErrorBodyMax),
			}
		}
		if eue := newEmbeddingUnavailableError(apiErr.Code, body); eue != nil {
			return eue
		}
		return fmt.Errorf("gemini embed: %w", err)
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) && ctx.Err() == nil {
		return &EmbeddingUnavailableError{Cause: err}
	}
	return fmt.Errorf("gemini embed: %w", err)
}

func geminiRetryDelay(details []map[string]any) time.Duration {
	for _, detail := range details {
		delay, _ := detail["retryDelay"].(string)
		if d := parseResetHint(delay); d > 0 {
			return d
		}
	}
	return 0
}
