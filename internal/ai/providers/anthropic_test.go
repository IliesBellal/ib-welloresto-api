package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestAnthropicComplete_TextOnlyRequestKeepsStringContent(t *testing.T) {
	srv, body := anthropicTestServer(t, anthropicTextReply)
	p := NewAnthropicProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	if _, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "bonjour", MaxTokens: 10}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	msgs, _ := (*body)["messages"].([]any)
	first, _ := msgs[0].(map[string]any)
	if first["content"] != "bonjour" {
		t.Errorf("content = %#v, want the plain string sent before images existed", first["content"])
	}
}

func TestAnthropicComplete_ImagesBeforeTextAndSchema(t *testing.T) {
	srv, body := anthropicTestServer(t, anthropicTextReply)
	p := NewAnthropicProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	schema := json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`)
	req := ai.CompletionRequest{
		UserPrompt: "Lis cette carte",
		MaxTokens:  10,
		Images:     []ai.Image{{MediaType: "image/jpeg", Data: []byte("jpeg-bytes")}},
		JSONSchema: schema,
	}
	if _, err := p.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	msgs, _ := (*body)["messages"].([]any)
	first, _ := msgs[0].(map[string]any)
	blocks, _ := first["content"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("content blocks = %#v, want image then text", first["content"])
	}
	img, _ := blocks[0].(map[string]any)
	src, _ := img["source"].(map[string]any)
	if img["type"] != "image" || src["type"] != "base64" || src["media_type"] != "image/jpeg" ||
		src["data"] != base64.StdEncoding.EncodeToString([]byte("jpeg-bytes")) {
		t.Errorf("image block = %#v", img)
	}
	txt, _ := blocks[1].(map[string]any)
	if txt["type"] != "text" || txt["text"] != "Lis cette carte" {
		t.Errorf("text block = %#v", txt)
	}

	cfg, _ := (*body)["output_config"].(map[string]any)
	format, _ := cfg["format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("output_config.format = %#v, want json_schema", cfg["format"])
	}
	sentSchema, _ := json.Marshal(format["schema"])
	var want, got any
	_ = json.Unmarshal(schema, &want)
	_ = json.Unmarshal(sentSchema, &got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("schema = %s, want %s", sentSchema, schema)
	}
	if _, sent := cfg["effort"]; sent {
		t.Errorf("effort sent while empty: %#v", cfg)
	}
}

func TestAnthropicComplete_KeepsOnlyTextBlocks(t *testing.T) {
	// Réponse d'un modèle à réflexion toujours active : bloc de réflexion
	// (texte vide par défaut) avant la réponse.
	srv, _ := anthropicTestServer(t, `{"model":"claude-opus-5-5","stop_reason":"end_turn","content":[`+
		`{"type":"thinking","thinking":"","signature":"sig"},`+
		`{"type":"text","text":"{\"a\":"},{"type":"text","text":"1}"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	p := NewAnthropicProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	resp, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "x", MaxTokens: 10})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != `{"a":1}` {
		t.Errorf("Content = %q, want the concatenated text blocks", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
	}
}

func TestAnthropicComplete_MaxTokensIsReported(t *testing.T) {
	srv, _ := anthropicTestServer(t, `{"model":"m","stop_reason":"max_tokens","content":[{"type":"text","text":"{\"a\":"}],"usage":{}}`)
	p := NewAnthropicProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	resp, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "x", MaxTokens: 10})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.StopReason != ai.StopReasonMaxTokens {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, ai.StopReasonMaxTokens)
	}
}

func TestAnthropicComplete_RefusalIsErrRefused(t *testing.T) {
	srv, _ := anthropicTestServer(t, `{"model":"m","stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"no"},"content":[],"usage":{}}`)
	p := NewAnthropicProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	_, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "x", MaxTokens: 10})
	if !errors.Is(err, ai.ErrRefused) {
		t.Fatalf("err = %v, want ai.ErrRefused", err)
	}
}

func TestAnthropicComplete_NoTextIsAnError(t *testing.T) {
	srv, _ := anthropicTestServer(t, `{"model":"m","stop_reason":"end_turn","content":[{"type":"thinking","thinking":""}],"usage":{}}`)
	p := NewAnthropicProvider(ai.ProviderConfig{BaseURL: srv.URL}, "")

	if _, err := p.Complete(context.Background(), ai.CompletionRequest{UserPrompt: "x", MaxTokens: 10}); err == nil {
		t.Fatalf("Complete should fail when the response has no text block")
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
