package analytics

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/modules/auth"
)

func TestResolveAccessibleMerchants_ReturnsExactlyTheTokenMerchant(t *testing.T) {
	user := &auth.UserLoginRow{MerchantID: "212"}
	got, err := ResolveAccessibleMerchants(context.Background(), user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "212" {
		t.Fatalf("expected exactly [\"212\"], got %+v", got)
	}
}

func TestValidateRequestedMerchants_EmptyRequestUsesFullAccessibleScope(t *testing.T) {
	accessible := []string{"212"}
	got, err := ValidateRequestedMerchants(nil, accessible)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "212" {
		t.Fatalf("expected the accessible scope back, got %+v", got)
	}
}

func TestValidateRequestedMerchants_SubsetOfAccessibleIsAccepted(t *testing.T) {
	accessible := []string{"212"}
	got, err := ValidateRequestedMerchants([]string{"212"}, accessible)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "212" {
		t.Fatalf("expected [\"212\"], got %+v", got)
	}
}

// TestValidateRequestedMerchants_OutsideScopeIs403 is the dedicated test
// PROMPT 03 §1.3 asks for: a client requesting an establishment outside its
// token's accessible scope must be rejected outright (the handler maps
// ErrMerchantNotAccessible to HTTP 403 — see handler.go), never silently
// narrowed to the accessible set.
func TestValidateRequestedMerchants_OutsideScopeIs403(t *testing.T) {
	accessible := []string{"212"}
	_, err := ValidateRequestedMerchants([]string{"999"}, accessible)
	if !errors.Is(err, ErrMerchantNotAccessible) {
		t.Fatalf("expected ErrMerchantNotAccessible, got %v", err)
	}
}

func TestValidateRequestedMerchants_PartiallyOutsideScopeIs403(t *testing.T) {
	accessible := []string{"212"}
	_, err := ValidateRequestedMerchants([]string{"212", "999"}, accessible)
	if !errors.Is(err, ErrMerchantNotAccessible) {
		t.Fatalf("expected ErrMerchantNotAccessible for a partially-outside-scope request, got %v", err)
	}
}
