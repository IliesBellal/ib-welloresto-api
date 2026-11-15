package scannorder

import (
	"time"

	"welloresto-api/internal/models"
)

// qrCodeValidity : durée de validité d'un QR code temporaire (table ou serveur).
const qrCodeValidity = 2 * time.Hour

// qrCodeExpired indique si le QR code est périmé. Seuls les QR liés à une table
// ou à un serveur expirent (qrcodes.creation_date + 2 h). Le QR principal de
// l'établissement — ni table ni serveur, celui de l'URL publique de la carte —
// est permanent : qrcodes.creation_date est renseignée à sa création
// (DEFAULT now()), et sans cette exception l'URL de la carte d'un
// établissement cesserait de fonctionner deux heures après sa création
// (docs/scannorder-carte-sans-stripe.md).
func qrCodeExpired(row *models.MerchantRow, now time.Time) bool {
	if row == nil || row.CreationDate == nil {
		return false
	}
	if row.LocationID == nil && row.UserID == nil {
		return false
	}
	return now.Sub(*row.CreationDate) > qrCodeValidity
}
