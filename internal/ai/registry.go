package ai

import "fmt"

// Registry resolves the correct LLMProvider for a given task name.
// It is built once at startup in SetupRoutes and injected into services that need it.
type Registry struct {
	providers map[string]LLMProvider
	tasks     map[string]TaskConfig
}

// NewRegistry builds a Registry from the given AIConfig and a map of already-instantiated
// providers. It validates that every task references a provider present in providers.
//
// providers is keyed by the provider name returned by LLMProvider.Name().
func NewRegistry(cfg AIConfig, providers map[string]LLMProvider) (*Registry, error) {
	r := &Registry{
		providers: providers,
		tasks:     cfg.Tasks,
	}
	if err := r.validateProviders(); err != nil {
		return nil, err
	}
	return r, nil
}

// GetProviderForTask returns the LLMProvider configured for the given task.
// Returns an explicit error when the task is unknown or its provider is not registered.
func (r *Registry) GetProviderForTask(task string) (LLMProvider, error) {
	taskCfg, ok := r.tasks[task]
	if !ok {
		return nil, fmt.Errorf("ai registry: no configuration found for task %q", task)
	}
	provider, ok := r.providers[taskCfg.Provider]
	if !ok {
		return nil, fmt.Errorf(
			"ai registry: provider %q (required by task %q) is not registered",
			taskCfg.Provider, task,
		)
	}
	return provider, nil
}

// TaskConfig returns the generation parameters configured for a task.
// The second return value is false when the task is not configured.
func (r *Registry) TaskConfig(task string) (TaskConfig, bool) {
	cfg, ok := r.tasks[task]
	return cfg, ok
}

// validateProviders ensures all task→provider references are satisfiable.
func (r *Registry) validateProviders() error {
	for task, taskCfg := range r.tasks {
		if _, ok := r.providers[taskCfg.Provider]; !ok {
			return fmt.Errorf(
				"ai registry: task %q references provider %q which is not registered",
				task, taskCfg.Provider,
			)
		}
	}
	return nil
}
