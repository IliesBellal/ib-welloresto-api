package models

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Refus d'annulation de paiement (lot C conformité caisse) : code dédié dans
// data.status pour les nouvelles versions de la caisse, et message en
// français au premier niveau, que les versions actuelles affichent pour un
// code qu'elles ne connaissent pas.
func TestSendErrorJSON_PaymentCancelRefusals(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status string
	}{
		{ErrPaymentOrderClosed, "payment_order_closed"},
		{ErrPaymentRegisterClosed, "payment_register_closed"},
		{fmt.Errorf("wrapped: %w", ErrOrderPaymentRegisterClosed), "order_payment_register_closed"},
		{ErrReopenOrderSealed, "reopen_order_sealed"},
		{ErrReopenPaymentRegisterClosed, "reopen_payment_register_closed"},
	} {
		w := httptest.NewRecorder()
		SendErrorJSON(w, "order_life_cycle", "delete_payment", tc.err)
		if w.Code != http.StatusConflict {
			t.Errorf("%s: HTTP %d, want 409", tc.status, w.Code)
		}
		var body struct {
			ID      string            `json:"id"`
			Message string            `json:"message"`
			Data    map[string]string `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v", tc.status, err)
		}
		if body.ID != "order_life_cycle.delete_payment" || body.Data["status"] != tc.status {
			t.Errorf("%s: id %q, data %v", tc.status, body.ID, body.Data)
		}
		if body.Message == "" || body.Data["message"] != body.Message {
			t.Errorf("%s: top-level message %q, data.message %q", tc.status, body.Message, body.Data["message"])
		}
	}

	// Les autres erreurs gardent leur réponse : pas de message au premier niveau.
	w := httptest.NewRecorder()
	SendErrorJSON(w, "order_life_cycle", "deny_order", ErrOrderClosed)
	var other map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &other); err != nil {
		t.Fatal(err)
	}
	if _, ok := other["message"]; ok || len(other) != 2 {
		t.Errorf("unchanged errors must keep {id, data}, got %s", w.Body.String())
	}
}
