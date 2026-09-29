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

	// Effort is the Anthropic effort level ("low", "medium", "high", "xhigh",
	// "max"). Empty means not sent — required for models that reject the
	// parameter (e.g. claude-haiku-4-5).
	Effort string

	// Timeout overrides the provider's HTTP timeout for this task only.
	// Zero keeps the provider default.
	Timeout time.Duration

	Temperature float64
	MaxTokens   int

	// Enabled gates whether GetProviderForTask will hand out a provider for
	// this task at all. False makes the registry return ErrTaskDisabled
	// immediately, without checking provider registration — a runtime
	// kill-switch (e.g. AI_TASK_UPSELL_ENABLED=false) distinct from a
	// misconfiguration, so callers can log/react differently.
	Enabled bool
}

// validEfforts lists the effort levels accepted by the Anthropic API.
var validEfforts = map[string]bool{"": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// Validate checks that every task references a provider declared in Providers
// and uses a known effort level. Called by AppConfig.validate() at startup.
func (c AIConfig) Validate() error {
	for task, taskCfg := range c.Tasks {
		if _, ok := c.Providers[taskCfg.Provider]; !ok {
			return fmt.Errorf(
				"ai config: task %q references unknown provider %q — add it to AI_PROVIDERS",
				task, taskCfg.Provider,
			)
		}
		if !validEfforts[taskCfg.Effort] {
			return fmt.Errorf(
				"ai config: task %q has unknown effort %q (want low, medium, high, xhigh or max)",
				task, taskCfg.Effort,
			)
		}
	}
	return nil
}
