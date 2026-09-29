package ai

import (
	"context"
	"testing"
	"time"
)

type recordingProvider struct {
	last CompletionRequest
}

func (p *recordingProvider) Name() string { return "anthropic" }

func (p *recordingProvider) Complete(_ context.Context, req CompletionRequest) (*CompletionResponse, error) {
	p.last = req
	return &CompletionResponse{}, nil
}

func newTestRegistry(t *testing.T, cfg TaskConfig) (*Registry, *recordingProvider) {
	t.Helper()
	inner := &recordingProvider{}
	cfg.Provider = inner.Name()
	cfg.Enabled = true
	reg, err := NewRegistry(AIConfig{Tasks: map[string]TaskConfig{"menu_ocr": cfg}}, map[string]LLMProvider{inner.Name(): inner})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return reg, inner
}

func TestGetProviderForTask_AppliesTaskSettings(t *testing.T) {
	reg, inner := newTestRegistry(t, TaskConfig{Model: "claude-opus-5-5", Effort: "medium", Timeout: 3 * time.Minute})

	provider, err := reg.GetProviderForTask("menu_ocr")
	if err != nil {
		t.Fatalf("GetProviderForTask: %v", err)
	}
	if provider.Name() != "anthropic" {
		t.Errorf("Name() = %q, want the wrapped provider's name", provider.Name())
	}
	if _, err := provider.Complete(context.Background(), CompletionRequest{Task: "menu_ocr"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if inner.last.Model != "claude-opus-5-5" || inner.last.Effort != "medium" || inner.last.Timeout != 3*time.Minute {
		t.Errorf("request = model %q effort %q timeout %v, want task settings", inner.last.Model, inner.last.Effort, inner.last.Timeout)
	}
}

func TestGetProviderForTask_ExplicitRequestValuesWin(t *testing.T) {
	reg, inner := newTestRegistry(t, TaskConfig{Model: "claude-opus-5-5", Effort: "medium", Timeout: 3 * time.Minute})

	provider, _ := reg.GetProviderForTask("menu_ocr")
	_, _ = provider.Complete(context.Background(), CompletionRequest{Model: "claude-sonnet-5-5", Effort: "low", Timeout: time.Second})
	if inner.last.Model != "claude-sonnet-5-5" || inner.last.Effort != "low" || inner.last.Timeout != time.Second {
		t.Errorf("request = model %q effort %q timeout %v, want explicit values kept", inner.last.Model, inner.last.Effort, inner.last.Timeout)
	}
}

func TestGetProviderForTask_EmptyTaskSettingsLeaveProviderDefaults(t *testing.T) {
	reg, inner := newTestRegistry(t, TaskConfig{})

	provider, _ := reg.GetProviderForTask("menu_ocr")
	_, _ = provider.Complete(context.Background(), CompletionRequest{})
	if inner.last.Model != "" || inner.last.Effort != "" || inner.last.Timeout != 0 {
		t.Errorf("request = model %q effort %q timeout %v, want all empty", inner.last.Model, inner.last.Effort, inner.last.Timeout)
	}
}

func TestAIConfigValidate_RejectsUnknownEffort(t *testing.T) {
	cfg := AIConfig{
		Providers: map[string]ProviderConfig{"anthropic": {}},
		Tasks:     map[string]TaskConfig{"menu_ocr": {Provider: "anthropic", Effort: "moyen"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("Validate() should reject effort %q", "moyen")
	}
	cfg.Tasks["menu_ocr"] = TaskConfig{Provider: "anthropic", Effort: "medium"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil for effort medium", err)
	}
}
