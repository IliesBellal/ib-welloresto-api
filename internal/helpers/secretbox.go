package helpers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"os"
	"sync"
)

// SecretBox chiffre des secrets en AES-256-GCM avec une clé lue dans une
// variable d'environnement choisie par l'appelant (base64, 32 octets). Même
// format que Encrypt/Decrypt (nonce de 12 octets en préfixe), mais une clé par
// usage : la compromission de l'une ne donne pas accès aux secrets de l'autre.
type SecretBox struct {
	envVar string
	once   sync.Once
	key    []byte
	err    error
}

// NewSecretBox crée une boîte dont la clé est lue, à la première utilisation,
// dans envVar. Une clé absente ou invalide fait échouer chaque appel (jamais
// de repli sur une clé par défaut).
func NewSecretBox(envVar string) *SecretBox {
	return &SecretBox{envVar: envVar}
}

func (b *SecretBox) load() ([]byte, error) {
	b.once.Do(func() {
		raw := os.Getenv(b.envVar)
		if raw == "" {
			b.err = ErrEncryptionKeyMissing
			return
		}
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			b.err = ErrEncryptionKeyInvalid
			return
		}
		b.key = key
	})
	return b.key, b.err
}

func (b *SecretBox) gcm() (cipher.AEAD, error) {
	key, err := b.load()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Available indique si la clé est configurée et valide.
func (b *SecretBox) Available() bool {
	_, err := b.load()
	return err == nil
}

// Encrypt chiffre plaintext ; le nonce aléatoire est préfixé au résultat.
func (b *SecretBox) Encrypt(plaintext []byte) ([]byte, error) {
	gcm, err := b.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt inverse Encrypt.
func (b *SecretBox) Decrypt(ciphertext []byte) ([]byte, error) {
	gcm, err := b.gcm()
	if err != nil {
		return nil, err
	}
	n := gcm.NonceSize()
	if len(ciphertext) < n {
		return nil, ErrCiphertextTooShort
	}
	return gcm.Open(nil, ciphertext[:n], ciphertext[n:], nil)
}
