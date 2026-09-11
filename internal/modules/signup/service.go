package signup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/googleauth"
	"welloresto-api/internal/modules/onboarding"
	"welloresto-api/internal/modules/pos"
	"welloresto-api/internal/modules/presets"
	"welloresto-api/internal/modules/users"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

// DefaultPackageID is used when the request carries no context_token, or
// the resolved signup_sessions row (see repository.go's
// GetSessionByContextToken) has no package_id in its payload. LOT A Semaine
// 2, Chantier 6b — decided with the user: packages.id=1 ("Essentiel", the
// only package with a genuine trial_period_days outside the internal/test-
// sounding ones) — the common self-serve SaaS pattern of starting a
// self-signup on the cheapest real tier rather than the most-used one.
const DefaultPackageID = "1"

type Service struct {
	database       *sql.DB
	sessionsRepo   *Repository
	usersRepo      *users.UsersRepository
	posService     *pos.POSService
	presetsRepo    *presets.Repository
	presetsService *presets.Service
	onboardingRepo *onboarding.Repository
	googleVerifier *googleauth.Verifier
}

func NewService(
	db *sql.DB,
	sessionsRepo *Repository,
	usersRepo *users.UsersRepository,
	posService *pos.POSService,
	presetsRepo *presets.Repository,
	presetsService *presets.Service,
	onboardingRepo *onboarding.Repository,
	googleVerifier *googleauth.Verifier,
) *Service {
	return &Service{
		database:       db,
		sessionsRepo:   sessionsRepo,
		usersRepo:      usersRepo,
		posService:     posService,
		presetsRepo:    presetsRepo,
		presetsService: presetsService,
		onboardingRepo: onboardingRepo,
		googleVerifier: googleVerifier,
	}
}

// Signup dispatches on req.Provider — "password" (chantier 6b) or "google"
// (chantier 7c, the id_token replacing email+password entirely).
func (s *Service) Signup(ctx context.Context, req SignupRequest) (SignupResponse, error) {
	switch req.Provider {
	case "password":
		return s.signupPassword(ctx, req)
	case "google":
		return s.signupGoogle(ctx, req)
	default:
		return SignupResponse{}, models.ErrInvalidInput
	}
}

// signupPassword validates the request and, if it passes, creates the owner
// user and the merchant inside a single transaction — reusing
// POSService.CreateMerchant wholesale (InsertMerchant/InsertSubscription/
// InitMerchantSatellites/EnsureSystemRoles/SetDefaultRoleID/owner ADMIN
// link), never duplicating that logic. POST /pos/create's settings.manage
// guard lives on the route (cmd/api/routes.go), not inside POSService —
// calling the service directly here, from an intentionally unauthenticated
// route, bypasses no permission check that was ever meant to apply to it.
//
// Idempotency (the Idempotency-Key header, 24h replay) is handled entirely
// by the caller (handler.go) — this method has no knowledge of it and can be
// called exactly once per real signup attempt.
func (s *Service) signupPassword(ctx context.Context, req SignupRequest) (SignupResponse, error) {
	email := strings.TrimSpace(req.Email)
	firstName := strings.TrimSpace(req.FirstName)
	lastName := strings.TrimSpace(req.LastName)
	if email == "" || firstName == "" || lastName == "" {
		return SignupResponse{}, models.ErrInvalidInput
	}
	if err := helpers.ValidatePassword(req.Password); err != nil {
		return SignupResponse{}, err
	}

	preset, siret, err := s.validateSharedFields(ctx, req)
	if err != nil {
		return SignupResponse{}, err
	}
	if err := s.rejectIfEmailTaken(ctx, email); err != nil {
		return SignupResponse{}, err
	}

	hashedPassword, err := helpers.HashUserPassword(req.Password)
	if err != nil {
		return SignupResponse{}, err
	}
	userID := helpers.GeneratePrefixedID(helpers.UserIDPrefix)
	userToken, err := helpers.GenerateToken(30)
	if err != nil {
		return SignupResponse{}, err
	}

	// name = lower(email) (chantier 6b) — CreateUser's fullName parameter is
	// caller-supplied for exactly this reason (staff-created members pass
	// first+last instead — see users/create_service.go). auth_provider
	// defaults to 'password' at the column level (migration 128) so it is
	// left implicit here.
	createOwner := func(txCtx context.Context) error {
		return s.usersRepo.CreateUser(txCtx, userID, strings.ToLower(email), firstName, lastName, email, req.Tel, hashedPassword, userToken)
	}

	return s.createOwnerAndMerchant(ctx, req, preset, siret, userID, createOwner)
}

