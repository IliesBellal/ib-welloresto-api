package franchises

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/importutil"
	"welloresto-api/internal/models"
)

// Store : accès aux données du module (Repository en production, faux en
// test).
type Store interface {
	RunInTx(ctx context.Context, fn func(txCtx context.Context) error) error

	GetFranchise(ctx context.Context, franchiseID string) (*Franchise, error)
	SlugTaken(ctx context.Context, slug, excludeFranchiseID string) (bool, error)
	InsertFranchise(ctx context.Context, f *Franchise) error
	UpdateFranchise(ctx context.Context, f *Franchise) error
	SetImageURL(ctx context.Context, franchiseID, column, url string) error
	DeleteFranchise(ctx context.Context, franchiseID string) error
	ListUserFranchises(ctx context.Context, userID string, allFranchises bool) ([]FranchiseSummary, error)

	GetMemberRole(ctx context.Context, franchiseID, userID string) (string, error)
	ListMembers(ctx context.Context, franchiseID string) ([]Member, error)
	CountOwners(ctx context.Context, franchiseID string) (int, error)
	InsertMember(ctx context.Context, franchiseID, userID, role string) error
	UpdateMemberRole(ctx context.Context, franchiseID, userID, role string) error
	DeleteMember(ctx context.Context, franchiseID, userID string) error
	FindUserIDByEmail(ctx context.Context, email string) (string, error)

	IsAdminOfAnyMerchant(ctx context.Context, userID string) (bool, error)
	IsMerchantAdmin(ctx context.Context, userID, merchantID string) (bool, error)
	ListAdminMerchants(ctx context.Context, userID string) ([]CandidateMerchant, error)
	ListFranchiseMerchants(ctx context.Context, franchiseID string) ([]FranchiseMerchant, error)
	MerchantIDsOf(ctx context.Context, franchiseID string) ([]string, error)
	AttachMerchant(ctx context.Context, franchiseID, merchantID string) (bool, error)
	DetachMerchant(ctx context.Context, franchiseID, merchantID string) error
	SetMerchantHubVisible(ctx context.Context, franchiseID, merchantID string, visible bool) error
	MerchantFranchiseID(ctx context.Context, merchantID string) (*string, bool, error)

	FranchiseTimezone(ctx context.Context, franchiseID string) (string, error)
	FranchiseStats(ctx context.Context, franchiseID string, merchantIDs []string, startUTC, endUTC time.Time, tz string) (*StatsResponse, error)
}

// Cache : purge des réponses ScanNOrder qui affichent la franchise (hub, et
// lien « autres établissements » de la page restaurant). *redis.Client.
type Cache interface {
	Delete(ctx context.Context, key string) bool
	InvalidateMerchantStatusCache(ctx context.Context, merchantID string)
}

const (
	maxNameLen        = 80
	minSlugLen        = 3
	maxSlugLen        = 50
	maxDescriptionLen = 500
	// Bandeau d'annonce du hub (D34) : une phrase, lisible sur mobile.
	maxAnnouncementLen    = 140
	maxAnnouncementURLLen = 500
)

