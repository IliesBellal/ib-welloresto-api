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
				// Empty default = the provider's own default model
				// (claude-haiku-4-5 / gpt-4o-mini): a provider switch alone
				// must not send an Anthropic model name to OpenAI.
				Model:       os.Getenv("AI_TASK_MENU_TRANSLATION_MODEL"),
				Temperature: parseFloat64(os.Getenv("AI_TASK_MENU_TRANSLATION_TEMPERATURE"), 0.3),
				MaxTokens:   parseInt(os.Getenv("AI_TASK_MENU_TRANSLATION_MAX_TOKENS"), 4096),
				Enabled:     true,
			},
			"upsell": {
				Provider:    getEnv("AI_TASK_UPSELL_PROVIDER", "anthropic"),
				Model:       os.Getenv("AI_TASK_UPSELL_MODEL"), // empty = provider default, see above
				Temperature: parseFloat64(os.Getenv("AI_TASK_UPSELL_TEMPERATURE"), 0.5),
				MaxTokens:   parseInt(os.Getenv("AI_TASK_UPSELL_MAX_TOKENS"), 1024),
				// Kill-switch: AI_TASK_UPSELL_ENABLED=false skips the LLM call
				// entirely on cache/pattern miss and goes straight to the
				// featured-products fallback (see upsell.Service), e.g. during
				// an LLM provider billing outage.
				Enabled: parseBool(os.Getenv("AI_TASK_UPSELL_ENABLED"), true),
			},
			// Lecture de carte par photo (import produits, porte IA) : un appel
			// par photo, image + JSON contraint par schéma. Voir
			// docs/cadrage-import-carte-photo-ia.md § 5.3.
			"menu_ocr": {
				Provider:  getEnv("AI_TASK_MENU_OCR_PROVIDER", "anthropic"),
				Model:     getEnv("AI_TASK_MENU_OCR_MODEL", "claude-opus-5-5"),
				Effort:    parseEffort(getEnv("AI_TASK_MENU_OCR_EFFORT", "medium")),
				MaxTokens: parseInt(os.Getenv("AI_TASK_MENU_OCR_MAX_TOKENS"), 16000),
				Timeout:   parseDuration(os.Getenv("AI_TASK_MENU_OCR_TIMEOUT_MS"), 3*time.Minute),
				// Fermé par défaut : ouvert d'abord sur staging pour les tests.
				Enabled: parseBool(os.Getenv("AI_TASK_MENU_OCR_ENABLED"), false),
			},
		},
	}
}

// parseEffort maps "none" to an empty effort (parameter not sent), so a model
// that rejects effort (e.g. claude-haiku-4-5) can be selected through env vars
// alone even where an empty variable cannot be set.
func parseEffort(raw string) string {
	if raw == "none" {
		return ""
	}
	return raw
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

// parseBool parses a bool from a string.
// Returns fallback when the value is empty or invalid.
func parseBool(raw string, fallback bool) bool {
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}
