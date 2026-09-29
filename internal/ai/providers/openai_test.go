package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"welloresto-api/internal/ai"
)

func openAIReplyServer(t *testing.T, finishReason string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"` + finishReason + `"}],"usage":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenAIComplete_FinishReasonNormalised(t *testing.T) {
	cases := map[string]string{"stop": "end_turn", "length": ai.StopReasonMaxTokens}
	for finish, want := range cases {
		p := NewOpenAIProvider(ai.ProviderConfig{BaseURL: openAIReplyServer(t, finish).URL}, "")
		resp, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "x"})
		if err != nil {
			t.Fatalf("finish_reason %q: Complete: %v", finish, err)
		}
		if resp.StopReason != want {
			t.Errorf("finish_reason %q: StopReason = %q, want %q", finish, resp.StopReason, want)
		}
	}

	p := NewOpenAIProvider(ai.ProviderConfig{BaseURL: openAIReplyServer(t, "content_filter").URL}, "")
	if _, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "x"}); !errors.Is(err, ai.ErrRefused) {
		t.Errorf("content_filter: err = %v, want ai.ErrRefused", err)
	}
}

func TestOpenAIComplete_RejectsAnthropicOnlyFeatures(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer srv.Close()
	p := NewOpenAIProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	for name, req := range map[string]ai.CompletionRequest{
		"images":      {UserPrompt: "x", Images: []ai.Image{{MediaType: "image/jpeg", Data: []byte("x")}}},
		"json schema": {UserPrompt: "x", JSONSchema: json.RawMessage(`{"type":"object"}`)},
	} {
		if _, err := p.Complete(context.Background(), req); !errors.Is(err, ai.ErrUnsupported) {
			t.Errorf("%s: err = %v, want ai.ErrUnsupported", name, err)
		}
	}
	if calls != 0 {
		t.Errorf("OpenAI called %d times, want 0 (refused before sending)", calls)
	}
}

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