var (
	slugPattern       = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	brandColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

type Service struct {
	store      Store
	cache      Cache
	hubBaseURL string
}

// NewService : hubBaseURL = URL publique de ScanNOrder (SCANNORDER_BASE_URL).
func NewService(store Store, cache Cache, hubBaseURL string) *Service {
	return &Service{store: store, cache: cache, hubBaseURL: strings.TrimRight(hubBaseURL, "/")}
}

// --- Accès ---

// roleOf : rôle de l'acteur dans la franchise ("" = aucun). Le staff
// WelloResto est owner partout.
func (s *Service) roleOf(ctx context.Context, actor Actor, franchiseID string) (string, error) {
	if actor.PlatformStaff {
		return RoleOwner, nil
	}
	return s.store.GetMemberRole(ctx, franchiseID, actor.UserID)
}

// requireRole charge la franchise et vérifie que l'acteur la gère (minRole
// manager) ou en est owner (minRole owner). Une franchise qu'il ne gère pas
// répond « introuvable », pour ne pas révéler son existence.
func (s *Service) requireRole(ctx context.Context, actor Actor, franchiseID, minRole string) (*Franchise, string, error) {
	if strings.TrimSpace(franchiseID) == "" {
		return nil, "", models.ErrMissingResourceID
	}
	role, err := s.roleOf(ctx, actor, franchiseID)
	if err != nil {
		return nil, "", err
	}
	if role == "" {
		return nil, "", models.ErrFranchiseNotFound
	}
	f, err := s.store.GetFranchise(ctx, franchiseID)
	if err != nil {
		return nil, "", err
	}
	if f == nil {
		return nil, "", models.ErrFranchiseNotFound
	}
	if minRole == RoleOwner && role != RoleOwner {
		return nil, "", models.ErrFranchiseForbidden
	}
	return f, role, nil
}

func (s *Service) canCreate(ctx context.Context, actor Actor) (bool, error) {
	if actor.PlatformStaff {
		return true, nil
	}
	return s.store.IsAdminOfAnyMerchant(ctx, actor.UserID)
}

// --- Franchises ---

func (s *Service) List(ctx context.Context, actor Actor) (*ListResponse, error) {
	items, err := s.store.ListUserFranchises(ctx, actor.UserID, actor.PlatformStaff)
	if err != nil {
		return nil, err
	}
	canCreate, err := s.canCreate(ctx, actor)
	if err != nil {
		return nil, err
	}
	return &ListResponse{Franchises: items, CanCreate: canCreate}, nil
}

// Create : réservé à un administrateur d'au moins un établissement, qui
// devient owner.
func (s *Service) Create(ctx context.Context, actor Actor, req CreateRequest) (*DetailResponse, error) {
	ok, err := s.canCreate(ctx, actor)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, models.ErrFranchiseCreateForbidden
	}

	name, err := normalizeName(req.Name)
	if err != nil {
		return nil, err
	}

	f := &Franchise{
		FranchiseID: helpers.GeneratePrefixedID("fra"),
		Name:        name,
		CreatedAt:   time.Now().UTC(),
	}

	err = s.store.RunInTx(ctx, func(txCtx context.Context) error {
		slug, err := s.resolveNewSlug(txCtx, req.Slug, name)
		if err != nil {
			return err
		}
		f.Slug = slug
		if err := s.store.InsertFranchise(txCtx, f); err != nil {
			return err
		}
		return s.store.InsertMember(txCtx, f.FranchiseID, actor.UserID, RoleOwner)
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, f, RoleOwner)
}

// resolveNewSlug : slug demandé (validé, libre), sinon dérivé du nom, suffixé
// -2, -3… s'il est pris.
func (s *Service) resolveNewSlug(ctx context.Context, requested, name string) (string, error) {
	if strings.TrimSpace(requested) != "" {
		slug, err := normalizeSlug(requested)
		if err != nil {
			return "", err
		}
		taken, err := s.store.SlugTaken(ctx, slug, "")
		if err != nil {
			return "", err
		}
		if taken {
			return "", models.ErrFranchiseSlugTaken
		}
		return slug, nil
	}

	base := importutil.Slugify(name)
	if len(base) < minSlugLen {
		return "", models.ErrFranchiseSlugInvalid
	}
	for i := 1; i <= 20; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		taken, err := s.store.SlugTaken(ctx, candidate, "")
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", models.ErrFranchiseSlugTaken
}

func (s *Service) Get(ctx context.Context, actor Actor, franchiseID string) (*DetailResponse, error) {
	f, role, err := s.requireRole(ctx, actor, franchiseID, RoleManager)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, f, role)
}

func (s *Service) detail(ctx context.Context, f *Franchise, role string) (*DetailResponse, error) {
	merchants, err := s.store.ListFranchiseMerchants(ctx, f.FranchiseID)
	if err != nil {
		return nil, err
	}
	return &DetailResponse{Franchise: *f, HubURL: s.HubURL(f.Slug), Role: role, Merchants: merchants}, nil
}

// HubURL : adresse publique du hub (/hub/{slug} ; l'ancienne adresse
// /franchise/{slug} redirige, docs/franchises.md D7).
func (s *Service) HubURL(slug string) string {
	return s.hubBaseURL + "/hub/" + slug
}

