package order_life_cycle

import (
	"errors"
	"testing"

	"welloresto-api/internal/models"
)

func TestNormalizeRemakeReason(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name    string
		reason  *string
		want    *string
		wantErr bool
	}{
		{name: "absent", reason: nil, want: nil},
		{name: "vide", reason: strPtr("  "), want: nil},
		{name: "connu", reason: strPtr("DROPPED"), want: strPtr("DROPPED")},
		{name: "casse et espaces", reason: strPtr(" customer_complaint "), want: strPtr("CUSTOMER_COMPLAINT")},
		{name: "inconnu", reason: strPtr("BURNT"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeRemakeReason(tt.reason)
			if tt.wantErr {
				if !errors.Is(err, models.ErrInvalidInput) {
					t.Fatalf("err = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
