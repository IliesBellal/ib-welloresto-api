package googleauth

import (
	"context"
	"errors"
)

// Rattachement d'un compte Google depuis les paramètres du compte (session
// déjà ouverte) — the explicit path that POST /v1/auth/google's refusal
// branch (ErrGoogleAccountHasPassword) points to. Identity always comes from
// the session token, never from the request body.
var (
	// ErrGoogleAccountLinkedElsewhere: the Google account is already attached
	// to a different Wello user (uq_users_google_sub would refuse it anyway).
	ErrGoogleAccountLinkedElsewhere = errors.New("google_account_linked_elsewhere")

	// ErrGoogleAlreadyLinked: the caller already has a different Google
	// account attached — never silently replaced, unlink first.
	ErrGoogleAlreadyLinked = errors.New("google_already_linked")

	// ErrGoogleUnlinkRequiresPassword: the caller has no password, Google is
	// their only login method.
	ErrGoogleUnlinkRequiresPassword = errors.New("google_unlink_requires_password")
)

// CallerUserID resolves a session token to its user_id, "" if unknown —
// same GetUserByToken lookup as auth.AuthHandler's self-service routes.
func (s *Service) CallerUserID(ctx context.Context, token string) (string, error) {
	row, err := s.authRepo.GetUserByToken(ctx, token)
	if err != nil || row == nil {
		return "", err
	}
	return row.UserID, nil
}

// LinkStatus reports userID's Google rattachement for the settings screen.
func (s *Service) LinkStatus(ctx context.Context, userID string) (LinkStatusResponse, error) {
	state, err := s.repo.GetLinkState(ctx, userID)
	if err != nil {
		return LinkStatusResponse{}, err
	}
	if state == nil {
		return LinkStatusResponse{}, ErrGoogleAccountNotFound
	}
	return LinkStatusResponse{Linked: state.Linked, CanUnlink: state.Linked && state.HasPassword}, nil
}

// Link verifies idToken and attaches its Google identity to userID.
func (s *Service) Link(ctx context.Context, userID, idToken string) error {
	claims, err := s.verifier.Verify(ctx, idToken)
	if err != nil {
		return err
	}
	return s.linkClaims(ctx, userID, claims)
}

// linkClaims is Link's decision logic, split out for the same reason as
// authenticateClaims: testable with fabricated Claims.
func (s *Service) linkClaims(ctx context.Context, userID string, claims *Claims) error {
	// Same systematic gate as the login path.
	if !claims.EmailVerified {
		return ErrGoogleEmailNotVerified
	}

	owner, err := s.repo.FindUserIDByGoogleSub(ctx, claims.Sub)
	if err != nil {
		return err
	}
	if owner == userID {
		return nil // already linked to this very Google account — idempotent
	}
	if owner != "" {
		return ErrGoogleAccountLinkedElsewhere
	}

	matched, err := s.repo.LinkGoogleSubIfUnlinked(ctx, userID, claims.Sub)
	if err != nil {
		return err
	}
	if !matched {
		state, err := s.repo.GetLinkState(ctx, userID)
		if err != nil {
			return err
		}
		if state == nil {
			return ErrGoogleAccountNotFound
		}
		return ErrGoogleAlreadyLinked
	}
	return nil
}

// Unlink detaches userID's Google identity. Not linked is a no-op.
func (s *Service) Unlink(ctx context.Context, userID string) error {
	matched, err := s.repo.UnlinkGoogleSub(ctx, userID)
	if err != nil || matched {
		return err
	}
	state, err := s.repo.GetLinkState(ctx, userID)
	if err != nil {
		return err
	}
	if state == nil {
		return ErrGoogleAccountNotFound
	}
	if state.Linked && !state.HasPassword {
		return ErrGoogleUnlinkRequiresPassword
	}
	return nil
}
