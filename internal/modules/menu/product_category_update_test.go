package menu

import (
	"errors"
	"testing"

	"welloresto-api/internal/models"
)

func strPtr(s string) *string { return &s }

func TestNormalizeProductCategoryUpdate(t *testing.T) {
	tests := []struct {
		name      string
		in        UpdateProductCategoryPayload
		wantName  *string
		wantColor *string
		wantErr   bool
	}{
		{name: "nom seul (ancien client)", in: UpdateProductCategoryPayload{Name: strPtr("  Entrées ")}, wantName: strPtr("Entrées")},
		{name: "couleur seule", in: UpdateProductCategoryPayload{BgColor: strPtr("#FF8800")}, wantColor: strPtr("#ff8800")},
		{name: "couleur avec alpha refusée", in: UpdateProductCategoryPayload{BgColor: strPtr("#FF880080")}, wantErr: true},
		{name: "nom et couleur", in: UpdateProductCategoryPayload{Name: strPtr("Plats"), BgColor: strPtr("#3b82f6")}, wantName: strPtr("Plats"), wantColor: strPtr("#3b82f6")},
		{name: "payload vide", in: UpdateProductCategoryPayload{}, wantErr: true},
		{name: "nom vide", in: UpdateProductCategoryPayload{Name: strPtr("   ")}, wantErr: true},
		{name: "couleur sans dièse", in: UpdateProductCategoryPayload{BgColor: strPtr("ff8800")}, wantErr: true},
		{name: "couleur courte", in: UpdateProductCategoryPayload{BgColor: strPtr("#f80")}, wantErr: true},
		{name: "couleur non hexadécimale", in: UpdateProductCategoryPayload{BgColor: strPtr("#gg8800")}, wantErr: true},
		{name: "couleur vide", in: UpdateProductCategoryPayload{BgColor: strPtr("")}, wantErr: true},
		{name: "couleur injectée", in: UpdateProductCategoryPayload{BgColor: strPtr("#ffffff'; DROP TABLE productcateg;--")}, wantErr: true},
		{name: "nom valide mais couleur invalide", in: UpdateProductCategoryPayload{Name: strPtr("Plats"), BgColor: strPtr("rouge")}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, color, err := normalizeProductCategoryUpdate(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("attendu une erreur, obtenu name=%v color=%v", name, color)
				}
				if !errors.Is(err, models.ErrInvalidInput) {
					t.Fatalf("l'erreur doit envelopper ErrInvalidInput (→ HTTP 400), obtenu %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("erreur inattendue : %v", err)
			}
			if !equalStrPtr(name, tt.wantName) {
				t.Errorf("name = %v, attendu %v", deref(name), deref(tt.wantName))
			}
			if !equalStrPtr(color, tt.wantColor) {
				t.Errorf("bgColor = %v, attendu %v", deref(color), deref(tt.wantColor))
			}
		})
	}
}

func equalStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
