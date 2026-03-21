package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"os"
)

func SignHash(dataHash string) string {
	key := []byte(os.Getenv("FISCAL_SIGNING_KEY"))
	h := hmac.New(sha256.New, key)
	h.Write([]byte(dataHash))
	return fmt.Sprintf("%x", h.Sum(nil))
}
