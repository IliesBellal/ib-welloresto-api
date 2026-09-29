package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"welloresto-api/internal/ai"
)

// anthropicTestServer répond avec reply et mémorise le dernier corps de
// requête reçu, décodé en map pour vérifier les champs réellement envoyés.
func anthropicTestServer(t *testing.T, reply string) (*httptest.Server, *map[string]any) {
	t.Helper()
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		lastBody = map[string]any{}
		if err := json.Unmarshal(raw, &lastBody); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &lastBody
}

const anthropicTextReply = `{"model":"claude-haiku-4-5","stop_reason":"end_turn","content":[{"type":"text","text":"{\"ok\":true}"}],"usage":{"input_tokens":10,"output_tokens":5}}`

func TestAnthropicComplete_RequestModelOverridesDefault(t *testing.T) {
	srv, body := anthropicTestServer(t, anthropicTextReply)
	p := NewAnthropicProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	if _, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "x", MaxTokens: 10}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := (*body)["model"]; got != defaultAnthropicModel {
		t.Errorf("model = %v, want provider default %q", got, defaultAnthropicModel)
	}

	if _, err := p.Complete(context.Background(), ai.CompletionRequest{Model: "claude-opus-5-5", UserPrompt: "x", MaxTokens: 10}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := (*body)["model"]; got != "claude-opus-5-5" {
		t.Errorf("model = %v, want claude-opus-5-5", got)
	}
}

func TestAnthropicComplete_EffortAndTemperatureOnlyWhenSet(t *testing.T) {
	srv, body := anthropicTestServer(t, anthropicTextReply)
	p := NewAnthropicProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	// Requête d'une tâche existante (traduction, upsell) : rien de nouveau
	// n'est envoyé, le corps reste celui d'avant.
	if _, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "x", MaxTokens: 10}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, sent := (*body)["output_config"]; sent {
		t.Errorf("output_config sent without effort: %v", (*body)["output_config"])
	}
	if _, sent := (*body)["temperature"]; sent {
		t.Errorf("temperature sent while zero")
	}

	if _, err := p.Complete(context.Background(), ai.CompletionRequest{Effort: "medium", UserPrompt: "x", MaxTokens: 10}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	cfg, _ := (*body)["output_config"].(map[string]any)
	if cfg["effort"] != "medium" {
		t.Errorf("output_config = %v, want effort medium", (*body)["output_config"])
	}
}

func TestAnthropicComplete_RequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	p := NewAnthropicProvider(ai.ProviderConfig{BaseURL: srv.URL, Timeout: time.Minute}, "")

	start := time.Now()
	_, err := p.Complete(context.Background(), ai.CompletionRequest{Timeout: 50 * time.Millisecond, UserPrompt: "x", MaxTokens: 10})
	if err == nil {
		t.Fatalf("Complete should time out")
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Errorf("Complete took %v, want the 50ms request timeout to apply", elapsed)
	}
}
