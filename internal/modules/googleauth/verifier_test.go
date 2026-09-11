package googleauth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// LOT A Semaine 2, Chantier 7b : Verifier.Verify exercised against a local
// JWKS server signing with a test-generated RSA key — no network call to
// Google, no dependency on Google's real private key. certsURL is
// injectable for exactly this reason (see verifier.go).
func newTestVerifierServer(t *testing.T, kid string, pub *rsa.PublicKey) *httptest.Server {
	t.Helper()
	nBytes := pub.N.Bytes()
	eBytes := []byte{byte(pub.E >> 16), byte(pub.E >> 8), byte(pub.E)}
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	set := jwkSet{Keys: []jwk{{
		Kty: "RSA",
		Kid: kid,
		N:   base64.RawURLEncoding.EncodeToString(nBytes),
		E:   base64.RawURLEncoding.EncodeToString(eBytes),
	}}}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(set)
	}))
}

func signTestToken(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return signed
}

func TestVerifier_Verify_ValidToken(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	server := newTestVerifierServer(t, "test-kid", &priv.PublicKey)
	defer server.Close()

	v := &Verifier{clientID: "test-client-id", certsURL: server.URL, httpClient: http.DefaultClient}

	token := signTestToken(t, priv, "test-kid", jwt.MapClaims{
		"aud":            "test-client-id",
		"iss":            "accounts.google.com",
		"sub":            "google-sub-123",
		"email":          "user@example.com",
		"email_verified": true,
		"exp":            time.Now().Add(time.Hour).Unix(),
	})

	claims, err := v.Verify(t.Context(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Sub != "google-sub-123" || claims.Email != "user@example.com" || !claims.EmailVerified {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestVerifier_Verify_WrongAudience(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	server := newTestVerifierServer(t, "test-kid", &priv.PublicKey)
	defer server.Close()

	v := &Verifier{clientID: "test-client-id", certsURL: server.URL, httpClient: http.DefaultClient}
	token := signTestToken(t, priv, "test-kid", jwt.MapClaims{
		"aud": "some-other-client-id", "iss": "accounts.google.com",
		"sub": "s", "email": "e@example.com", "email_verified": true,
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := v.Verify(t.Context(), token); err != ErrInvalidGoogleToken {
		t.Fatalf("Verify (wrong aud) = %v, want ErrInvalidGoogleToken", err)
	}
}

func TestVerifier_Verify_WrongIssuer(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	server := newTestVerifierServer(t, "test-kid", &priv.PublicKey)
	defer server.Close()

	v := &Verifier{clientID: "test-client-id", certsURL: server.URL, httpClient: http.DefaultClient}
	token := signTestToken(t, priv, "test-kid", jwt.MapClaims{
		"aud": "test-client-id", "iss": "https://evil.example.com",
		"sub": "s", "email": "e@example.com", "email_verified": true,
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := v.Verify(t.Context(), token); err != ErrInvalidGoogleToken {
		t.Fatalf("Verify (wrong iss) = %v, want ErrInvalidGoogleToken", err)
	}
}

func TestVerifier_Verify_Expired(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	server := newTestVerifierServer(t, "test-kid", &priv.PublicKey)
	defer server.Close()

	v := &Verifier{clientID: "test-client-id", certsURL: server.URL, httpClient: http.DefaultClient}
	token := signTestToken(t, priv, "test-kid", jwt.MapClaims{
		"aud": "test-client-id", "iss": "accounts.google.com",
		"sub": "s", "email": "e@example.com", "email_verified": true,
		"exp": time.Now().Add(-time.Hour).Unix(),
	})

	if _, err := v.Verify(t.Context(), token); err != ErrInvalidGoogleToken {
		t.Fatalf("Verify (expired) = %v, want ErrInvalidGoogleToken", err)
	}
}

func TestVerifier_Verify_WrongSignature(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	server := newTestVerifierServer(t, "test-kid", &priv.PublicKey) // serves priv's public key
	defer server.Close()

	v := &Verifier{clientID: "test-client-id", certsURL: server.URL, httpClient: http.DefaultClient}
	// signed with a DIFFERENT private key than the one whose public key is served
	token := signTestToken(t, otherPriv, "test-kid", jwt.MapClaims{
		"aud": "test-client-id", "iss": "accounts.google.com",
		"sub": "s", "email": "e@example.com", "email_verified": true,
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := v.Verify(t.Context(), token); err != ErrInvalidGoogleToken {
		t.Fatalf("Verify (wrong signature) = %v, want ErrInvalidGoogleToken", err)
	}
}

func TestVerifier_Verify_EmailNotVerifiedIsReturnedNotRejected(t *testing.T) {
	// Verify() itself does not reject on email_verified=false — that gate is
	// Service.authenticateClaims's job (a business rule, not cryptographic).
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	server := newTestVerifierServer(t, "test-kid", &priv.PublicKey)
	defer server.Close()

	v := &Verifier{clientID: "test-client-id", certsURL: server.URL, httpClient: http.DefaultClient}
	token := signTestToken(t, priv, "test-kid", jwt.MapClaims{
		"aud": "test-client-id", "iss": "accounts.google.com",
		"sub": "s", "email": "e@example.com", "email_verified": false,
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	claims, err := v.Verify(t.Context(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.EmailVerified {
		t.Fatal("expected EmailVerified=false to be passed through, got true")
	}
}
