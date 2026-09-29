package ai

import (
	"context"
	"errors"
	"time"
)

// ErrRefused is returned (wrapped) by a provider when the model declined the
// request (Anthropic stop_reason "refusal", OpenAI finish_reason
// "content_filter"). Callers can tell it apart from a technical failure with
// errors.Is.
var ErrRefused = errors.New("ai: the model declined the request")

// StopReasonMaxTokens is CompletionResponse.StopReason when the output hit
// MaxTokens: Content is then truncated (and a JSON payload likely invalid).
const StopReasonMaxTokens = "max_tokens"

// LLMProvider is the interface every LLM backend must implement.
// Add new providers (OpenAI, Mistral, …) by creating a new struct in
// internal/ai/providers/ and registering it in the Registry.
type LLMProvider interface {
	// Name returns the canonical provider identifier (e.g. "anthropic", "openai").
	Name() string

	// Complete sends a completion request and returns the model's response.
	// The passed context must be respected for cancellation / deadline propagation.
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}

// CompletionRequest holds all parameters for a single LLM call.
type CompletionRequest struct {
	// Task identifies the business feature driving this call.
	// Expected values: "menu_translation", "upsell", "menu_ocr".
	// Used for metrics tagging — must be non-empty.
	Task string

	// Model is the exact model identifier sent to the provider. Empty means
	// the provider's built-in default. Filled from TaskConfig.Model by the
	// provider returned by Registry.GetProviderForTask when left empty.
	Model string

	// Effort is the Anthropic output_config.effort level ("low" … "max").
	// Empty means not sent (the model default applies). Ignored by OpenAI.
	// Filled from TaskConfig.Effort when left empty.
	Effort string

	// Timeout bounds the whole HTTP round-trip. Zero means the provider's
	// default (ProviderConfig.Timeout). Filled from TaskConfig.Timeout when
	// left empty. A shorter deadline already set on ctx still wins.
	Timeout time.Duration

	SystemPrompt string
	UserPrompt   string

	// Temperature controls randomness [0.0, 1.0]. 0 means use model default.
	Temperature float64

	// MaxTokens caps the response length. 0 means use model default.
	MaxTokens int

	// JSONMode instructs the provider to return pure JSON with no markdown wrapping.
	JSONMode bool
}

// CompletionResponse holds the result of a successful LLM call.
type CompletionResponse struct {
	Content string

	// StopReason is why generation stopped, normalised across providers:
	// "end_turn" on a normal finish, StopReasonMaxTokens when truncated.
	// A refusal is never a response: the provider returns ErrRefused.
	StopReason string

	InputTokens  int
	OutputTokens int

	// Model is the exact model identifier used by the provider.
	Model string

	// LatencyMs is the round-trip time of the HTTP call in milliseconds.
	LatencyMs int64
}