func (s *Service) Update(ctx context.Context, actor Actor, franchiseID string, req UpdateRequest) (*DetailResponse, error) {
	f, role, err := s.requireRole(ctx, actor, franchiseID, RoleManager)
	if err != nil {
		return nil, err
	}
	oldSlug := f.Slug

	if req.Name != nil {
		if f.Name, err = normalizeName(*req.Name); err != nil {
			return nil, err
		}
	}
	if req.Description != nil {
		d := strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(d) > maxDescriptionLen {
			return nil, models.ErrFranchiseDescriptionInvalid
		}
		f.Description = nilIfEmpty(d)
	}
	if req.BrandColor != nil {
		c := strings.TrimSpace(*req.BrandColor)
		if c != "" && !brandColorPattern.MatchString(c) {
			return nil, models.ErrFranchiseBrandColorInvalid
		}
		f.BrandColor = nilIfEmpty(strings.ToUpper(c))
	}
	if req.Announcement != nil {
		if err := applyAnnouncement(&f.Announcement, *req.Announcement); err != nil {
			return nil, err
		}
	}
	if req.Slug != nil {
		slug, err := normalizeSlug(*req.Slug)
		if err != nil {
			return nil, err
		}
		if slug != f.Slug {
			taken, err := s.store.SlugTaken(ctx, slug, f.FranchiseID)
			if err != nil {
				return nil, err
			}
			if taken {
				return nil, models.ErrFranchiseSlugTaken
			}
			f.Slug = slug
		}
	}

	if err := s.store.UpdateFranchise(ctx, f); err != nil {
		return nil, err
	}
	s.invalidateHub(ctx, oldSlug, f.Slug)
	if oldSlug != f.Slug || req.Name != nil {
		// La page restaurant affiche le nom et le lien du hub.
		s.invalidateMerchantsOf(ctx, f.FranchiseID)
	}
	return s.detail(ctx, f, role)
}

// applyAnnouncement applique une modification du bandeau d'annonce (D34) :
// texte de 140 caractères au plus, obligatoire pour l'activer ; lien https
// facultatif ; date de fin facultative (YYYY-MM-DD).
func applyAnnouncement(a *Announcement, req AnnouncementUpdate) error {
	if req.Text != nil {
		t := strings.Join(strings.Fields(*req.Text), " ")
		if utf8.RuneCountInString(t) > maxAnnouncementLen {
			return models.ErrFranchiseAnnouncementInvalid
		}
		a.Text = nilIfEmpty(t)
	}
	if req.URL != nil {
		raw := strings.TrimSpace(*req.URL)
		if raw != "" {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host == "" || len(raw) > maxAnnouncementURLLen {
				return models.ErrFranchiseAnnouncementInvalid
			}
		}
		a.URL = nilIfEmpty(raw)
	}
	if req.EndsOn != nil {
		d := strings.TrimSpace(*req.EndsOn)
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return models.ErrFranchiseAnnouncementInvalid
			}
		}
		a.EndsOn = nilIfEmpty(d)
	}
	if req.Enabled != nil {
		a.Enabled = *req.Enabled
	}
	if a.Enabled && a.Text == nil {
		return models.ErrFranchiseAnnouncementInvalid
	}
	return nil
}

// SetImage enregistre l'URL d'un logo ou d'une bannière déjà envoyé sur R2
// (handler). Retourne l'ancienne URL, que l'appelant peut supprimer.
func (s *Service) SetImage(ctx context.Context, actor Actor, franchiseID, imageType, url string) (oldURL string, err error) {
	f, _, err := s.requireRole(ctx, actor, franchiseID, RoleManager)
	if err != nil {
		return "", err
	}
	column, current := "logo_url", f.LogoURL
	if imageType == "banner" {
		column, current = "banner_url", f.BannerURL
	}
	if err := s.store.SetImageURL(ctx, f.FranchiseID, column, url); err != nil {
		return "", err
	}
	s.invalidateHub(ctx, f.Slug)
	if current != nil {
		return *current, nil
	}
	return "", nil
}

// RequireManager : contrôle d'accès seul (le handler d'upload le fait avant
// d'envoyer le fichier sur R2).
func (s *Service) RequireManager(ctx context.Context, actor Actor, franchiseID string) error {
	_, _, err := s.requireRole(ctx, actor, franchiseID, RoleManager)
	return err
}

// Delete : owner seulement. Les établissements sont détachés, pas supprimés.
func (s *Service) Delete(ctx context.Context, actor Actor, franchiseID string) error {
	f, _, err := s.requireRole(ctx, actor, franchiseID, RoleOwner)
	if err != nil {
		return err
	}
	merchantIDs, err := s.store.MerchantIDsOf(ctx, f.FranchiseID)
	if err != nil {
		return err
	}
	if err := s.store.RunInTx(ctx, func(txCtx context.Context) error {
		return s.store.DeleteFranchise(txCtx, f.FranchiseID)
	}); err != nil {
		return err
	}
	s.invalidateHub(ctx, f.Slug)
	for _, id := range merchantIDs {
		s.invalidateMerchant(ctx, id)
	}
	return nil
}

