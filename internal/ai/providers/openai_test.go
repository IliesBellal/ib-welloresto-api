package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"welloresto-api/internal/ai"
)

func TestOpenAIComplete_RequestModelOverridesDefault(t *testing.T) {
	var lastModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		lastModel = body.Model
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{}}`))
	}))
	defer srv.Close()
	p := NewOpenAIProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	if _, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "x"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if lastModel != defaultOpenAIModel {
		t.Errorf("model = %q, want provider default %q", lastModel, defaultOpenAIModel)
	}
	if _, err := p.Complete(context.Background(), ai.CompletionRequest{Model: "gpt-4o", UserPrompt: "x"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if lastModel != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", lastModel)
	}
}