// signupGoogle mirrors signupPassword, except identity comes from a verified
// Google id_token instead of email+password (LOT A Semaine 2, Chantier 7c).
// The same email_verified gate as POST /v1/auth/google applies — accepting
// an unverified email here would let someone bypass that endpoint's own
// refusal by going through signup instead with an address they don't
// control.
func (s *Service) signupGoogle(ctx context.Context, req SignupRequest) (SignupResponse, error) {
	if s.googleVerifier == nil {
		return SignupResponse{}, fmt.Errorf("signup: google provider not configured")
	}
	claims, err := s.googleVerifier.Verify(ctx, req.IDToken)
	if err != nil {
		return SignupResponse{}, models.ErrInvalidGoogleToken
	}
	if !claims.EmailVerified {
		return SignupResponse{}, models.ErrGoogleEmailNotVerified
	}

	email := strings.TrimSpace(claims.Email)
	firstName := strings.TrimSpace(req.FirstName)
	lastName := strings.TrimSpace(req.LastName)
	if email == "" || firstName == "" || lastName == "" {
		return SignupResponse{}, models.ErrInvalidInput
	}

	preset, siret, err := s.validateSharedFields(ctx, req)
	if err != nil {
		return SignupResponse{}, err
	}
	if err := s.rejectIfEmailTaken(ctx, email); err != nil {
		return SignupResponse{}, err
	}

	userID := helpers.GeneratePrefixedID(helpers.UserIDPrefix)
	userToken, err := helpers.GenerateToken(30)
	if err != nil {
		return SignupResponse{}, err
	}

	// name = lower(email), auth_provider = 'google', google_sub renseigné,
	// email_verified_at rempli depuis le jeton (chantier 7c) — all inside
	// CreateGoogleUser (users/create_repository.go), not duplicated here.
	createOwner := func(txCtx context.Context) error {
		return s.usersRepo.CreateGoogleUser(txCtx, userID, strings.ToLower(email), firstName, lastName, email, req.Tel, claims.Sub, userToken)
	}

	return s.createOwnerAndMerchant(ctx, req, preset, siret, userID, createOwner)
}

// validateSharedFields checks everything both providers need identically:
// SIRET format + uniqueness, preset_code validity, required merchant
// identity fields. Returns the resolved preset and normalized SIRET.
func (s *Service) validateSharedFields(ctx context.Context, req SignupRequest) (*presets.MerchantPreset, string, error) {
	siret := strings.TrimSpace(req.Merchant.SIRET)
	if !helpers.ValidateSIRETFormat(siret) {
		return nil, "", models.ErrInvalidSIRETFormat
	}
	presetCode := strings.TrimSpace(req.PresetCode)
	if presetCode == "" {
		return nil, "", models.ErrInvalidInput
	}
	if strings.TrimSpace(req.Merchant.FullName) == "" || strings.TrimSpace(req.Merchant.Tel) == "" {
		return nil, "", models.ErrInvalidInput
	}

	// SIRET already attached to another merchant: refuse without revealing
	// account existence (§5.7), log for manual review. Pre-check only —
	// merchant.siret has no unique constraint in this schema, so a
	// genuinely concurrent double-signup on the same SIRET is not caught at
	// the DB level here. Flagged in docs/decisions.md; out of this
	// chantier's explicit scope (unlike email, which had its own dedicated
	// chantier — LOT A Semaine 1, Chantier 3).
	siretTaken, err := s.merchantSIRETExists(ctx, siret)
	if err != nil {
		return nil, "", err
	}
	if siretTaken {
		logger.FromContext(ctx).Warn("signup: SIRET already attached to another merchant — rejected generically, needs manual review",
			zap.String("siret", siret))
		return nil, "", models.ErrInvalidInput
	}

	preset, err := s.presetsRepo.GetActivePresetByCode(ctx, presetCode)
	if err != nil {
		if errors.Is(err, presets.ErrPresetNotFound) {
			return nil, "", models.ErrInvalidPresetCode
		}
		return nil, "", err
	}

	return preset, siret, nil
}

