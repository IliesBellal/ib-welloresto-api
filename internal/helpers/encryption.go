package helpers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"sync"
)

// EncryptionKeyEnvVar — clé AES-256 (32 octets, encodée base64) utilisée pour
// chiffrer les secrets devant rester déchiffrables en base (ex: PIN admin
// Kiosk consultable depuis le POS). Génération : `openssl rand -base64 32`.
// Voir docs/KIOSK_DECISIONS.md pour la justification du choix chiffrement
// réversible vs hash.
const EncryptionKeyEnvVar = "KIOSK_PIN_ENCRYPTION_KEY"

var (
	ErrEncryptionKeyMissing = errors.New("encryption key not configured")
	ErrEncryptionKeyInvalid = errors.New("encryption key must be 32 bytes (AES-256) base64-encoded")
	ErrCiphertextTooShort   = errors.New("ciphertext too short to contain a nonce")
)

var (
	encryptionKeyOnce sync.Once
	encryptionKey     []byte
	encryptionKeyErr  error
)

// loadEncryptionKey lit et valide EncryptionKeyEnvVar une seule fois
// (sync.Once) — les appels suivants réutilisent le résultat mis en cache,
// y compris l'erreur (un serveur démarré sans clé configurée échoue toujours
// de la même façon, pas seulement à la première requête).
func loadEncryptionKey() ([]byte, error) {
	encryptionKeyOnce.Do(func() {
		raw := os.Getenv(EncryptionKeyEnvVar)
		if raw == "" {
			encryptionKeyErr = ErrEncryptionKeyMissing
			return
		}
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			encryptionKeyErr = ErrEncryptionKeyInvalid
			return
		}
		encryptionKey = key
	})
	return encryptionKey, encryptionKeyErr
}

// Encrypt chiffre plaintext avec AES-256-GCM. Le nonce (12 octets, généré
// aléatoirement à chaque appel) est préfixé au ciphertext retourné — Decrypt
// l'extrait avant de déchiffrer, pas besoin de le stocker séparément.
func Encrypt(plaintext string) ([]byte, error) {
	key, err := loadEncryptionKey()
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt inverse Encrypt — ciphertext doit porter le nonce en préfixe.
func Decrypt(ciphertext []byte) (string, error) {
	key, err := loadEncryptionKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", ErrCiphertextTooShort
	}
	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
