package merchantsms

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"welloresto-api/internal/infrastructure/sms"
)

type recordedCost struct {
	merchantID string
	count      int
	unitPrice  float64
}

type fakeRepo struct {
	mu           sync.Mutex
	settings     *MarketingSettings
	settingsErr  error
	unitPrice    float64
	unitPriceErr error
	recorded     []recordedCost
	trackingRef  *OrderTrackingRef
	trackingErr  error
}

const testPublicID = "order--ffe6c970-0701-4dd2-88ce-4e61a0da1d86"

func (f *fakeRepo) GetOrderTrackingRef(ctx context.Context, merchantID, orderID string) (*OrderTrackingRef, error) {
	if f.trackingErr != nil {
		return nil, f.trackingErr
	}
	if f.trackingRef != nil {
		return f.trackingRef, nil
	}
	return &OrderTrackingRef{PublicID: testPublicID, OrderNum: "17"}, nil
}

func (f *fakeRepo) GetMarketingSettings(ctx context.Context, merchantID string) (*MarketingSettings, error) {
	return f.settings, f.settingsErr
}

func (f *fakeRepo) GetSMSUnitPrice(ctx context.Context, merchantID string) (float64, error) {
	return f.unitPrice, f.unitPriceErr
}

func (f *fakeRepo) RecordSMSCost(ctx context.Context, merchantID string, count int, unitPrice float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, recordedCost{merchantID, count, unitPrice})
	return nil
}

func (f *fakeRepo) records() []recordedCost {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedCost(nil), f.recorded...)
}

type sentSMS struct{ sender, phone, message string }

type fakeSender struct {
	mu   sync.Mutex
	err  error
	sent []sentSMS
}

func (f *fakeSender) SendSMS(senderID, phoneNumber, message string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	f.sent = append(f.sent, sentSMS{senderID, phoneNumber, message})
	return "brevo-msg-1", nil
}

func TestSend_CountsOneSMSAtMerchantUnitPrice(t *testing.T) {
	repo := &fakeRepo{unitPrice: 9}
	svc := NewService(repo, &fakeSender{}, nil)

	messageID, err := svc.Send(context.Background(), "merchant_1", "Wello Resto", "+33612345678", "hello")
	if err != nil || messageID != "brevo-msg-1" {
		t.Fatalf("Send() = (%q, %v)", messageID, err)
	}
	if got := repo.records(); len(got) != 1 || got[0] != (recordedCost{"merchant_1", 1, 9}) {
		t.Fatalf("recorded = %+v, want one SMS for merchant_1 at 9", got)
	}
}

func TestSend_FailedSendIsNotCounted(t *testing.T) {
	repo := &fakeRepo{unitPrice: 7}
	svc := NewService(repo, &fakeSender{err: errors.New("brevo SMS API error (status 400)")}, nil)

	if _, err := svc.Send(context.Background(), "merchant_1", "Wello Resto", "+33612345678", "hello"); err == nil {
		t.Fatalf("Send() error = nil, want the Brevo error")
	}
	if got := repo.records(); len(got) != 0 {
		t.Fatalf("recorded = %+v, want nothing for an SMS that was not sent", got)
	}
}

