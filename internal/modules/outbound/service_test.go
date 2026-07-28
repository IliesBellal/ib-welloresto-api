package outbound

import (
	"context"
	"errors"
	"testing"
)

type memoryRepo struct {
	inserted     []CreateParams
	statuses     map[string]string
	insertErr    error
	findErr      error
	updateErr    error
	updateCalls  int
	updatedValue string
}

func (m *memoryRepo) Insert(_ context.Context, params CreateParams) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserted = append(m.inserted, params)
	if m.statuses == nil {
		m.statuses = map[string]string{}
	}
	m.statuses[params.ProviderMessageID] = params.Status
	return nil
}

func (m *memoryRepo) FindStatusByProviderMessageID(_ context.Context, providerMessageID string) (string, bool, error) {
	if m.findErr != nil {
		return "", false, m.findErr
	}
	status, ok := m.statuses[providerMessageID]
	if !ok {
		return "", false, nil
	}
	return status, true, nil
}

func (m *memoryRepo) UpdateStatusByProviderMessageID(_ context.Context, providerMessageID, status string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updateCalls++
	m.updatedValue = status
	m.statuses[providerMessageID] = status
	return nil
}

func TestRecordOutboundMessage_CreatesSentRow(t *testing.T) {
	repo := &memoryRepo{}
	svc := NewService(repo, nil)

	err := svc.RecordOutboundMessage("email", "brevo", "msg-1", "booking", "book-1", "client@example.com")
	if err != nil {
		t.Fatalf("RecordOutboundMessage returned error: %v", err)
	}

	if len(repo.inserted) != 1 {
		t.Fatalf("expected 1 insert, got %d", len(repo.inserted))
	}
	got := repo.inserted[0]
	if got.Channel != ChannelEmail || got.Provider != "brevo" || got.ProviderMessageID != "msg-1" || got.Domain != "booking" || got.DomainRefID != "book-1" || got.Recipient != "client@example.com" || got.Status != StatusSent {
		t.Fatalf("unexpected inserted payload: %+v", got)
	}
}

func TestUpdateStatusByProviderMessageID_DeliveredThenOpenedNoRegression(t *testing.T) {
	repo := &memoryRepo{statuses: map[string]string{"msg-1": StatusDelivered}}
	svc := NewService(repo, nil)

	found, err := svc.UpdateStatusByProviderMessageID(context.Background(), "msg-1", StatusOpened)
	if err != nil {
		t.Fatalf("update delivered->opened error: %v", err)
	}
	if !found {
		t.Fatal("expected message to be found")
	}
	if repo.statuses["msg-1"] != StatusOpened {
		t.Fatalf("expected status opened, got %s", repo.statuses["msg-1"])
	}

	found, err = svc.UpdateStatusByProviderMessageID(context.Background(), "msg-1", StatusDelivered)
	if err != nil {
		t.Fatalf("update opened->delivered error: %v", err)
	}
	if !found {
		t.Fatal("expected message to be found")
	}
	if repo.statuses["msg-1"] != StatusOpened {
		t.Fatalf("expected no regression from opened, got %s", repo.statuses["msg-1"])
	}
}

func TestUpdateStatusByProviderMessageID_UnknownProviderMessageID(t *testing.T) {
	repo := &memoryRepo{statuses: map[string]string{}}
	svc := NewService(repo, nil)

	found, err := svc.UpdateStatusByProviderMessageID(context.Background(), "missing", StatusDelivered)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected not found")
	}
}

func TestRecordOutboundMessage_InsertFailure(t *testing.T) {
	repo := &memoryRepo{insertErr: errors.New("boom")}
	svc := NewService(repo, nil)

	err := svc.RecordOutboundMessage("email", "brevo", "msg-1", "booking", "book-1", "client@example.com")
	if err == nil {
		t.Fatal("expected an error")
	}
}
