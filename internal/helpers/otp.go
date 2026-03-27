package helpers

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// GenerateOTP génère un code numérique aléatoire à 6 chiffres
func GenerateOTP() (string, error) {
	// Limite max à 999999
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	// %06d force le remplissage avec des zéros au début si n < 100000
	return fmt.Sprintf("%06d", n.Int64()), nil
}
