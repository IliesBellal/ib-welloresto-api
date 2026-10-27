package brevo_mailer

import "strings"

// copyBCC reçoit, en copie cachée, tous les mails envoyés via Brevo.
// TEMPORAIRE : à retirer le 15 décembre 2026 (docs/REMINDERS.md,
// docs/copie-mails-sms.md).
const copyBCC = "iliesbellal@gmail.com"

// withCopy ajoute la copie cachée au payload Brevo, sauf si l'adresse est déjà
// le destinataire. Cachée (bcc) pour que le client ou le restaurateur ne voie
// pas l'adresse.
func withCopy(payload map[string]interface{}, to string) map[string]interface{} {
	if strings.EqualFold(strings.TrimSpace(to), copyBCC) {
		return payload
	}
	payload["bcc"] = []map[string]string{{"email": copyBCC}}
	return payload
}
