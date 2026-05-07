package ai

import (
	"fmt"
	"time"
)

// AIConfig holds the complete AI layer configuration.
// It is embedded in config.AppConfig under the field AI.
type AIConfig struct {
	// Providers maps a provider name (e.g. "anthropic") to its connection settings.
	Providers map[string]ProviderConfig

	// Tasks maps a business task name (e.g. "menu_translation") to its execution config.
	Tasks map[string]TaskConfig
}

// ProviderConfig holds credentials and connection settings for one LLM backend.
type ProviderConfig struct {
	// APIKey is the secret credential sent to the provider.
	APIKey string

	// BaseURL overrides the provider's default endpoint (useful for testing / proxies).
	BaseURL string

	// Timeout is the HTTP client timeout for this provider. Defaults to 30s when zero.
	Timeout time.Duration
}

// TaskConfig maps a business task to a provider + model + generation parameters.
type TaskConfig struct {
	// Provider must match a key in AIConfig.Providers.
	Provider string

	// Model is the exact model identifier sent to the provider API.
	// Defaults to the provider's built-in default when empty.
	Model string

	Temperature float64
	MaxTokens   int
}

// Validate checks that every task references a provider declared in Providers.
// Called by AppConfig.validate() at startup.
func (c AIConfig) Validate() error {
	for task, taskCfg := range c.Tasks {
		if _, ok := c.Providers[taskCfg.Provider]; !ok {
			return fmt.Errorf(
				"ai config: task %q references unknown provider %q — add it to AI_PROVIDERS",
				task, taskCfg.Provider,
			)
		}
	}
	return nil
}
