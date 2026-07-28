package brevo_events

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

type noopUpdater struct{}

func (n *noopUpdater) UpdateStatusByProviderMessageID(_ interface{}, _ string, _ string) (bool, error) {
	return false, nil
}

func TestHandleWebhook_RejectsInvalidToken(t *testing.T) {
	h := NewHandler(NewService(&outboundSpy{found: true}, nil), "secret-1")
	req := httptest.NewRequest(http.MethodPost, "/webhooks/brevo/events?token=wrong", bytes.NewBufferString(`{"event":"delivered","message-id":"x"}`))
	w := httptest.NewRecorder()

	h.HandleWebhook(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleWebhook_AcceptsValidTokenAndReturns200(t *testing.T) {
	h := NewHandler(NewService(&outboundSpy{found: true}, nil), "secret-1")
	req := httptest.NewRequest(http.MethodPost, "/webhooks/brevo/events?token=secret-1", bytes.NewBufferString(`{"event":"delivered","message-id":"x"}`))
	w := httptest.NewRecorder()

	h.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWebhook_UnknownProviderMessageIDStillReturns200(t *testing.T) {
	h := NewHandler(NewService(&outboundSpy{found: false}, nil), "")
	req := httptest.NewRequest(http.MethodPost, "/webhooks/brevo/events", bytes.NewBufferString(`{"event":"delivered","message-id":"missing"}`))
	w := httptest.NewRecorder()

	h.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
