package stripeclient

import (
	"testing"

	"github.com/stripe/stripe-go/v84"
)

func actionStatus(s stripe.TerminalReaderActionStatus) *stripe.TerminalReaderActionStatus {
	return &s
}

func TestNormalizePaymentStatus(t *testing.T) {
	tests := []struct {
		name           string
		in             PaymentStatusInput
		wantStatus     string
		wantFailCode   string // "" means wantFailureNil
		wantFailureNil bool
	}{
		{
			name:       "rule1: local CAPTURED",
			in:         PaymentStatusInput{LocalStatus: "CAPTURED", PIStatus: stripe.PaymentIntentStatusRequiresPaymentMethod},
			wantStatus: "succeeded", wantFailureNil: true,
		},
		{
			name:       "rule1: local TO_REFUND",
			in:         PaymentStatusInput{LocalStatus: "TO_REFUND", PIStatus: stripe.PaymentIntentStatusCanceled},
			wantStatus: "succeeded", wantFailureNil: true,
		},
		{
			name:       "rule1: PI succeeded live even if local not yet updated",
			in:         PaymentStatusInput{LocalStatus: "REQUIRES_CONFIRMATION", PIStatus: stripe.PaymentIntentStatusSucceeded},
			wantStatus: "succeeded", wantFailureNil: true,
		},
		{
			name:       "rule2: processing",
			in:         PaymentStatusInput{PIStatus: stripe.PaymentIntentStatusProcessing},
			wantStatus: "processing", wantFailureNil: true,
		},
		{
			name:       "rule1: requires_capture (autorisé, capture différée) -> succeeded",
			in:         PaymentStatusInput{PIStatus: stripe.PaymentIntentStatusRequiresCapture},
			wantStatus: "succeeded", wantFailureNil: true,
		},
		{
			name:       "rule3: canceled",
			in:         PaymentStatusInput{PIStatus: stripe.PaymentIntentStatusCanceled},
			wantStatus: "canceled", wantFailureNil: true,
		},
		{
			name: "rule3 primes over rule4: canceled with in_progress action simultaneously",
			in: PaymentStatusInput{
				PIStatus:           stripe.PaymentIntentStatusCanceled,
				ReaderActionStatus: actionStatus(stripe.TerminalReaderActionStatusInProgress),
			},
			wantStatus: "canceled", wantFailureNil: true,
		},
		{
			name: "rule4: action in_progress -> waiting_for_card",
			in: PaymentStatusInput{
				PIStatus:            stripe.PaymentIntentStatusRequiresPaymentMethod,
				ReaderActionStatus:  actionStatus(stripe.TerminalReaderActionStatusInProgress),
				HasLastPaymentError: false,
			},
			wantStatus: "waiting_for_card", wantFailureNil: true,
		},
		{
			name: "rule4 primes over rule6: retry in progress despite a stale last_payment_error",
			in: PaymentStatusInput{
				PIStatus:             stripe.PaymentIntentStatusRequiresPaymentMethod,
				ReaderActionStatus:   actionStatus(stripe.TerminalReaderActionStatusInProgress),
				HasLastPaymentError:  true,
				LastPaymentErrorCode: "card_declined",
			},
			wantStatus: "waiting_for_card", wantFailureNil: true,
		},
		{
			name: "rule5: action succeeded but PI not yet succeeded -> processing",
			in: PaymentStatusInput{
				PIStatus:           stripe.PaymentIntentStatusRequiresConfirmation,
				ReaderActionStatus: actionStatus(stripe.TerminalReaderActionStatusSucceeded),
			},
			wantStatus: "processing", wantFailureNil: true,
		},
		{
			name: "rule6: action failed -> failed with action failure code",
			in: PaymentStatusInput{
				PIStatus:                stripe.PaymentIntentStatusRequiresPaymentMethod,
				ReaderActionStatus:      actionStatus(stripe.TerminalReaderActionStatusFailed),
				ReaderActionFailureCode: "terminal_reader_offline",
				HasLastPaymentError:     false,
			},
			wantStatus: "failed", wantFailCode: "terminal_reader_offline",
		},
		{
			name: "rule6: last_payment_error present without any reader action",
			in: PaymentStatusInput{
				PIStatus:             stripe.PaymentIntentStatusRequiresPaymentMethod,
				HasLastPaymentError:  true,
				LastPaymentErrorCode: "card_declined",
			},
			wantStatus: "failed", wantFailCode: "card_declined",
		},
		{
			name:       "rule7: requires_payment_method with nothing above -> reader_action_missing",
			in:         PaymentStatusInput{PIStatus: stripe.PaymentIntentStatusRequiresPaymentMethod},
			wantStatus: "failed", wantFailCode: "reader_action_missing",
		},
		{
			name:       "rule8: unexpected state without action -> processing, never failed",
			in:         PaymentStatusInput{PIStatus: stripe.PaymentIntentStatusRequiresConfirmation},
			wantStatus: "processing", wantFailureNil: true,
		},
		{
			name:       "rule8: requires_action without action -> processing",
			in:         PaymentStatusInput{PIStatus: stripe.PaymentIntentStatusRequiresAction},
			wantStatus: "processing", wantFailureNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatus, gotFailureCode := NormalizePaymentStatus(tt.in)
			if gotStatus != tt.wantStatus {
				t.Fatalf("status = %q, want %q", gotStatus, tt.wantStatus)
			}
			if tt.wantFailureNil {
				if gotFailureCode != nil {
					t.Fatalf("failureCode = %q, want nil", *gotFailureCode)
				}
				return
			}
			if gotFailureCode == nil {
				t.Fatalf("failureCode = nil, want %q", tt.wantFailCode)
			}
			if *gotFailureCode != tt.wantFailCode {
				t.Fatalf("failureCode = %q, want %q", *gotFailureCode, tt.wantFailCode)
			}
		})
	}
}

