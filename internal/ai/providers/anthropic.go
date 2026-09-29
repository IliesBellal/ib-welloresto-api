package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	timeout    time.Duration
	httpClient *http.Client
}

// NewAnthropicProvider creates a configured Anthropic provider.
//   - cfg.APIKey is required.
//   - cfg.BaseURL overrides the default endpoint when non-empty.
//   - cfg.Timeout overrides the default 30s timeout when non-zero; a request
//     can override it again with CompletionRequest.Timeout.
//   - model defaults to claude-haiku-4-5 when empty; a request can override
//     it with CompletionRequest.Model.
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
		timeout: timeout,
		// No client-level Timeout: the deadline is set per request on the
		// context (see Complete), so one task can wait longer than another.
		httpClient: &http.Client{},
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

	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	body := anthropicRequest{
		Model:     model,
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
	if req.Effort != "" {
		body.OutputConfig = &anthropicOutputConfig{Effort: req.Effort}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to marshal request: %w", err)
	}

	timeout := p.timeout
	if req.Timeout > 0 {
		timeout = req.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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

	if apiResp.StopReason == "refusal" {
		return nil, fmt.Errorf("anthropic: %w (category %q: %s)",
			ai.ErrRefused, apiResp.StopDetails.Category, apiResp.StopDetails.Explanation)
	}

	// Current models (thinking always on) put thinking blocks before the
	// answer: keep only the text blocks, in order.
	var text strings.Builder
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return nil, fmt.Errorf("anthropic: no text content in response (stop_reason %q)", apiResp.StopReason)
	}

	return &ai.CompletionResponse{
		Content:      stripMarkdownFences(text.String()),
		StopReason:   apiResp.StopReason,
		InputTokens:  apiResp.Usage.InputTokens,
		OutputTokens: apiResp.Usage.OutputTokens,
		Model:        apiResp.Model,
		LatencyMs:    latencyMs,
	}, nil
}

// stripMarkdownFences removes ``` code fences that Anthropic sometimes adds
// around JSON responses despite explicit instructions not to.
func stripMarkdownFences(s string) string {
	cleaned := strings.TrimSpace(s)
	if !strings.HasPrefix(cleaned, "```") {
		return cleaned
	}
	if idx := strings.Index(cleaned, "\n"); idx != -1 {
		cleaned = cleaned[idx+1:]
	}
	if idx := strings.LastIndex(cleaned, "```"); idx != -1 {
		cleaned = strings.TrimSpace(cleaned[:idx])
	}
	return cleaned
}

// ---- internal Anthropic API types ----

type anthropicRequest struct {
	Model        string                 `json:"model"`
	MaxTokens    int                    `json:"max_tokens"`
	System       string                 `json:"system,omitempty"`
	Messages     []anthropicMessage     `json:"messages"`
	Temperature  *float64               `json:"temperature,omitempty"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
}

type anthropicOutputConfig struct {
	Effort string `json:"effort,omitempty"`
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

	StopReason  string `json:"stop_reason"`
	StopDetails struct {
		Category    string `json:"category"`
		Explanation string `json:"explanation"`
	} `json:"stop_details"`

	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