func (s *Service) SlugAvailability(ctx context.Context, actor Actor, slug, excludeFranchiseID string) (*SlugAvailabilityResponse, error) {
	normalized, err := normalizeSlug(slug)
	if err != nil {
		return &SlugAvailabilityResponse{Slug: slug}, nil
	}
	exclude := ""
	if excludeFranchiseID != "" {
		// Exclure sa propre franchise suppose de la gérer.
		if _, _, err := s.requireRole(ctx, actor, excludeFranchiseID, RoleManager); err != nil {
			return nil, err
		}
		exclude = excludeFranchiseID
	}
	taken, err := s.store.SlugTaken(ctx, normalized, exclude)
	if err != nil {
		return nil, err
	}
	return &SlugAvailabilityResponse{Slug: normalized, Valid: true, Available: !taken}, nil
}

// --- Établissements ---

// Candidates : établissements que l'acteur administre, ceux déjà dans une
// franchise compris (le back-office les grise).
func (s *Service) Candidates(ctx context.Context, actor Actor, franchiseID string) ([]CandidateMerchant, error) {
	if _, _, err := s.requireRole(ctx, actor, franchiseID, RoleManager); err != nil {
		return nil, err
	}
	return s.store.ListAdminMerchants(ctx, actor.UserID)
}

// AddMerchants rattache des établissements : l'acteur doit gérer la franchise
// ET administrer chaque établissement, qui ne doit appartenir à aucune autre
// franchise. Tout ou rien.
func (s *Service) AddMerchants(ctx context.Context, actor Actor, franchiseID string, req AddMerchantsRequest) (*DetailResponse, error) {
	f, role, err := s.requireRole(ctx, actor, franchiseID, RoleManager)
	if err != nil {
		return nil, err
	}
	ids := uniqueTrimmed(req.MerchantIDs)
	if len(ids) == 0 {
		return nil, models.ErrInvalidInput
	}

	for _, id := range ids {
		if !actor.PlatformStaff {
			admin, err := s.store.IsMerchantAdmin(ctx, actor.UserID, id)
			if err != nil {
				return nil, err
			}
			if !admin {
				return nil, models.ErrFranchiseMerchantNotAdmin
			}
		}
	}

	err = s.store.RunInTx(ctx, func(txCtx context.Context) error {
		for _, id := range ids {
			attached, err := s.store.AttachMerchant(txCtx, f.FranchiseID, id)
			if err != nil {
				return err
			}
			if attached {
				continue
			}
			_, found, err := s.store.MerchantFranchiseID(txCtx, id)
			if err != nil {
				return err
			}
			if !found {
				return models.ErrFranchiseMerchantNotAdmin
			}
			return models.ErrFranchiseMerchantInOtherFranchise
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.invalidateHub(ctx, f.Slug)
	for _, id := range ids {
		s.invalidateMerchant(ctx, id)
	}
	return s.detail(ctx, f, role)
}

func (s *Service) UpdateMerchant(ctx context.Context, actor Actor, franchiseID, merchantID string, req UpdateMerchantRequest) (*DetailResponse, error) {
	f, role, err := s.requireRole(ctx, actor, franchiseID, RoleManager)
	if err != nil {
		return nil, err
	}
	if req.HubVisible == nil {
		return nil, models.ErrInvalidInput
	}
	if err := s.store.SetMerchantHubVisible(ctx, f.FranchiseID, merchantID, *req.HubVisible); err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrFranchiseMerchantNotInFranchise
		}
		return nil, err
	}
	s.invalidateHub(ctx, f.Slug)
	s.invalidateMerchant(ctx, merchantID)
	return s.detail(ctx, f, role)
}

// RemoveMerchant : tout gestionnaire peut retirer un établissement de la
// franchise (il reste intact, simplement détaché).
func (s *Service) RemoveMerchant(ctx context.Context, actor Actor, franchiseID, merchantID string) (*DetailResponse, error) {
	f, role, err := s.requireRole(ctx, actor, franchiseID, RoleManager)
	if err != nil {
		return nil, err
	}
	if err := s.store.DetachMerchant(ctx, f.FranchiseID, merchantID); err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrFranchiseMerchantNotInFranchise
		}
		return nil, err
	}
	s.invalidateHub(ctx, f.Slug)
	s.invalidateMerchant(ctx, merchantID)
	return s.detail(ctx, f, role)
}

// --- Gestionnaires ---

func (s *Service) ListMembers(ctx context.Context, actor Actor, franchiseID string) ([]Member, error) {
	if _, _, err := s.requireRole(ctx, actor, franchiseID, RoleManager); err != nil {
		return nil, err
	}
	return s.store.ListMembers(ctx, franchiseID)
}

