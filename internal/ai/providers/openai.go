package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"welloresto-api/internal/ai"
)

const (
	defaultOpenAIModel   = "gpt-4o-mini"
	defaultOpenAIBaseURL = "https://api.openai.com/v1/chat/completions"
	openAIDefaultTimeout = 30 * time.Second

	openAIJSONModeInstruction = "\n\nIMPORTANT: Your response must be pure JSON only. " +
		"Do not wrap it in markdown code blocks. " +
		"Do not add any text before or after the JSON object."
)

// OpenAIProvider implements ai.LLMProvider for OpenAI Chat Completions API.
// https://platform.openai.com/docs/api-reference/chat/create
type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewOpenAIProvider creates a configured OpenAI provider.
//   - cfg.APIKey is required.
//   - cfg.BaseURL overrides the default endpoint when non-empty.
//   - cfg.Timeout overrides the default 30s HTTP timeout when non-zero.
//   - model defaults to gpt-4o-mini when empty.
func NewOpenAIProvider(cfg ai.ProviderConfig, model string) *OpenAIProvider {
	timeout := openAIDefaultTimeout
	if cfg.Timeout > 0 {
		timeout = cfg.Timeout
	}

	baseURL := defaultOpenAIBaseURL
	if cfg.BaseURL != "" {
		baseURL = cfg.BaseURL
	}

	if model == "" {
		model = defaultOpenAIModel
	}

	return &OpenAIProvider{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns the canonical provider identifier.
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Complete sends a request to the OpenAI Chat Completions API and returns the completion.
// The ctx is forwarded to the HTTP request for cancellation / deadline support.
func (p *OpenAIProvider) Complete(ctx context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	systemPrompt := req.SystemPrompt
	if req.JSONMode {
		systemPrompt += openAIJSONModeInstruction
	}

	body := openAIRequest{
		Model: p.model,
		Messages: []openAIMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: req.UserPrompt},
		},
	}

	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		t := req.Temperature
		body.Temperature = &t
	}
	if req.JSONMode {
		body.ResponseFormat = &openAIResponseFormat{Type: "json_object"}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("openai: failed to build HTTP request: %w", err)
	}
	httpReq.Header.Set("authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("content-type", "application/json")

	start := time.Now()
	resp, err := p.httpClient.Do(httpReq)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return nil, fmt.Errorf("openai: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: unexpected status %d: %s", resp.StatusCode, string(rawBody))
	}

	var apiResp openAIResponse
	if err := json.Unmarshal(rawBody, &apiResp); err != nil {
		return nil, fmt.Errorf("openai: failed to parse response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("openai: empty choices in response")
	}

	return &ai.CompletionResponse{
		Content:      apiResp.Choices[0].Message.Content,
		InputTokens:  apiResp.Usage.PromptTokens,
		OutputTokens: apiResp.Usage.CompletionTokens,
		Model:        apiResp.Model,
		LatencyMs:    latencyMs,
	}, nil
}

// ---- internal OpenAI API types ----

type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	Temperature    *float64              `json:"temperature,omitempty"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type string `json:"type"`
}

type openAIResponse struct {
	ID    string `json:"id"`
	Model string `json:"model"`

	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`

	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}
