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
	defaultAnthropicModel   = "claude-haiku-4-5"
	defaultAnthropicBaseURL = "https://api.anthropic.com/v1/messages"
	anthropicVersion        = "2023-06-01"
	defaultTimeout          = 30 * time.Second

	// jsonModeInstruction is appended to the system prompt when JSONMode is true.
	// Anthropic does not expose a native JSON mode on all models, so we use an
	// explicit instruction instead.
	jsonModeInstruction = "\n\nIMPORTANT: Your response must be pure JSON only. " +
		"Do not wrap it in markdown code blocks. " +
		"Do not add any text before or after the JSON object."
)

// AnthropicProvider implements ai.LLMProvider for the Anthropic Messages API.
// https://docs.anthropic.com/en/api/messages
type AnthropicProvider struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewAnthropicProvider creates a configured Anthropic provider.
//   - cfg.APIKey is required.
//   - cfg.BaseURL overrides the default endpoint when non-empty.
//   - cfg.Timeout overrides the default 30s HTTP timeout when non-zero.
//   - model defaults to claude-haiku-4-5 when empty.
func NewAnthropicProvider(cfg ai.ProviderConfig, model string) *AnthropicProvider {
	timeout := defaultTimeout
	if cfg.Timeout > 0 {
		timeout = cfg.Timeout
	}

	baseURL := defaultAnthropicBaseURL
	if cfg.BaseURL != "" {
		baseURL = cfg.BaseURL
	}

	if model == "" {
		model = defaultAnthropicModel
	}

	return &AnthropicProvider{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns the canonical provider identifier.
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// Complete sends a request to the Anthropic Messages API and returns the completion.
// The ctx is forwarded to the HTTP request for cancellation / deadline support.
func (p *AnthropicProvider) Complete(ctx context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	systemPrompt := req.SystemPrompt
	if req.JSONMode {
		systemPrompt += jsonModeInstruction
	}

	body := anthropicRequest{
		Model:     p.model,
		MaxTokens: req.MaxTokens,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: req.UserPrompt},
		},
	}

	if req.Temperature > 0 {
		t := req.Temperature
		body.Temperature = &t
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to build HTTP request: %w", err)
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("content-type", "application/json")

	start := time.Now()
	resp, err := p.httpClient.Do(httpReq)
	latencyMs := time.Since(start).Milliseconds()

	if err != nil {
		return nil, fmt.Errorf("anthropic: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: unexpected status %d: %s", resp.StatusCode, string(rawBody))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(rawBody, &apiResp); err != nil {
		return nil, fmt.Errorf("anthropic: failed to parse response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("anthropic: empty content block in response")
	}

	return &ai.CompletionResponse{
		Content:      apiResp.Content[0].Text,
		InputTokens:  apiResp.Usage.InputTokens,
		OutputTokens: apiResp.Usage.OutputTokens,
		Model:        apiResp.Model,
		LatencyMs:    latencyMs,
	}, nil
}

// ---- internal Anthropic API types ----

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature *float64           `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Role  string `json:"role"`
	Model string `json:"model"`

	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`

	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
