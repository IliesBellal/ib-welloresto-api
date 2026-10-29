// Package deliveroosig vérifie la signature des webhooks Deliveroo
// (commandes et menu), d'après api-docs.deliveroo.com/docs/securing-webhooks :
// HMAC-SHA256, clé = secret webhook fourni par Deliveroo, sur le GUID de
// l'en-tête X-Deliveroo-Sequence-Guid, un séparateur, puis le corps brut ;
// résultat en hexadécimal dans X-Deliveroo-Hmac-Sha256. Séparateur " " pour
// les webhooks actuels, " \n " pour les anciens événements POS
// (new_order/cancel_order).
package deliveroosig

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"welloresto-api/internal/logger"
)

const (
	headerGUID      = "X-Deliveroo-Sequence-Guid"
	headerSignature = "X-Deliveroo-Hmac-Sha256"
)

var separators = []struct{ name, value string }{
	{"space", " "},
	{"legacy_newline", " \n "},
}

// Verify renvoie le nom du séparateur dont la signature concorde
// ("space" ou "legacy_newline"), ou "" si aucun. Comparaison en temps constant.
func Verify(body []byte, guid, signature, secret string) string {
	if secret == "" || guid == "" || signature == "" {
		return ""
	}
	got := []byte(strings.ToLower(strings.TrimSpace(signature)))
	for _, sep := range separators {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(guid))
		mac.Write([]byte(sep.value))
		mac.Write(body)
		if hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), got) {
			return sep.name
		}
	}
	return ""
}

// Observe contrôle la signature en mode observation (« soft run »), comme le
// webhook Uber Eats : le résultat est journalisé — erreur si elle manque ou ne
// concorde pas, information si elle est vérifiée, avec le séparateur qui a
// concordé — et le webhook est TOUJOURS traité. Le rejet ne sera activé
// qu'après une période sans aucune erreur en production.
func Observe(ctx context.Context, source string, headers http.Header, body []byte, secret string) {
	log := logger.FromContext(ctx)
	guid := headers.Get(headerGUID)
	sig := headers.Get(headerSignature)
	fields := []zap.Field{
		zap.String("webhook", source),
		zap.Int("body_bytes", len(body)),
		zap.Bool("guid_present", guid != ""),
		zap.Bool("signature_present", sig != ""),
	}
	switch {
	case secret == "":
		log.Error("[DELIVEROO] webhook signature not checked: DELIVEROO_WEBHOOK_SECRET is empty (soft run, webhook processed)", fields...)
	case guid == "" || sig == "":
		log.Error("[DELIVEROO] webhook signature missing: no X-Deliveroo-Sequence-Guid or X-Deliveroo-Hmac-Sha256 header (soft run, webhook processed)", fields...)
	default:
		if sep := Verify(body, guid, sig, secret); sep != "" {
			log.Info("[DELIVEROO] webhook signature verified", append(fields, zap.String("separator", sep))...)
		} else {
			log.Error("[DELIVEROO] webhook signature mismatch (soft run, webhook processed)", fields...)
		}
	}
}
