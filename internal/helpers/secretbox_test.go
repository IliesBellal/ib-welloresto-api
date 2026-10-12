package helpers

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func testKey(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

func TestSecretBox_RoundTripAndTamper(t *testing.T) {
	t.Setenv("TEST_SECRETBOX_KEY", testKey(1))
	box := NewSecretBox("TEST_SECRETBOX_KEY")
	if !box.Available() {
		t.Fatal("clé valide jugée indisponible")
	}
	a, err := box.Encrypt([]byte("jeton-secret"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := box.Encrypt([]byte("jeton-secret"))
	if bytes.Equal(a, b) || bytes.Contains(a, []byte("jeton-secret")) {
		t.Error("chiffrement déterministe ou en clair")
	}
	if got, err := box.Decrypt(a); err != nil || string(got) != "jeton-secret" {
		t.Fatalf("déchiffrement : %q %v", got, err)
	}
	a[len(a)-1] ^= 1
	if _, err := box.Decrypt(a); err == nil {
		t.Error("message altéré accepté")
	}
	if _, err := box.Decrypt([]byte("court")); !errors.Is(err, ErrCiphertextTooShort) {
		t.Errorf("message trop court : %v", err)
	}
}

func TestSecretBox_DifferentKeysDoNotOpenEachOther(t *testing.T) {
	t.Setenv("TEST_SECRETBOX_A", testKey(1))
	t.Setenv("TEST_SECRETBOX_B", testKey(2))
	sealed, _ := NewSecretBox("TEST_SECRETBOX_A").Encrypt([]byte("x"))
	if _, err := NewSecretBox("TEST_SECRETBOX_B").Decrypt(sealed); err == nil {
		t.Error("une clé différente a ouvert le message")
	}
}

func TestSecretBox_MissingOrInvalidKey(t *testing.T) {
	t.Setenv("TEST_SECRETBOX_MISSING", "")
	missing := NewSecretBox("TEST_SECRETBOX_MISSING")
	if missing.Available() {
		t.Error("clé absente jugée disponible")
	}
	if _, err := missing.Encrypt([]byte("x")); !errors.Is(err, ErrEncryptionKeyMissing) {
		t.Errorf("clé absente : %v", err)
	}
	t.Setenv("TEST_SECRETBOX_SHORT", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 16))))
	if _, err := NewSecretBox("TEST_SECRETBOX_SHORT").Encrypt([]byte("x")); !errors.Is(err, ErrEncryptionKeyInvalid) {
		t.Errorf("clé trop courte : %v", err)
	}
}