func TestSend_UnitPriceUnavailableFallsBackToDefault(t *testing.T) {
	repo := &fakeRepo{unitPriceErr: errors.New("db down")}
	svc := NewService(repo, &fakeSender{}, nil)

	if _, err := svc.Send(context.Background(), "merchant_1", "Wello Resto", "+33612345678", "hello"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := repo.records(); len(got) != 1 || got[0].unitPrice != defaultSMSUnitPrice {
		t.Fatalf("recorded = %+v, want one SMS at the default price", got)
	}
}

func TestSendAsync_CountsAndCallsOnSentAfterRequestContextIsCanceled(t *testing.T) {
	repo := &fakeRepo{unitPrice: 7}
	svc := NewService(repo, &fakeSender{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	svc.SendAsync(ctx, "merchant_1", "Wello Resto", "+33612345678", "hello", func(messageID string) { done <- messageID })
	cancel()

	select {
	case messageID := <-done:
		if messageID != "brevo-msg-1" {
			t.Fatalf("onSent messageID = %q", messageID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("onSent was not called")
	}
	if got := repo.records(); len(got) != 1 {
		t.Fatalf("recorded = %+v, want 1 SMS", got)
	}
}

func TestSendOrderConfirmationSMS_IsCountedForTheMerchant(t *testing.T) {
	repo := &fakeRepo{unitPrice: 7}
	sender := &fakeSender{}
	svc := NewService(repo, sender, nil)

	svc.SendOrderConfirmationSMS(context.Background(), "merchant_1", "+33612345678", sms.OrderConfirmationSMSData{OrderID: "42", MerchantName: "Brasserie"})

	deadline := time.Now().Add(2 * time.Second)
	for len(repo.records()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := repo.records(); len(got) != 1 || got[0].merchantID != "merchant_1" {
		t.Fatalf("recorded = %+v, want 1 SMS for merchant_1", got)
	}
}

func TestSendOrderTrackingSMS(t *testing.T) {
	enabled := func() *MarketingSettings {
		return &MarketingSettings{SMSEnabled: true, TrackingTemplate: "Commande #{order_id} : {tracking_url}", QRCode: "qr1", SMSUnitPrice: 7}
	}

	t.Run("envoyé via Brevo au format international et compté", func(t *testing.T) {
		settings := enabled()
		settings.SMSSenderName = "Brasserie"
		repo := &fakeRepo{settings: settings, unitPrice: 7}
		sender := &fakeSender{}
		svc := NewService(repo, sender, nil)

		if err := svc.SendOrderTrackingSMS(context.Background(), "merchant_1", "42", "06 12 34 56 78"); err != nil {
			t.Fatalf("SendOrderTrackingSMS() error = %v", err)
		}
		// Lien vers /order/{id public}, numéro de retrait dans {order_id} :
		// l'id interne "42" n'apparaît nulle part.
		want := sentSMS{"Brasserie", "+33612345678", "Commande #17 : https://scannorder.welloresto.fr/restaurant/qr1/order/" + testPublicID}
		if len(sender.sent) != 1 || sender.sent[0] != want {
			t.Fatalf("sent = %+v, want %+v", sender.sent, want)
		}
		if got := repo.records(); len(got) != 1 {
			t.Fatalf("recorded = %+v, want 1 SMS", got)
		}
	})

	t.Run("base SCANNORDER_BASE_URL configurée", func(t *testing.T) {
		sender := &fakeSender{}
		svc := NewService(&fakeRepo{settings: enabled()}, sender, nil)
		svc.SetScanNOrderBaseURL("https://scannorder-staging.example/")

		if err := svc.SendOrderTrackingSMS(context.Background(), "merchant_1", "42", "+33612345678"); err != nil {
			t.Fatalf("SendOrderTrackingSMS() error = %v", err)
		}
		want := "Commande #17 : https://scannorder-staging.example/restaurant/qr1/order/" + testPublicID
		if len(sender.sent) != 1 || sender.sent[0].message != want {
			t.Fatalf("sent = %+v, want message %q", sender.sent, want)
		}
	})

	t.Run("commande sans id public : rien envoyé", func(t *testing.T) {
		repo := &fakeRepo{settings: enabled(), trackingRef: &OrderTrackingRef{OrderNum: "17"}}
		sender := &fakeSender{}
		svc := NewService(repo, sender, nil)

		if err := svc.SendOrderTrackingSMS(context.Background(), "merchant_1", "42", "+33612345678"); err == nil {
			t.Fatalf("SendOrderTrackingSMS() error = nil, want an error")
		}
		if len(sender.sent) != 0 || len(repo.records()) != 0 {
			t.Fatalf("sent = %+v / recorded = %+v, want nothing", sender.sent, repo.records())
		}
	})

	t.Run("nom d'expéditeur trop long pour Brevo : expéditeur par défaut", func(t *testing.T) {
		settings := enabled()
		settings.SMSSenderName = "La Brasserie du Midi"
		sender := &fakeSender{}
		svc := NewService(&fakeRepo{settings: settings}, sender, nil)

		if err := svc.SendOrderTrackingSMS(context.Background(), "merchant_1", "42", "+33612345678"); err != nil {
			t.Fatalf("SendOrderTrackingSMS() error = %v", err)
		}
		if len(sender.sent) != 1 || sender.sent[0].sender != defaultSenderName {
			t.Fatalf("sent = %+v, want sender %q", sender.sent, defaultSenderName)
		}
	})

	t.Run("refus de Brevo : erreur remontée, rien compté", func(t *testing.T) {
		repo := &fakeRepo{settings: enabled()}
		svc := NewService(repo, &fakeSender{err: errors.New("status 400")}, nil)

		if err := svc.SendOrderTrackingSMS(context.Background(), "merchant_1", "42", "+33612345678"); err == nil {
			t.Fatalf("SendOrderTrackingSMS() error = nil, want the Brevo error")
		}
		if got := repo.records(); len(got) != 0 {
			t.Fatalf("recorded = %+v, want nothing", got)
		}
	})

	t.Run("SMS désactivé : ni envoi ni comptage", func(t *testing.T) {
		settings := enabled()
		settings.SMSEnabled = false
		repo := &fakeRepo{settings: settings}
		sender := &fakeSender{}
		svc := NewService(repo, sender, nil)

		if err := svc.SendOrderTrackingSMS(context.Background(), "merchant_1", "42", "+33612345678"); err != nil {
			t.Fatalf("SendOrderTrackingSMS() error = %v", err)
		}
		if len(sender.sent) != 0 || len(repo.records()) != 0 {
			t.Fatalf("sent = %+v / recorded = %+v, want nothing", sender.sent, repo.records())
		}
	})
}