// rejectIfEmailTaken is the pre-check (CreateUser/CreateGoogleUser's own
// dbx.IsDuplicateEntry fallback still catches the concurrent race).
func (s *Service) rejectIfEmailTaken(ctx context.Context, email string) error {
	taken, err := s.usersRepo.EmailExists(ctx, email)
	if err != nil {
		return err
	}
	if taken {
		return models.ErrEmailAlreadyUsed
	}
	return nil
}

// createOwnerAndMerchant is the transactional tail shared by both providers:
// create the owner user (via createOwner, provider-specific), reuse
// POSService.CreateMerchant wholesale, ApplyPreset, create onboarding_tasks.
func (s *Service) createOwnerAndMerchant(ctx context.Context, req SignupRequest, preset *presets.MerchantPreset, siret, userID string, createOwner func(context.Context) error) (SignupResponse, error) {
	packageID := s.resolvePackageID(ctx, req.ContextToken)

	var merchantID, ownerToken string
	err := dbutils.RunInTx(ctx, s.database, func(txCtx context.Context) error {
		if err := createOwner(txCtx); err != nil {
			return err
		}

		merchantResp, err := s.posService.CreateMerchant(txCtx, pos.CreateMerchantRequest{
			FullName: req.Merchant.FullName, Address: req.Merchant.Address, StreetNumber: req.Merchant.StreetNumber,
			Street: req.Merchant.Street, ZipCode: req.Merchant.ZipCode, City: req.Merchant.City, Country: req.Merchant.Country,
			SIRET: siret, Tel: req.Merchant.Tel, WebSite: req.Merchant.WebSite, Email: req.Merchant.Email,
			PackageID: packageID, UserID: userID, Admin: true,
		})
		if err != nil {
			return err
		}
		merchantID = merchantResp.MerchantID
		ownerToken = merchantResp.OwnerRightsToken

		if err := s.presetsService.ApplyPreset(txCtx, merchantID, preset.Code); err != nil {
			return err
		}

		if err := s.onboardingRepo.CreateDefaultTasks(txCtx, merchantID); err != nil {
			return err
		}

		// activation_state = 'SETUP' — already merchant's column default
		// (migration 125); InsertMerchant does not override it, so no
		// explicit write is needed here.

		return nil
	})
	if err != nil {
		return SignupResponse{}, err
	}

	return SignupResponse{
		MerchantID:      merchantID,
		UserID:          userID,
		Token:           ownerToken,
		ActivationState: "SETUP",
	}, nil
}

func (s *Service) merchantSIRETExists(ctx context.Context, siret string) (bool, error) {
	var exists bool
	err := s.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM merchant WHERE siret = $1)`, siret).Scan(&exists)
	return exists, err
}

// resolvePackageID reads package_id from the signup_sessions row identified
// by contextToken (see repository.go's GetSessionByContextToken), falling
// back to DefaultPackageID when contextToken is empty, resolves to nothing,
// or its payload carries no package_id. Best-effort: any lookup error falls
// back to the default rather than failing the whole signup over an optional
// hint.
func (s *Service) resolvePackageID(ctx context.Context, contextToken string) string {
	if strings.TrimSpace(contextToken) == "" {
		return DefaultPackageID
	}
	session, err := s.sessionsRepo.GetSessionByContextToken(ctx, contextToken)
	if err != nil || session == nil || len(session.Payload) == 0 {
		return DefaultPackageID
	}
	var ctxPayload struct {
		PackageID string `json:"package_id"`
	}
	if json.Unmarshal(session.Payload, &ctxPayload) != nil || strings.TrimSpace(ctxPayload.PackageID) == "" {
		return DefaultPackageID
	}
	return ctxPayload.PackageID
}
