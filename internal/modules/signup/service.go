package signup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/googleauth"
	"welloresto-api/internal/modules/onboarding"
	"welloresto-api/internal/modules/pos"
	"welloresto-api/internal/modules/presets"
	"welloresto-api/internal/modules/pricing"
	"welloresto-api/internal/modules/users"
	"welloresto-api/internal/utils/dbutils"

	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
)

// DefaultPackageID is used when the request carries no context_token, or
// the context_token's plan_code cannot be resolved to a real packages.id
// for any reason. LOT A Semaine 2, Chantier 6b — decided with the user:
// packages.id=1 ("Essentiel", the only package with a genuine
// trial_period_days outside the internal/test-sounding ones) — the common
// self-serve SaaS pattern of starting a self-signup on the cheapest real
// tier rather than the most-used one.
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
	pricingService *pricing.Service
	redis          *redisclient.Client
	contextSigner  *contextTokenSigner
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
	pricingService *pricing.Service,
	redis *redisclient.Client,
	contextSigningKey string,
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
		pricingService: pricingService,
		redis:          redis,
		contextSigner:  newContextTokenSigner(contextSigningKey),
	}
}

// signupContextIPThrottlePrefix / Max / Window bound POST
// /v1/public/signup-context — a public route with no auth, callable by the
// vitrine site for every visitor composing a cart, so it gets the same
// per-IP throttle shape as password-reset (see redisclient.Client.TooManyRequestsFromIP).
const (
	signupContextIPThrottlePrefix = "signupctx:ipthrottle:"
	signupContextIPThrottleMax    = 30
	signupContextIPThrottleWindow = time.Hour
)

// CreateContext handles POST /v1/public/signup-context (LOT A Semaine 3,
// Chantier 11) — docs/WelloResto-Parcours-Client-v2.docx §4.4. Prices cart
// via pricingService (the single implementation — see
// pricing.Service.ResolveCheapestPlan's doc comment) and signs the result
// into a stateless context_token (no DB row — see context_token.go's doc
// comment on why).
func (s *Service) CreateContext(ctx context.Context, clientIP string, req CreateContextRequest) (CreateContextResponse, error) {
	if s.redis.TooManyRequestsFromIP(ctx, signupContextIPThrottlePrefix, clientIP, signupContextIPThrottleMax, signupContextIPThrottleWindow) {
		return CreateContextResponse{}, models.ErrRateLimited
	}

	quote, err := s.pricingService.ResolveCheapestPlan(ctx, req.Cart)
	if err != nil {
		return CreateContextResponse{}, models.ErrInvalidInput
	}

	// recommended_channel: no business rule for self_serve vs assisted was
	// found in the reference doc beyond the concept existing — always
	// "self_serve" for now (the only channel this API actually routes
	// through today). Revisit once an assisted-routing rule is specified.
	const recommendedChannel = "self_serve"

	claims := contextClaims{
		Segment:            req.Segment,
		Cart:               req.Cart,
		PlanCode:           quote.PlanCode,
		MonthlyTotalCents:  quote.MonthlyTotalCents,
		Breakdown:          quote.Breakdown,
		RecommendedChannel: recommendedChannel,
		Attribution:        req.Attribution,
	}
	token, err := s.contextSigner.sign(claims)
	if err != nil {
		return CreateContextResponse{}, err
	}

	return CreateContextResponse{
		ContextToken:       token,
		ResolvedPlan:       quote,
		RecommendedChannel: recommendedChannel,
	}, nil
}

