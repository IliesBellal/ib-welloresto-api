package ai

import "go.uber.org/zap"

// modelCostPer1KTokens holds estimated cost in EUR per 1 000 tokens, split as
// [input price, output price]. Update manually when provider pricing changes.
var modelCostPer1KTokens = map[string][2]float64{
	// Anthropic — https://www.anthropic.com/pricing
	"claude-haiku-4-5":           {0.00025, 0.00125},
	"claude-3-5-haiku-20241022":  {0.00080, 0.00400},
	"claude-3-5-sonnet-20241022": {0.00300, 0.01500},
	"claude-3-opus-20240229":     {0.01500, 0.07500},
	// OpenAI — https://openai.com/api/pricing/
	"gpt-4o":      {0.00250, 0.01000},
	"gpt-4o-mini": {0.00015, 0.00060},
}

// LogLLMCall records a structured log entry for every LLM call attempt.
// Call it after every Complete(), regardless of success or failure.
//
// Parameters:
//   - log       : zap logger (logger.FromContext(ctx) at the call site)
//   - task      : business task identifier ("menu_translation", "upsell", …)
//   - provider  : provider name returned by LLMProvider.Name()
//   - model     : exact model identifier used
//   - inputTokens / outputTokens : token counts from CompletionResponse
//   - latencyMs : round-trip latency from CompletionResponse
//   - cacheHit  : true when the response was served from the AI cache
//   - err       : non-nil when Complete() failed
func LogLLMCall(
	log *zap.Logger,
	task string,
	provider string,
	model string,
	inputTokens int,
	outputTokens int,
	latencyMs int64,
	cacheHit bool,
	err error,
) {
	estimatedCost := estimateCostEUR(model, inputTokens, outputTokens)

	fields := []zap.Field{
		zap.String("task", task),
		zap.String("provider", provider),
		zap.String("model", model),
		zap.Int("input_tokens", inputTokens),
		zap.Int("output_tokens", outputTokens),
		zap.Int64("latency_ms", latencyMs),
		zap.Bool("cache_hit", cacheHit),
		zap.Float64("estimated_cost_eur", estimatedCost),
	}

	if err != nil {
		log.Error("llm_call failed", append(fields, zap.Error(err))...)
		return
	}

	log.Info("llm_call succeeded", fields...)
}

// estimateCostEUR returns the estimated cost in EUR for a single LLM call.
// Returns 0 when the model is not in the pricing table.
func estimateCostEUR(model string, inputTokens, outputTokens int) float64 {
	prices, ok := modelCostPer1KTokens[model]
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1000)*prices[0] + (float64(outputTokens)/1000)*prices[1]
}
