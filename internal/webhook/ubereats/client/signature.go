package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func VerifySignature(body []byte, headerSignature string, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(headerSignature))
}