// GetContext handles GET /v1/public/signup-context/{token} — the tunnel
// restitutes the cart and its server-computed price server-side, never
// trusting anything the client itself carried (§4.4). Stateless: just JWT
// verification, no DB lookup.
func (s *Service) GetContext(ctx context.Context, token string) (GetContextResponse, error) {
	claims, err := s.contextSigner.verify(strings.TrimSpace(token))
	if err != nil {
		return GetContextResponse{}, models.ErrContextNotFound
	}
	return GetContextResponse{
		Segment: claims.Segment,
		Cart:    claims.Cart,
		ResolvedPlan: pricing.Quote{
			PlanCode:          claims.PlanCode,
			MonthlyTotalCents: claims.MonthlyTotalCents,
			Breakdown:         claims.Breakdown,
		},
		RecommendedChannel: claims.RecommendedChannel,
	}, nil
}

// Signup dispatches on req.Identity.Provider — "password" (chantier 6) or
// "google" (chantier 7c, the id_token replacing email + password entirely).
func (s *Service) Signup(ctx context.Context, req SignupRequest) (SignupResponse, error) {
	switch req.Identity.Provider {
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
	email := strings.TrimSpace(req.Identity.Email)
	firstName := strings.TrimSpace(req.Identity.FirstName)
	lastName := strings.TrimSpace(req.Identity.LastName)
	if email == "" || firstName == "" || lastName == "" {
		return SignupResponse{}, models.ErrInvalidInput
	}
	if err := helpers.ValidatePassword(req.Identity.Password); err != nil {
		return SignupResponse{}, err
	}

	preset, siret, err := s.validateSharedFields(ctx, req)
	if err != nil {
		return SignupResponse{}, err
	}
	if err := s.rejectIfEmailTaken(ctx, email); err != nil {
		return SignupResponse{}, err
	}

	hashedPassword, err := helpers.HashUserPassword(req.Identity.Password)
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
		return s.usersRepo.CreateUser(txCtx, userID, strings.ToLower(email), firstName, lastName, email, req.Merchant.Tel, hashedPassword, userToken, req.AcceptsTerms, req.AcceptsMarketing)
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
	claims, err := s.googleVerifier.Verify(ctx, req.Identity.IDToken)
	if err != nil {
		return SignupResponse{}, models.ErrInvalidGoogleToken
	}
	if !claims.EmailVerified {
		return SignupResponse{}, models.ErrGoogleEmailNotVerified
	}

	email := strings.TrimSpace(claims.Email)
	firstName := strings.TrimSpace(req.Identity.FirstName)
	lastName := strings.TrimSpace(req.Identity.LastName)
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
		return s.usersRepo.CreateGoogleUser(txCtx, userID, strings.ToLower(email), firstName, lastName, email, req.Merchant.Tel, claims.Sub, userToken, req.AcceptsTerms, req.AcceptsMarketing)
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
	// account existence, but WITH a distinct message this time
	// (docs/WelloResto-Parcours-Client-v2.docx §5.7: "Cet établissement
	// semble déjà enregistré. Contactez-nous pour être rattaché." — a
	// different message from email-taken's, both equally silent on whether
	// an *account* exists, but SIRET's own message additionally implies
	// "get in touch to be attached", which email's must never imply since
	// there the situation is "log in", not "contact us"). Pre-check only —
	// the genuinely concurrent case (two signups for the same SIRET both
	// passing this check before either commits) is caught at the DB level
	// by uq_merchant_siret_valid (migration 132, LOT A Semaine 3 Chantier
	// 10) and translated identically by isSIRETUniqueViolation below.
	siretTaken, err := s.merchantSIRETExists(ctx, siret)
	if err != nil {
		return nil, "", err
	}
	if siretTaken {
		logger.FromContext(ctx).Warn("signup: SIRET already attached to another merchant — rejected with a dedicated message, needs manual review",
			zap.String("siret", siret))
		return nil, "", models.ErrSIRETAlreadyRegistered
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
	packageID, segment := s.resolveContext(ctx, req.ContextToken)
	_ = segment // reserved: not consumed by merchant creation yet (screen 3's preset_code already carries the effective choice)

	var merchantID, ownerToken string
	err := dbutils.RunInTx(ctx, s.database, func(txCtx context.Context) error {
		if err := createOwner(txCtx); err != nil {
			return err
		}

		signupSource, _ := json.Marshal(s.contextAttribution(req))

		merchantResp, err := s.posService.CreateMerchant(txCtx, pos.CreateMerchantRequest{
			FullName: req.Merchant.FullName, Address: req.Merchant.Address,
			ZipCode: req.Merchant.ZipCode, City: req.Merchant.City, Country: req.Merchant.Country,
			Lat: req.Merchant.Lat, Lng: req.Merchant.Lng, PlaceID: req.Merchant.PlaceID,
			SIRET: siret, Tel: req.Merchant.Tel, Email: req.Merchant.Email,
			SignupChannel: "self_signup", SignupSource: signupSource,
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
		if isSIRETUniqueViolation(err) {
			// Lost the concurrent race the pre-check cannot catch (two
			// signups for the same SIRET both passing validateSharedFields
			// before either commits) — same dedicated message as the
			// pre-check's own rejection.
			logger.FromContext(ctx).Warn("signup: SIRET race lost to a concurrent signup — rejected with a dedicated message, needs manual review",
				zap.String("siret", siret))
			return SignupResponse{}, models.ErrSIRETAlreadyRegistered
		}
		return SignupResponse{}, err
	}

	return SignupResponse{
		MerchantID:      merchantID,
		UserID:          userID,
		Token:           ownerToken,
		ActivationState: "SETUP",
	}, nil
}

// contextAttribution decodes req.ContextToken (best-effort — an
// expired/absent context loses only the attribution record, never blocks
// signup) to fetch the vitrine's marketing attribution for
// merchant.signup_source.
func (s *Service) contextAttribution(req SignupRequest) Attribution {
	if strings.TrimSpace(req.ContextToken) == "" {
		return Attribution{}
	}
	claims, err := s.contextSigner.verify(req.ContextToken)
	if err != nil {
		return Attribution{}
	}
	return claims.Attribution
}

func (s *Service) merchantSIRETExists(ctx context.Context, siret string) (bool, error) {
	// merchantSIRETExists is the pre-check — same predicate as
	// uq_merchant_siret_valid (migration 132: `siret ~ '^[0-9]{14}$' AND
	// is_active`, the format half already guaranteed here since siret has
	// already passed helpers.ValidateSIRETFormat by the time this runs) so it
	// never rejects a SIRET the index would actually accept — notably one
	// previously used by a merchant since deactivated (is_active = false),
	// which the index deliberately lets a new signup reclaim.
	var exists bool
	err := s.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM merchant WHERE siret = $1 AND is_active)`, siret).Scan(&exists)
	return exists, err
}

// isSIRETUniqueViolation reports whether err is the race the pre-check in
// validateSharedFields cannot catch: two concurrent signups both passing
// the pre-check for the same SIRET before either commits. Scoped to
// uq_merchant_siret_valid specifically (migration 132) — a different unique
// violation inside the same transaction should surface as a genuine error,
// not be silently reworded into "SIRET taken".
func isSIRETUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && pgErr.ConstraintName == "uq_merchant_siret_valid"
	}
	return false
}

// resolveContext decodes contextToken (best-effort — see resolvePackageID's
// predecessor doc comment) into (package_id, segment). Falls back to
// DefaultPackageID when the token is empty, expired, or its plan_code
// cannot be resolved to a real packages.id — any lookup failure here falls
// back rather than failing the whole signup over an optional hint.
func (s *Service) resolveContext(ctx context.Context, contextToken string) (packageID, segment string) {
	if strings.TrimSpace(contextToken) == "" {
		return DefaultPackageID, ""
	}
	claims, err := s.contextSigner.verify(contextToken)
	if err != nil {
		return DefaultPackageID, ""
	}
	pkgID, err := s.pricingService.GetPackageIDForPlan(ctx, claims.PlanCode)
	if err != nil || pkgID == "" {
		return DefaultPackageID, claims.Segment
	}
	return pkgID, claims.Segment
}
