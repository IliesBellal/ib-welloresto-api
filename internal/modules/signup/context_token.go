package signup

import (
	"crypto/rand"
	"time"

	"welloresto-api/internal/modules/pricing"

	"github.com/golang-jwt/jwt/v5"
)

// ContextTokenTTL is POST /v1/public/signup-context's token lifetime — 24h,
// per docs/WelloResto-Parcours-Client-v2.docx §4.4 ("TTL 24 h, signé
// HS256"). Earlier in this chantier, before that document was found, this
// was guessed at 7 days; see docs/decisions.md for that history.
const ContextTokenTTL = 24 * time.Hour

// Attribution is the vitrine site's marketing attribution, carried through
// the context token opaquely and written to merchant.signup_source at
// account creation (§4.4's "attribution" object).
type Attribution struct {
	UTMSource string `json:"utm_source,omitempty"`
	Landing   string `json:"landing,omitempty"`
	Referrer  string `json:"referrer,omitempty"`
}

// contextClaims is the context_token's payload — a self-contained, signed
// JWT (not a database row: nothing here is sensitive, and statelessness
// means GET /v1/public/signup-context/{token} and POST /v1/signup's own
// resolution never need a DB round trip). §4.4 explicitly requires this be
// signed server-side ("sinon un visiteur pourrait s'attribuer un pack
// Complet au prix Essentiel") — HS256 over a server-held key satisfies that;
// the client never sees anything but the opaque compact token string.
type contextClaims struct {
	jwt.RegisteredClaims
	Segment            string                  `json:"segment,omitempty"`
	Cart               pricing.Cart            `json:"cart"`
	PlanCode           string                  `json:"plan_code"`
	MonthlyTotalCents  int                     `json:"monthly_total_cents"`
	Breakdown          []pricing.BreakdownLine `json:"breakdown"`
	RecommendedChannel string                  `json:"recommended_channel"`
	Attribution        Attribution             `json:"attribution"`
}

// contextTokenSigner signs and verifies context_token values.
type contextTokenSigner struct {
	key []byte
}

// newContextTokenSigner builds the signer from AppConfig.SignupContextSigningKey.
// An empty configuredKey (unset env var) falls back to a random in-process
// key — tokens then don't survive a restart or work across instances, but
// startup never crashes over a key that has nothing deployed depending on
// it yet. Set SIGNUP_CONTEXT_SIGNING_KEY for real before relying on this
// past a single dev session.
func newContextTokenSigner(configuredKey string) *contextTokenSigner {
	if configuredKey != "" {
		return &contextTokenSigner{key: []byte(configuredKey)}
	}
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return &contextTokenSigner{key: random}
}

func (s *contextTokenSigner) sign(claims contextClaims) (string, error) {
	now := time.Now().UTC()
	claims.RegisteredClaims = jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ContextTokenTTL)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.key)
}

// verify returns the decoded claims, or (nil, err) for a missing, expired,
// or tampered token — models.ErrContextNotFound covers all three, on
// purpose: a caller has no legitimate use for distinguishing "expired" from
// "forged" here.
func (s *contextTokenSigner) verify(tokenString string) (*contextClaims, error) {
	claims := &contextClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		return s.key, nil
	})
	if err != nil || !token.Valid {
		return nil, errContextTokenInvalid
	}
	return claims, nil
}
