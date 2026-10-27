package brevo_sms

import (
	"log"
	"strings"
)

// copyPhone reçoit un second SMS pour chaque SMS envoyé via Brevo (l'API SMS n'a
// pas de copie cachée : c'est un envoi supplémentaire, non facturé pour le
// moment). TEMPORAIRE : à retirer le 15 décembre 2026 (docs/REMINDERS.md,
// docs/copie-mails-sms.md).
const copyPhone = "+33609217928"

// copyMessage préfixe le SMS copié du destinataire d'origine, pour savoir à qui
// il était adressé.
func copyMessage(phoneNumber, message string) string {
	return "[copie → " + phoneNumber + "] " + message
}

// isCopyPhone indique si le numéro est déjà celui de la copie (pas de doublon).
func isCopyPhone(phoneNumber string) bool {
	return strings.ReplaceAll(strings.TrimSpace(phoneNumber), " ", "") == copyPhone
}

// sendCopy envoie la copie d'un SMS déjà accepté par Brevo. Un échec est
// seulement loggué : il ne doit jamais faire échouer l'envoi réel.
func (b *BrevoSMS) sendCopy(senderID, phoneNumber, message string) {
	if isCopyPhone(phoneNumber) {
		return
	}
	if _, err := b.postSMS(senderID, copyPhone, copyMessage(phoneNumber, message)); err != nil {
		log.Printf("ERROR sending SMS copy via Brevo: %v", err)
	}
}
