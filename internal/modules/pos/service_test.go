package pos

import (
	"testing"

	"welloresto-api/internal/modules/notification"
)

// fakeBroadcaster capture les diffusions au lieu de les envoyer sur un hub.
type fakeBroadcaster struct {
	calls []broadcastCall
	ret   bool
}

type broadcastCall struct {
	merchantID string
	payload    map[string]interface{}
}

func (f *fakeBroadcaster) BroadcastToMerchant(merchantID string, payload map[string]interface{}) bool {
	f.calls = append(f.calls, broadcastCall{merchantID: merchantID, payload: payload})
	return f.ret
}

func TestBroadcastPOSStatus_PayloadContract(t *testing.T) {
	tests := []struct {
		name   string
		isOpen bool
	}{
		{name: "ouverture", isOpen: true},
		{name: "fermeture", isOpen: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeBroadcaster{ret: true}
			svc := &POSService{broadcaster: fake}

			svc.broadcastPOSStatus("merchant-42", tc.isOpen)

			if len(fake.calls) != 1 {
				t.Fatalf("BroadcastToMerchant appelé %d fois, attendu 1", len(fake.calls))
			}
			call := fake.calls[0]
			if call.merchantID != "merchant-42" {
				t.Errorf("merchantID = %q, attendu %q", call.merchantID, "merchant-42")
			}
			if got := call.payload["type"]; got != notification.WSEventPOSStatusChanged {
				t.Errorf("type = %v, attendu %q", got, notification.WSEventPOSStatusChanged)
			}
			if got := call.payload["merchant_id"]; got != "merchant-42" {
				t.Errorf("merchant_id = %v, attendu %q", got, "merchant-42")
			}
			// is_open doit rester un bool : le client Flutter le lit
			// directement sans reparsing (voir PushNotificationController).
			got, ok := call.payload["is_open"].(bool)
			if !ok {
				t.Fatalf("is_open = %T, attendu bool", call.payload["is_open"])
			}
			if got != tc.isOpen {
				t.Errorf("is_open = %v, attendu %v", got, tc.isOpen)
			}
		})
	}
}

// La diffusion est best-effort : ni un hub absent, ni l'absence de device
// connecté ne doivent faire paniquer le chemin d'écriture du statut.
func TestBroadcastPOSStatus_NilBroadcasterIsNoop(t *testing.T) {
	svc := &POSService{broadcaster: nil}
	svc.broadcastPOSStatus("merchant-42", true)
}

func TestBroadcastPOSStatus_IgnoresNoListener(t *testing.T) {
	fake := &fakeBroadcaster{ret: false} // aucun device connecté
	svc := &POSService{broadcaster: fake}

	svc.broadcastPOSStatus("merchant-42", false)

	if len(fake.calls) != 1 {
		t.Fatalf("BroadcastToMerchant appelé %d fois, attendu 1", len(fake.calls))
	}
}
