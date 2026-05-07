package ai

import "context"

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
	// Expected values: "menu_translation", "upsell".
	// Used for metrics tagging — must be non-empty.
	Task string

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

	InputTokens  int
	OutputTokens int

	// Model is the exact model identifier used by the provider.
	Model string

	// LatencyMs is the round-trip time of the HTTP call in milliseconds.
	LatencyMs int64
}
