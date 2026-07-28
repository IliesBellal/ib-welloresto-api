package brevo_events

import (
	"context"
	"testing"
	"welloresto-api/internal/modules/outbound"
)

type statefulOutbound struct {
	statuses map[string]string
	updates  []struct {
		providerMessageID string
		status            string
	}
}

func (s *statefulOutbound) UpdateStatusByProviderMessageID(_ context.Context, providerMessageID, nextStatus string) (bool, error) {
	if s.statuses == nil {
		s.statuses = map[string]string{}
	}
	current, ok := s.statuses[providerMessageID]
	if !ok {
		return false, nil
	}
	if outbound.ShouldAdvanceStatus(current, nextStatus) {
		s.statuses[providerMessageID] = nextStatus
		s.updates = append(s.updates, struct {
			providerMessageID string
			status            string
		}{providerMessageID: providerMessageID, status: nextStatus})
	}
	return true, nil
}

type outboundSpy struct {
	calls []struct {
		providerMessageID string
		status            string
	}
	found bool
	err   error
}

func (s *outboundSpy) UpdateStatusByProviderMessageID(_ context.Context, providerMessageID, nextStatus string) (bool, error) {
	s.calls = append(s.calls, struct {
		providerMessageID string
		status            string
	}{providerMessageID: providerMessageID, status: nextStatus})
	return s.found, s.err
}

func TestProcessEvent_DeliveredMapsToOutboundStatus(t *testing.T) {
	spy := &outboundSpy{found: true}
	svc := NewService(spy, nil)

	err := svc.ProcessEvent(context.Background(), BrevoEventPayload{Event: "delivered", MessageID: "msg-123"})
	if err != nil {
		t.Fatalf("ProcessEvent returned error: %v", err)
	}
	if len(spy.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(spy.calls))
	}
	if spy.calls[0].providerMessageID != "msg-123" || spy.calls[0].status != outbound.StatusDelivered {
		t.Fatalf("unexpected call payload: %+v", spy.calls[0])
	}
}

func TestProcessEvent_OpenedAndUniqueOpenedMapToOpened(t *testing.T) {
	for _, event := range []string{"opened", "uniqueOpened"} {
		spy := &outboundSpy{found: true}
		svc := NewService(spy, nil)
		err := svc.ProcessEvent(context.Background(), BrevoEventPayload{Event: event, MessageID: "msg-1"})
		if err != nil {
			t.Fatalf("event %s returned error: %v", event, err)
		}
		if len(spy.calls) != 1 || spy.calls[0].status != outbound.StatusOpened {
			t.Fatalf("event %s did not map to opened: %+v", event, spy.calls)
		}
	}
}

func TestProcessEvent_UnknownProviderMessageIDReturnsNil(t *testing.T) {
	spy := &outboundSpy{found: false}
	svc := NewService(spy, nil)

	err := svc.ProcessEvent(context.Background(), BrevoEventPayload{Event: "delivered", MessageID: "missing-id"})
	if err != nil {
		t.Fatalf("expected nil error for unknown provider_message_id, got %v", err)
	}
	if len(spy.calls) != 1 {
		t.Fatalf("expected update attempt, got %d calls", len(spy.calls))
	}
}

func TestProcessEvent_OpenedAdvancesDeliveredWithoutRegression(t *testing.T) {
	state := &statefulOutbound{statuses: map[string]string{"msg-1": outbound.StatusDelivered}}
	svc := NewService(state, nil)

	if err := svc.ProcessEvent(context.Background(), BrevoEventPayload{Event: "opened", MessageID: "msg-1"}); err != nil {
		t.Fatalf("opened event failed: %v", err)
	}
	if got := state.statuses["msg-1"]; got != outbound.StatusOpened {
		t.Fatalf("expected opened after opened event, got %s", got)
	}

	if err := svc.ProcessEvent(context.Background(), BrevoEventPayload{Event: "delivered", MessageID: "msg-1"}); err != nil {
		t.Fatalf("late delivered event failed: %v", err)
	}
	if got := state.statuses["msg-1"]; got != outbound.StatusOpened {
		t.Fatalf("expected status to stay opened after late delivered, got %s", got)
	}
}
