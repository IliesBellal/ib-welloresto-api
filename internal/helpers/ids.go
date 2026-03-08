package helpers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GeneratePrefixedID generates a cryptographically secure ID in the format:
// "{prefix}-{20 random hex chars}" — total max length = len(prefix)+1+20.
// For prefix="user"     → "user-<20chars>"     = 25 chars  (fits VARCHAR(32))
// For prefix="merchant" → "merchant-<20chars>" = 29 chars  (fits VARCHAR(32))
func GeneratePrefixedID(prefix string) (string, error) {
	b := make([]byte, 10) // 10 bytes → 20 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generatePrefixedID: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}

// GenerateToken returns a cryptographically secure random hex token of the
// given byte length (e.g., 16 → 32-char token).
func GenerateToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generateToken: %w", err)
	}
	return hex.EncodeToString(b), nil
}
