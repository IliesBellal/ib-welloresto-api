package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifySignature vérifie l'en-tête X-Uber-Signature d'un webhook Uber Eats :
// HMAC-SHA256 du corps brut, clé = client secret de l'application, en
// hexadécimal (casse ignorée). Comparaison en temps constant.
func VerifySignature(body []byte, headerSignature string, secret string) bool {
	if secret == "" || headerSignature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(strings.ToLower(strings.TrimSpace(headerSignature))))
}
