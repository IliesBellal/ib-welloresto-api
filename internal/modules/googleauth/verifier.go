package googleauth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	googleCertsURL = "https://www.googleapis.com/oauth2/v3/certs"
	keysCacheTTL   = time.Hour
)

// ErrInvalidGoogleToken covers every way a Google id_token can fail
// verification (bad signature, wrong aud/iss, expired, malformed) — the
// caller never needs to distinguish these, only "reject".
var ErrInvalidGoogleToken = errors.New("invalid_google_token")

// Claims is the subset of a verified Google id_token's claims this module
// needs. Only returned once signature, aud, iss and exp have all passed —
// EmailVerified is the one claim the caller (Service) still has to check
// itself, since "refus systématique" on it is a business rule, not a
// cryptographic one.
type Claims struct {
	Sub           string
	Email         string
	EmailVerified bool
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// Verifier verifies Google Sign-In id_tokens against Google's published
// public keys (LOT A Semaine 2, Chantier 7b), cached 1h as specified —
// managed here explicitly (a plain mutex-guarded map) rather than relying
// on a library's own cache policy, so the 1h figure is exact and visible.
type Verifier struct {
	clientID   string
	certsURL   string
	httpClient *http.Client

	mu            sync.Mutex
	keys          map[string]*rsa.PublicKey
	keysFetchedAt time.Time
}

func NewVerifier(clientID string) *Verifier {
	return &Verifier{
		clientID:   clientID,
		certsURL:   googleCertsURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewVerifierWithCertsURL is NewVerifier with an overridable JWKS URL — for
// tests (in other packages, e.g. internal/modules/signup) that need to point
// at a local JWKS server instead of Google's real endpoint. Production code
// should always use NewVerifier.
func NewVerifierWithCertsURL(clientID, certsURL string) *Verifier {
	v := NewVerifier(clientID)
	v.certsURL = certsURL
	return v
}

// Verify checks the id_token's signature against Google's cached public
// keys, then aud == clientID, iss, and exp (exp via jwt's built-in
// expiration validation). Returns Claims only once every one of those has
// passed.
func (v *Verifier) Verify(ctx context.Context, idToken string) (*Claims, error) {
	if v.clientID == "" {
		return nil, fmt.Errorf("googleauth: GOOGLE_CLIENT_ID is not configured")
	}

	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(idToken, &claims, func(t *jwt.Token) (interface{}, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("googleauth: id_token has no kid header")
		}
		return v.publicKey(ctx, kid)
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !token.Valid {
		return nil, ErrInvalidGoogleToken
	}

	aud, _ := claims.GetAudience()
	audOK := false
	for _, a := range aud {
		if a == v.clientID {
			audOK = true
			break
		}
	}
	if !audOK {
		return nil, ErrInvalidGoogleToken
	}

	iss, _ := claims.GetIssuer()
	if iss != "accounts.google.com" && iss != "https://accounts.google.com" {
		return nil, ErrInvalidGoogleToken
	}

	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	if sub == "" || email == "" {
		return nil, ErrInvalidGoogleToken
	}
	emailVerified, _ := claims["email_verified"].(bool)

	return &Claims{Sub: sub, Email: email, EmailVerified: emailVerified}, nil
}

func (v *Verifier) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.keys == nil || time.Since(v.keysFetchedAt) > keysCacheTTL {
		keys, err := v.fetchKeys(ctx)
		if err != nil {
			return nil, err
		}
		v.keys = keys
		v.keysFetchedAt = time.Now()
	}

	key, ok := v.keys[kid]
	if !ok {
		return nil, fmt.Errorf("googleauth: no public key for kid %q", kid)
	}
	return key, nil
}

func (v *Verifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("googleauth: fetch certs: unexpected status %d", resp.StatusCode)
	}

	var set jwkSet
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, err
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}

	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}

	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}
