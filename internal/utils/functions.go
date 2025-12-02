package utils

import (
	"crypto/rand"
	"math/big"
)

var charset = []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// GenerateRandomString returns a cryptographically secure random string.
func GenerateRandomString(length int) string {
	result := make([]rune, length)

	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic(err)
		}
		result[i] = charset[n.Int64()]
	}

	return string(result)
}
