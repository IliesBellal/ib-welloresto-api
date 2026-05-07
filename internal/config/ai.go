package config

import (
	"os"
	"strconv"
	"time"

	"welloresto-api/internal/ai"
)

// loadAIConfig assembles the AI layer configuration from environment variables.
// All fields have sensible defaults so the app starts without AI env vars set —
// AI features will simply return errors at runtime if provider API keys are missing.
func loadAIConfig() ai.AIConfig {
	return ai.AIConfig{
		Providers: map[string]ai.ProviderConfig{
			"anthropic": {
				APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
				BaseURL: os.Getenv("ANTHROPIC_BASE_URL"), // optional override
				Timeout: parseDuration(os.Getenv("ANTHROPIC_TIMEOUT_MS"), 30*time.Second),
			},
			"openai": {
				APIKey:  os.Getenv("OPENAI_API_KEY"),
				BaseURL: os.Getenv("OPENAI_BASE_URL"), // optional override
				Timeout: parseDuration(os.Getenv("OPENAI_TIMEOUT_MS"), 30*time.Second),
			},
		},
		Tasks: map[string]ai.TaskConfig{
			"menu_translation": {
				Provider:    getEnv("AI_TASK_MENU_TRANSLATION_PROVIDER", "anthropic"),
				Model:       getEnv("AI_TASK_MENU_TRANSLATION_MODEL", "claude-haiku-4-5"),
				Temperature: parseFloat64(os.Getenv("AI_TASK_MENU_TRANSLATION_TEMPERATURE"), 0.3),
				MaxTokens:   parseInt(os.Getenv("AI_TASK_MENU_TRANSLATION_MAX_TOKENS"), 4096),
			},
			"upsell": {
				Provider:    getEnv("AI_TASK_UPSELL_PROVIDER", "anthropic"),
				Model:       getEnv("AI_TASK_UPSELL_MODEL", "claude-haiku-4-5"),
				Temperature: parseFloat64(os.Getenv("AI_TASK_UPSELL_TEMPERATURE"), 0.5),
				MaxTokens:   parseInt(os.Getenv("AI_TASK_UPSELL_MAX_TOKENS"), 1024),
			},
		},
	}
}

// parseDuration parses a duration given as milliseconds (e.g. "5000" → 5s).
// Returns fallback when the value is empty or invalid.
func parseDuration(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

// parseFloat64 parses a float64 from a string.
// Returns fallback when the value is empty or invalid.
func parseFloat64(raw string, fallback float64) float64 {
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return v
}

// parseInt parses an int from a string.
// Returns fallback when the value is empty or invalid.
func parseInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