func TestFailureMessageFR(t *testing.T) {
	tests := []struct {
		code     string
		wantNote string // just checks it's non-empty and specific vs generic
	}{
		{code: string(stripe.ErrorCodeTerminalReaderBusy)},
		{code: string(stripe.ErrorCodeTerminalReaderOffline)},
		{code: string(stripe.ErrorCodeTerminalReaderTimeout)},
		{code: string(stripe.ErrorCodeTerminalReaderHardwareFault)},
		{code: string(stripe.ErrorCodeTerminalReaderInvalidLocationForPayment)},
		{code: "reader_action_missing"},
	}
	seen := map[string]bool{}
	for _, tt := range tests {
		msg := FailureMessageFR(tt.code)
		if msg == "" {
			t.Fatalf("FailureMessageFR(%q) returned empty", tt.code)
		}
		if seen[msg] {
			t.Fatalf("FailureMessageFR(%q) returned a message already used by another code — each mapped code should have a distinct message", tt.code)
		}
		seen[msg] = true
	}

	generic := FailureMessageFR("some_unmapped_decline_code")
	if generic == "" {
		t.Fatal("FailureMessageFR(unmapped) returned empty, want generic fallback")
	}
	if seen[generic] {
		t.Fatal("FailureMessageFR(unmapped) should return the generic fallback, distinct from mapped codes")
	}
}

func TestExtractCardPresentDetails(t *testing.T) {
	if got := ExtractCardPresentDetails(nil); got != nil {
		t.Fatalf("ExtractCardPresentDetails(nil) = %+v, want nil", got)
	}

	charge := &stripe.Charge{PaymentMethodDetails: &stripe.ChargePaymentMethodDetails{
		CardPresent: &stripe.ChargePaymentMethodDetailsCardPresent{
			Brand: stripe.PaymentMethodCardBrandVisa,
			Last4: "4242",
			Receipt: &stripe.ChargePaymentMethodDetailsCardPresentReceipt{
				ApplicationPreferredName: "CB",
				DedicatedFileName:        "A000000042",
				AuthorizationCode:        "123456",
			},
		},
	}}
	got := ExtractCardPresentDetails(charge)
	if got == nil {
		t.Fatal("ExtractCardPresentDetails returned nil for a populated card_present charge")
	}
	if got.Brand != string(stripe.PaymentMethodCardBrandVisa) || got.Last4 != "4242" ||
		got.ApplicationPreferredName != "CB" || got.DedicatedFileName != "A000000042" || got.AuthorizationCode != "123456" {
		t.Fatalf("ExtractCardPresentDetails = %+v, unexpected", got)
	}

	if got := ExtractCardPresentDetails(&stripe.Charge{PaymentMethodDetails: &stripe.ChargePaymentMethodDetails{}}); got != nil {
		t.Fatalf("ExtractCardPresentDetails(no card_present) = %+v, want nil", got)
	}
}
