package discounts

import (
	"strings"
	"testing"
)

func validRequest() *CreateDiscountRequest {
	return &CreateDiscountRequest{
		DiscountName:  "Lundi pizza",
		DiscountDesc:  "Toutes les pizzas 31 cm à 9 €",
		DiscountValue: 10,
		DiscountUnit:  DiscountUnitPercentage,
	}
}

func TestCreateDiscountRequest_Validate_MultiByteDescUnderRuneLimit(t *testing.T) {
	// 100 runes of 2-byte UTF-8 content (200 bytes): valid per the varchar(100)
	// column (Postgres counts characters, not bytes) but previously rejected
	// because Validate used len() (byte count).
	req := validRequest()
	req.DiscountDesc = strings.Repeat("é", 100)
	if err := req.Validate(); err != nil {
		t.Fatalf("expected a 100-rune description to be valid, got: %v", err)
	}
}

func TestCreateDiscountRequest_Validate_DescOver100Runes(t *testing.T) {
	req := validRequest()
	req.DiscountDesc = strings.Repeat("é", 101)
	if err := req.Validate(); err != ErrInvalidDiscountDesc {
		t.Fatalf("expected ErrInvalidDiscountDesc, got: %v", err)
	}
}

func TestCreateDiscountRequest_Validate_NameOver50Runes(t *testing.T) {
	req := validRequest()
	req.DiscountName = strings.Repeat("à", 51)
	if err := req.Validate(); err != ErrInvalidDiscountName {
		t.Fatalf("expected ErrInvalidDiscountName, got: %v", err)
	}
}

func TestCreateDiscountRequest_Validate_EmptyDesc(t *testing.T) {
	req := validRequest()
	req.DiscountDesc = ""
	if err := req.Validate(); err != ErrInvalidDiscountDesc {
		t.Fatalf("expected ErrInvalidDiscountDesc, got: %v", err)
	}
}
