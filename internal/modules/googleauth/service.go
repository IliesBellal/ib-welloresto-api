package googleauth

import (
	"context"
	"errors"
	"strings"

	authModule "welloresto-api/internal/modules/auth"
)

// LOT A Semaine 2, Chantier 7b — the five branches of §5.2.3, implemented
// exactly, in this order. The refusal branch (ErrGoogleAccountHasPassword)
// never falls through to auto-link — that is the chantier's stated security
// point ("le refus ne doit jamais fusionner silencieusement").
var (
	// ErrGoogleAccountNotFound: google_sub unknown, email unknown — no
	// account to log into. Not this endpoint's job to create one (see
	// chantier 7c: POST /v1/signup, provider "google").
	ErrGoogleAccountNotFound = errors.New("google_account_not_found")

	// ErrGoogleAccountHasPassword: google_sub unknown, email known, the
	// account already has a password — refuse, never auto-link. The client
	// should invite the user to log in with their password and link Google
	// from settings instead.
	ErrGoogleAccountHasPassword = errors.New("google_account_has_password")

	// ErrGoogleEmailNotVerified: email_verified == false on the id_token —
	// systematic refusal regardless of every other condition.
	ErrGoogleEmailNotVerified = errors.New("google_email_not_verified")
)

type Service struct {
	verifier *Verifier
	repo     *Repository
	// authModule.NewAuthRepository returns a value (not a pointer) — matched
	// here rather than imposing a pointer this module doesn't own.
	authRepo authModule.AuthRepository
}

func NewService(verifier *Verifier, repo *Repository, authRepo authModule.AuthRepository) *Service {
	return &Service{verifier: verifier, repo: repo, authRepo: authRepo}
}

// Authenticate verifies idToken and resolves it to a session per §5.2.3.
func (s *Service) Authenticate(ctx context.Context, idToken string) (AuthenticateResponse, error) {
	claims, err := s.verifier.Verify(ctx, idToken)
	if err != nil {
		return AuthenticateResponse{}, err
	}
	return s.authenticateClaims(ctx, claims)
}

// authenticateClaims is Authenticate's branch logic, split out so it can be
// exercised directly with fabricated Claims — the five branches of §5.2.3
// are a database-driven decision tree that has nothing to do with JWT
// signature verification, and testing them through real Google-signed
// tokens would need Google's own private key.
func (s *Service) authenticateClaims(ctx context.Context, claims *Claims) (AuthenticateResponse, error) {
	// Systematic gate, before anything else — never bypassed by any branch below.
	if !claims.EmailVerified {
		return AuthenticateResponse{}, ErrGoogleEmailNotVerified
	}

	// Branch 1: google_sub known -> connexion.
	if token, err := s.repo.FindTokenByGoogleSub(ctx, claims.Sub); err != nil {
		return AuthenticateResponse{}, err
	} else if token != "" {
		return s.sessionFromToken(ctx, token)
	}

	// google_sub unknown: resolve by email.
	existing, err := s.repo.FindUserByEmail(ctx, strings.TrimSpace(claims.Email))
	if err != nil {
		return AuthenticateResponse{}, err
	}

	// Branch 2: address unknown -> not this endpoint's job (chantier 7c).
	if existing == nil {
		return AuthenticateResponse{}, ErrGoogleAccountNotFound
	}

	// Branch 4: address known, account HAS a password -> refuse, never
	// auto-link. Checked and returned before any write happens.
	if existing.HasPassword {
		return AuthenticateResponse{}, ErrGoogleAccountHasPassword
	}

	// Branch 3: address known, no password -> automatic rattachement.
	if err := s.repo.LinkGoogleSub(ctx, existing.UserID, claims.Sub); err != nil {
		return AuthenticateResponse{}, err
	}
	token, err := s.repo.FindTokenByGoogleSub(ctx, claims.Sub)
	if err != nil {
		return AuthenticateResponse{}, err
	}
	if token == "" {
		return AuthenticateResponse{}, errors.New("googleauth: linked google_sub but found no users_rights token")
	}
	return s.sessionFromToken(ctx, token)
}

// sessionFromToken reuses auth.AuthRepository.GetUserByToken wholesale — the
// same rich lookup every other session-resolving path in this API goes
// through — rather than re-deriving a session response from scratch.
func (s *Service) sessionFromToken(ctx context.Context, token string) (AuthenticateResponse, error) {
	row, err := s.authRepo.GetUserByToken(ctx, token)
	if err != nil {
		return AuthenticateResponse{}, err
	}
	if row == nil {
		return AuthenticateResponse{}, ErrGoogleAccountNotFound
	}
	return AuthenticateResponse{MerchantID: row.MerchantID, UserID: row.UserID, Token: row.Token}, nil
}
