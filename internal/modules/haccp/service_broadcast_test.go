package haccp

import (
	"testing"

	"welloresto-api/internal/modules/notification"
)

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

func TestBroadcastHACCPUpdated_PayloadContract(t *testing.T) {
	fake := &fakeBroadcaster{ret: true}
	svc := &Service{broadcaster: fake}

	svc.broadcastHACCPUpdated("merchant-42")

	if len(fake.calls) != 1 {
		t.Fatalf("BroadcastToMerchant appelé %d fois, attendu 1", len(fake.calls))
	}
	call := fake.calls[0]
	if call.merchantID != "merchant-42" {
		t.Errorf("merchantID = %q, attendu %q", call.merchantID, "merchant-42")
	}
	if got := call.payload["type"]; got != notification.WSEventHACCPUpdated {
		t.Errorf("type = %v, attendu %q", got, notification.WSEventHACCPUpdated)
	}
	if got := call.payload["merchant_id"]; got != "merchant-42" {
		t.Errorf("merchant_id = %v, attendu %q", got, "merchant-42")
	}
	// Notification sans état (décision D2) : aucune donnée métier au-delà du
	// triplet type/merchant_id ne doit transiter par le WebSocket.
	if len(call.payload) != 2 {
		t.Errorf("payload a %d clés, attendu 2 (type, merchant_id) — pas d'état métier", len(call.payload))
	}
}

// La diffusion est best-effort : un service câblé sans hub (tests, tâches
// batch) ne doit jamais paniquer.
func TestBroadcastHACCPUpdated_NilBroadcasterIsNoop(t *testing.T) {
	svc := &Service{broadcaster: nil}
	svc.broadcastHACCPUpdated("merchant-42")
}

func TestBroadcastHACCPUpdated_IgnoresNoListener(t *testing.T) {
	fake := &fakeBroadcaster{ret: false} // aucun device connecté
	svc := &Service{broadcaster: fake}

	svc.broadcastHACCPUpdated("merchant-42")

	if len(fake.calls) != 1 {
		t.Fatalf("BroadcastToMerchant appelé %d fois, attendu 1", len(fake.calls))
	}
}
