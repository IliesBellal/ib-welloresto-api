package scannorder

import (
	"testing"
	"time"

	"welloresto-api/internal/models"
)

func TestQRCodeExpired(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	old := now.Add(-3 * time.Hour)
	recent := now.Add(-time.Hour)
	table, server := "12", "u-1"

	cases := []struct {
		name string
		row  *models.MerchantRow
		want bool
	}{
		{"QR principal créé il y a 3 h : permanent", &models.MerchantRow{CreationDate: &old}, false},
		{"QR principal sans date", &models.MerchantRow{}, false},
		{"QR de table périmé", &models.MerchantRow{CreationDate: &old, LocationID: &table}, true},
		{"QR de serveur périmé", &models.MerchantRow{CreationDate: &old, UserID: &server}, true},
		{"QR de table encore valide", &models.MerchantRow{CreationDate: &recent, LocationID: &table}, false},
		{"QR de table sans date", &models.MerchantRow{LocationID: &table}, false},
		{"marchand inconnu", nil, false},
	}
	for _, c := range cases {
		if got := qrCodeExpired(c.row, now); got != c.want {
			t.Errorf("%s : qrCodeExpired = %v, want %v", c.name, got, c.want)
		}
	}
}
