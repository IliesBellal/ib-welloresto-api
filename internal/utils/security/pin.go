package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
)

// HashPIN returns HMAC-SHA256(pin, pepper) as a lowercase hex string.
// The result is deterministic, allowing indexed WHERE pin_hash = ? lookups.
// pepper must be a non-empty secret loaded at startup (see PIN_PEPPER env var).
func HashPIN(pin, pepper string) string {
	h := hmac.New(sha256.New, []byte(pepper))
	h.Write([]byte(pin))
	return fmt.Sprintf("%x", h.Sum(nil))
}
