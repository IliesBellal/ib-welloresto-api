package stripe

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v78/webhook"
)

func signedHeader(body []byte, secret string, at time.Time) string {
	return webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: body, Secret: secret, Timestamp: at,
	}).Header
}

func TestMatchSignature(t *testing.T) {
	body := []byte(`{"id":"evt_1","type":"checkout.session.completed","account":"acct_1"}`)
	s := &StripeWebhookService{}
	s.SetSignatureSecrets("whsec_account", " whsec_connect ")

	if scope, _ := s.matchSignature(body, signedHeader(body, "whsec_account", time.Now())); scope != "account" {
		t.Errorf("secret plateforme : scope = %q, want account", scope)
	}
	if scope, _ := s.matchSignature(body, signedHeader(body, "whsec_connect", time.Now())); scope != "connect" {
		t.Errorf("secret Connect : scope = %q, want connect", scope)
	}
	if scope, failures := s.matchSignature(body, signedHeader(body, "whsec_inconnu", time.Now())); scope != "" || len(failures) != 2 {
		t.Errorf("secret inconnu : scope = %q, failures = %v", scope, failures)
	}
	if scope, _ := s.matchSignature([]byte(`{"id":"evt_forge"}`), signedHeader(body, "whsec_account", time.Now())); scope != "" {
		t.Errorf("corps modifié : scope = %q, want vide", scope)
	}
	if scope, _ := s.matchSignature(body, signedHeader(body, "whsec_account", time.Now().Add(-time.Hour))); scope != "" {
		t.Errorf("signature trop ancienne : scope = %q, want vide", scope)
	}
}

func TestSetSignatureSecrets_IgnoresEmpty(t *testing.T) {
	s := &StripeWebhookService{}
	s.SetSignatureSecrets("", "whsec_connect")
	if len(s.signatureSecrets) != 1 || s.signatureSecrets[0].scope != "connect" {
		t.Fatalf("signatureSecrets = %+v", s.signatureSecrets)
	}
}

// Mode observation : jamais de panique ni de blocage, avec ou sans secret.
func TestVerifySignature_NeverBlocks(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	h := http.Header{}
	(&StripeWebhookService{}).VerifySignature(context.Background(), h, body, StripeEvent{ID: "evt_1"})
	s := &StripeWebhookService{}
	s.SetSignatureSecrets("whsec_account", "")
	s.VerifySignature(context.Background(), h, body, StripeEvent{ID: "evt_1"})
	h.Set("Stripe-Signature", "t=1,v1=00")
	s.VerifySignature(context.Background(), h, body, StripeEvent{ID: "evt_1"})
}