// AddMember : owner seulement, par l'email d'un compte WelloResto existant.
func (s *Service) AddMember(ctx context.Context, actor Actor, franchiseID string, req AddMemberRequest) ([]Member, error) {
	f, _, err := s.requireRole(ctx, actor, franchiseID, RoleOwner)
	if err != nil {
		return nil, err
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = RoleManager
	}
	if !validRole(role) {
		return nil, models.ErrFranchiseRoleInvalid
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		return nil, models.ErrInvalidInput
	}
	userID, err := s.store.FindUserIDByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if userID == "" {
		return nil, models.ErrFranchiseUserNotFound
	}
	existing, err := s.store.GetMemberRole(ctx, f.FranchiseID, userID)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		return nil, models.ErrFranchiseMemberExists
	}
	if err := s.store.InsertMember(ctx, f.FranchiseID, userID, role); err != nil {
		return nil, err
	}
	return s.store.ListMembers(ctx, f.FranchiseID)
}

// UpdateMember : owner seulement ; la franchise garde au moins un owner.
func (s *Service) UpdateMember(ctx context.Context, actor Actor, franchiseID, userID string, req UpdateMemberRequest) ([]Member, error) {
	f, _, err := s.requireRole(ctx, actor, franchiseID, RoleOwner)
	if err != nil {
		return nil, err
	}
	role := strings.TrimSpace(req.Role)
	if !validRole(role) {
		return nil, models.ErrFranchiseRoleInvalid
	}
	err = s.store.RunInTx(ctx, func(txCtx context.Context) error {
		current, err := s.store.GetMemberRole(txCtx, f.FranchiseID, userID)
		if err != nil {
			return err
		}
		if current == "" {
			return models.ErrFranchiseMemberNotFound
		}
		if current == RoleOwner && role != RoleOwner {
			if err := s.ensureAnotherOwner(txCtx, f.FranchiseID); err != nil {
				return err
			}
		}
		return s.store.UpdateMemberRole(txCtx, f.FranchiseID, userID, role)
	})
	if err != nil {
		return nil, err
	}
	return s.store.ListMembers(ctx, f.FranchiseID)
}

// RemoveMember : un owner retire n'importe qui ; un manager peut se retirer
// lui-même (quitter la franchise). La franchise garde au moins un owner.
func (s *Service) RemoveMember(ctx context.Context, actor Actor, franchiseID, userID string) ([]Member, error) {
	minRole := RoleOwner
	if userID == actor.UserID {
		minRole = RoleManager
	}
	f, _, err := s.requireRole(ctx, actor, franchiseID, minRole)
	if err != nil {
		return nil, err
	}
	err = s.store.RunInTx(ctx, func(txCtx context.Context) error {
		current, err := s.store.GetMemberRole(txCtx, f.FranchiseID, userID)
		if err != nil {
			return err
		}
		if current == "" {
			return models.ErrFranchiseMemberNotFound
		}
		if current == RoleOwner {
			if err := s.ensureAnotherOwner(txCtx, f.FranchiseID); err != nil {
				return err
			}
		}
		return s.store.DeleteMember(txCtx, f.FranchiseID, userID)
	})
	if err != nil {
		return nil, err
	}
	return s.store.ListMembers(ctx, f.FranchiseID)
}

func (s *Service) ensureAnotherOwner(ctx context.Context, franchiseID string) error {
	owners, err := s.store.CountOwners(ctx, franchiseID)
	if err != nil {
		return err
	}
	if owners <= 1 {
		return models.ErrFranchiseLastOwner
	}
	return nil
}

// --- Cache ---

func (s *Service) invalidateHub(ctx context.Context, slugs ...string) {
	if s.cache == nil {
		return
	}
	for _, slug := range slugs {
		if slug != "" {
			s.cache.Delete(ctx, models.ScannorderFranchiseHub+slug)
		}
	}
}

func (s *Service) invalidateMerchant(ctx context.Context, merchantID string) {
	if s.cache != nil && merchantID != "" {
		s.cache.InvalidateMerchantStatusCache(ctx, merchantID)
	}
}

func (s *Service) invalidateMerchantsOf(ctx context.Context, franchiseID string) {
	ids, err := s.store.MerchantIDsOf(ctx, franchiseID)
	if err != nil {
		return // best-effort : le cache merchant expire en 2 min
	}
	for _, id := range ids {
		s.invalidateMerchant(ctx, id)
	}
}

// --- Validation ---

func normalizeName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" || utf8.RuneCountInString(n) > maxNameLen {
		return "", models.ErrFranchiseNameInvalid
	}
	return n, nil
}

func normalizeSlug(slug string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(slug))
	if len(s) < minSlugLen || len(s) > maxSlugLen || !slugPattern.MatchString(s) {
		return "", models.ErrFranchiseSlugInvalid
	}
	return s, nil
}

func validRole(role string) bool {
	return role == RoleOwner || role == RoleManager
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func uniqueTrimmed(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
