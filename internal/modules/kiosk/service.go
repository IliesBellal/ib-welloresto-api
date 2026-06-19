package kiosk

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/modules/orders"
	"welloresto-api/internal/modules/upsell"
	"welloresto-api/internal/utils/dbutils"
	"welloresto-api/internal/utils/security"
)

type Service struct {
	cfg                Config
	repo               *Repository
	db                 *sql.DB
	redis              *redisclient.Client
	menuService        *menu.MenuService
	ordersService      *orders.OrdersService
	ordersLifeCycleSvc *order_life_cycle.OrdersLifeCycleService
	upsellService      *upsell.Service
	notificationSvc    *notification.NotificationService
}

func NewService(
	cfg Config,
	repo *Repository,
	db *sql.DB,
	redis *redisclient.Client,
	menuService *menu.MenuService,
	ordersService *orders.OrdersService,
	ordersLifeCycleSvc *order_life_cycle.OrdersLifeCycleService,
	upsellService *upsell.Service,
	notificationSvc *notification.NotificationService,
) *Service {
	return &Service{
		cfg:                cfg,
		repo:               repo,
		db:                 db,
		redis:              redis,
		menuService:        menuService,
		ordersService:      ordersService,
		ordersLifeCycleSvc: ordersLifeCycleSvc,
		upsellService:      upsellService,
		notificationSvc:    notificationSvc,
	}
}

// kioskCreatedBy / kioskCashRegisterID marquent les commandes créées depuis
// une borne — même convention que "SCANNORDER" pour les commandes ScanNOrder
// (voir scannorder.CreateOrderSNO). orders.kiosk_id (colonne dédiée, migration
// 038) porte en plus l'identité précise de la borne.
const (
	kioskCreatedBy    = "KIOSK"
	kioskCashRegister = "KIOSK"
)

// EnrollDevice valide un code d'enrôlement et crée la borne + son premier
// couple de tokens. Tout se passe dans une transaction : création de la
// borne, marquage du code comme utilisé, création du refresh token.
func (s *Service) EnrollDevice(ctx context.Context, req EnrollRequest, ip string) (*EnrollResponse, error) {
	codeHash := security.HashPIN(req.EnrollmentCode, s.cfg.Pepper)

	code, err := s.repo.GetEnrollmentCodeByHash(ctx, codeHash)
	if err != nil {
		return nil, err
	}
	if code == nil {
		return nil, models.ErrKioskEnrollmentCodeInvalid
	}
	if code.UsedAt != nil {
		return nil, models.ErrKioskEnrollmentCodeUsed
	}
	if time.Now().UTC().After(code.ExpiresAt) {
		return nil, models.ErrKioskEnrollmentCodeExpired
	}

	maxKiosks, err := s.repo.GetMerchantMaxKiosks(ctx, code.MerchantID)
	if err != nil {
		return nil, err
	}
	activeCount, err := s.repo.GetActiveKioskCount(ctx, code.MerchantID)
	if err != nil {
		return nil, err
	}
	if activeCount >= maxKiosks {
		return nil, models.ErrKioskMaxKiosksReached
	}

	kioskID := helpers.GeneratePrefixedID(helpers.KioskIDPrefix)
	deviceTokenID := helpers.GeneratePrefixedID(helpers.KioskDeviceTokenIDPrefix)
	refreshToken, err := helpers.GenerateToken(32)
	if err != nil {
		return nil, fmt.Errorf("kiosk enroll: generate refresh token: %w", err)
	}
	refreshTokenHash := security.HashPIN(refreshToken, s.cfg.Pepper)
	refreshExpiresAt := time.Now().UTC().AddDate(0, 0, s.cfg.DeviceRefreshTokenTTLDays)

	var kiosk *KioskRow
	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		kiosk, err = s.repo.CreateKiosk(txCtx, kioskID, code.MerchantID, req.Name, req.HardwareModel, req.OSVersion)
		if err != nil {
			return err
		}
		if err := s.repo.MarkEnrollmentCodeUsed(txCtx, code.ID, kiosk.ID); err != nil {
			return err
		}
		return s.repo.CreateDeviceToken(txCtx, deviceTokenID, kiosk.ID, refreshTokenHash, refreshExpiresAt)
	})
	if err != nil {
		return nil, err
	}

	accessToken, expiresAt, err := s.generateAccessToken(kiosk.ID, kiosk.MerchantID)
	if err != nil {
		return nil, err
	}

	return &EnrollResponse{
		KioskID:      kiosk.ID,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
	}, nil
}

// RefreshDeviceToken échange un refresh token valide contre une nouvelle
// paire access/refresh (rotation : l'ancien refresh token est révoqué).
func (s *Service) RefreshDeviceToken(ctx context.Context, refreshToken string) (*RefreshTokenResponse, error) {
	tokenHash := security.HashPIN(refreshToken, s.cfg.Pepper)

	deviceToken, err := s.repo.GetDeviceTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, err
	}
	if deviceToken == nil {
		return nil, models.ErrKioskDeviceTokenInvalid
	}
	if deviceToken.RevokedAt != nil {
		return nil, models.ErrKioskDeviceTokenInvalid
	}
	if time.Now().UTC().After(deviceToken.ExpiresAt) {
		return nil, models.ErrKioskDeviceTokenInvalid
	}

	kiosk, err := s.repo.GetKioskByID(ctx, deviceToken.KioskID)
	if err != nil {
		return nil, err
	}
	if kiosk == nil {
		return nil, models.ErrKioskNotFound
	}
	if kiosk.Status == "revoked" {
		return nil, models.ErrKioskRevoked
	}

	newRefreshToken, err := helpers.GenerateToken(32)
	if err != nil {
		return nil, fmt.Errorf("kiosk refresh: generate refresh token: %w", err)
	}
	newRefreshTokenHash := security.HashPIN(newRefreshToken, s.cfg.Pepper)
	newRefreshExpiresAt := time.Now().UTC().AddDate(0, 0, s.cfg.DeviceRefreshTokenTTLDays)
	newTokenID := helpers.GeneratePrefixedID(helpers.KioskDeviceTokenIDPrefix)

	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		return s.repo.RotateDeviceToken(txCtx, deviceToken.ID, newTokenID, kiosk.ID, newRefreshTokenHash, newRefreshExpiresAt)
	})
	if err != nil {
		return nil, err
	}

	accessToken, expiresAt, err := s.generateAccessToken(kiosk.ID, kiosk.MerchantID)
	if err != nil {
		return nil, err
	}

	return &RefreshTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
	}, nil
}

// ValidateAccessToken vérifie la signature et l'expiration de l'access
// token, sans accès base de données (token auto-porteur, signé HMAC-SHA256,
// non persisté — voir docs/KIOSK_DECISIONS.md section G.1). C'est le
// middleware.KioskAuth qui appelle cette méthode sur chaque requête protégée.
func (s *Service) ValidateAccessToken(ctx context.Context, accessToken string) (*AuthenticatedKiosk, error) {
	kioskID, merchantID, expiresAt, err := s.parseAccessToken(accessToken)
	if err != nil {
		return nil, models.ErrKioskDeviceTokenInvalid
	}
	if time.Now().UTC().After(expiresAt) {
		return nil, models.ErrKioskDeviceTokenInvalid
	}

	return &AuthenticatedKiosk{KioskID: kioskID, MerchantID: merchantID}, nil
}

// RecordHeartbeat met à jour le dernier contact connu de la borne.
func (s *Service) RecordHeartbeat(ctx context.Context, kiosk *AuthenticatedKiosk, req HeartbeatRequest, ip string) (*HeartbeatResponse, error) {
	row, err := s.repo.GetKioskByIDForMerchant(ctx, kiosk.MerchantID, kiosk.KioskID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, models.ErrKioskNotFound
	}
	if row.Status == "revoked" {
		return nil, models.ErrKioskRevoked
	}

	if err := s.repo.UpdateKioskHeartbeat(ctx, row.ID, req.AppVersion, ip); err != nil {
		return nil, err
	}

	return &HeartbeatResponse{Status: "ok"}, nil
}

// RevokeKiosk révoque immédiatement une borne : tous ses refresh tokens
// sont invalidés et son statut passe à "revoked". L'access token déjà
// émis (non révocable, auto-porteur) reste valide au maximum
// AccessTokenTTLMinutes — voir docs/KIOSK_DECISIONS.md section G.1.
func (s *Service) RevokeKiosk(ctx context.Context, merchantID, kioskID string) error {
	kiosk, err := s.repo.GetKioskByIDForMerchant(ctx, merchantID, kioskID)
	if err != nil {
		return err
	}
	if kiosk == nil {
		return models.ErrKioskNotFound
	}

	return dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.RevokeAllDeviceTokens(txCtx, kiosk.ID); err != nil {
			return err
		}
		return s.repo.UpdateKioskStatus(txCtx, kiosk.ID, "revoked")
	})
}

// ---- Back-office (admin) ----

// GenerateEnrollmentCode crée un code d'enrôlement à usage unique pour le
// merchant, après vérification du quota de bornes actives.
func (s *Service) GenerateEnrollmentCode(ctx context.Context, merchantID, createdByUserID string) (*GenerateEnrollmentCodeResponse, error) {
	maxKiosks, err := s.repo.GetMerchantMaxKiosks(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	activeCount, err := s.repo.GetActiveKioskCount(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if activeCount >= maxKiosks {
		return nil, models.ErrKioskMaxKiosksReached
	}

	code, err := generateEnrollmentCode()
	if err != nil {
		return nil, fmt.Errorf("kiosk admin: generate enrollment code: %w", err)
	}
	codeHash := security.HashPIN(code, s.cfg.Pepper)
	expiresAt := time.Now().UTC().Add(time.Duration(s.cfg.EnrollmentCodeTTLMinutes) * time.Minute)
	codeID := helpers.GeneratePrefixedID(helpers.KioskEnrollmentCodeIDPrefix)

	if err := s.repo.CreateEnrollmentCode(ctx, codeID, merchantID, codeHash, expiresAt, createdByUserID); err != nil {
		return nil, err
	}

	return &GenerateEnrollmentCodeResponse{Code: code, ExpiresAt: expiresAt.Format(time.RFC3339)}, nil
}

// ListKioskDevices liste les bornes enrôlées d'un merchant.
func (s *Service) ListKioskDevices(ctx context.Context, merchantID string) (*ListKioskDevicesResponse, error) {
	rows, err := s.repo.ListKiosksByMerchant(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	devices := make([]KioskDeviceResponse, 0, len(rows))
	for i := range rows {
		devices = append(devices, toKioskDeviceResponse(&rows[i]))
	}
	return &ListKioskDevicesResponse{Devices: devices}, nil
}

// toKioskDeviceResponse mappe une ligne kiosks vers la réponse exposée au
// back-office — factorisé pour rester identique entre liste et détail.
func toKioskDeviceResponse(row *KioskRow) KioskDeviceResponse {
	var lastHeartbeat *string
	if row.LastHeartbeatAt != nil {
		v := row.LastHeartbeatAt.Format(time.RFC3339)
		lastHeartbeat = &v
	}
	return KioskDeviceResponse{
		KioskID:         row.ID,
		Name:            row.Name,
		Status:          row.Status,
		AppVersion:      row.AppVersion,
		HardwareModel:   row.HardwareModel,
		OSVersion:       row.OSVersion,
		LastHeartbeatAt: lastHeartbeat,
		LastIP:          row.LastIP,
		Enabled:         row.Enabled,
		CreatedAt:       row.CreatedAt.Format(time.RFC3339),
	}
}

// GetKioskDevice retourne le détail d'une borne, scopée au merchant
// authentifié — 404 si non trouvée ou appartenant à un autre merchant.
func (s *Service) GetKioskDevice(ctx context.Context, merchantID, kioskID string) (*KioskDeviceResponse, error) {
	kiosk, err := s.repo.GetKioskByIDForMerchant(ctx, merchantID, kioskID)
	if err != nil {
		return nil, err
	}
	if kiosk == nil {
		return nil, models.ErrKioskNotFound
	}
	resp := toKioskDeviceResponse(kiosk)
	return &resp, nil
}

// UpdateKioskDeviceName renomme une borne (seul champ éditable pour
// l'instant côté back-office).
func (s *Service) UpdateKioskDeviceName(ctx context.Context, merchantID, kioskID, name string) (*KioskDeviceResponse, error) {
	if strings.TrimSpace(name) == "" {
		return nil, models.ErrInvalidInput
	}

	kiosk, err := s.repo.GetKioskByIDForMerchant(ctx, merchantID, kioskID)
	if err != nil {
		return nil, err
	}
	if kiosk == nil {
		return nil, models.ErrKioskNotFound
	}

	if err := s.repo.UpdateKioskName(ctx, kiosk.ID, name); err != nil {
		return nil, err
	}
	kiosk.Name = name
	resp := toKioskDeviceResponse(kiosk)
	return &resp, nil
}

// EnableKioskDevice repasse une borne en "active" — vérifie le quota de
// bornes actives du merchant avant d'autoriser le passage (sauf si déjà
// active, no-op de quota).
func (s *Service) EnableKioskDevice(ctx context.Context, merchantID, kioskID string) (*KioskDeviceResponse, error) {
	kiosk, err := s.repo.GetKioskByIDForMerchant(ctx, merchantID, kioskID)
	if err != nil {
		return nil, err
	}
	if kiosk == nil {
		return nil, models.ErrKioskNotFound
	}

	if kiosk.Status != "active" {
		maxKiosks, err := s.repo.GetMerchantMaxKiosks(ctx, merchantID)
		if err != nil {
			return nil, err
		}
		activeCount, err := s.repo.GetActiveKioskCount(ctx, merchantID)
		if err != nil {
			return nil, err
		}
		if activeCount >= maxKiosks {
			return nil, models.ErrKioskMaxKiosksReached
		}
	}

	if err := s.repo.SetKioskStatusEnabled(ctx, kiosk.ID, "active", true); err != nil {
		return nil, err
	}
	kiosk.Status, kiosk.Enabled = "active", true
	resp := toKioskDeviceResponse(kiosk)
	return &resp, nil
}

// DisableKioskDevice passe une borne en "inactive" sans révoquer ses tokens
// — elle peut se réactiver (heartbeat/refresh restent fonctionnels tant que
// le statut n'est pas "revoked", voir RefreshDeviceToken/RecordHeartbeat).
func (s *Service) DisableKioskDevice(ctx context.Context, merchantID, kioskID string) (*KioskDeviceResponse, error) {
	kiosk, err := s.repo.GetKioskByIDForMerchant(ctx, merchantID, kioskID)
	if err != nil {
		return nil, err
	}
	if kiosk == nil {
		return nil, models.ErrKioskNotFound
	}

	if err := s.repo.SetKioskStatusEnabled(ctx, kiosk.ID, "inactive", false); err != nil {
		return nil, err
	}
	kiosk.Status, kiosk.Enabled = "inactive", false
	resp := toKioskDeviceResponse(kiosk)
	return &resp, nil
}

// ListEnrollmentCodes liste les codes d'enrôlement en attente (non utilisés,
// non expirés) d'un merchant — jamais le code en clair ni son hash.
func (s *Service) ListEnrollmentCodes(ctx context.Context, merchantID string) (*ListEnrollmentCodesResponse, error) {
	rows, err := s.repo.ListPendingEnrollmentCodes(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	codes := make([]EnrollmentCodeListItem, 0, len(rows))
	for _, row := range rows {
		var usedAt *string
		if row.UsedAt != nil {
			v := row.UsedAt.Format(time.RFC3339)
			usedAt = &v
		}
		codes = append(codes, EnrollmentCodeListItem{
			ID:        row.ID,
			CreatedAt: row.CreatedAt.Format(time.RFC3339),
			ExpiresAt: row.ExpiresAt.Format(time.RFC3339),
			UsedAt:    usedAt,
		})
	}
	return &ListEnrollmentCodesResponse{Codes: codes}, nil
}

// RevokeEnrollmentCode supprime un code d'enrôlement avant son utilisation.
// 409 si déjà utilisé, 404 si introuvable ou déjà expiré (un code expiré
// n'a plus d'existence actionnable côté back-office).
func (s *Service) RevokeEnrollmentCode(ctx context.Context, merchantID, codeID string) error {
	code, err := s.repo.GetEnrollmentCodeByID(ctx, merchantID, codeID)
	if err != nil {
		return err
	}
	if code == nil {
		return models.ErrKioskEnrollmentCodeNotFound
	}
	if code.UsedAt != nil {
		return models.ErrKioskEnrollmentCodeAlreadyUsed
	}
	if time.Now().UTC().After(code.ExpiresAt) {
		return models.ErrKioskEnrollmentCodeNotFound
	}

	return s.repo.DeleteEnrollmentCode(ctx, code.ID)
}

// GetSettings récupère les paramètres Kiosk d'un merchant (valeurs par
// défaut si jamais configurés).
func (s *Service) GetSettings(ctx context.Context, merchantID string) (*KioskSettingsResponse, error) {
	row, err := s.repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	return &KioskSettingsResponse{
		FulfillmentDineIn:    row.FulfillmentDineIn,
		FulfillmentTakeAway:  row.FulfillmentTakeAway,
		ForceFulfillmentType: row.ForceFulfillmentType,
		PagerNumberRequired:  row.PagerNumberRequired,
		ShowAllergens:        row.ShowAllergens,
		InactivityTimeoutSec: row.InactivityTimeoutSec,
		UpsellEnabled:        row.UpsellEnabled,
		PayAtCounterEnabled:  row.PayAtCounterEnabled,
		CardPaymentEnabled:   row.CardPaymentEnabled,
		LogoURL:              row.LogoURL,
		IdleImageURL:         row.IdleImageURL,
		IdleVideoURL:         row.IdleVideoURL,
		PrimaryColor:         row.PrimaryColor,
	}, nil
}

// UpdateSettings applique un patch partiel sur les paramètres Kiosk du
// merchant (les champs nil ne sont pas modifiés).
func (s *Service) UpdateSettings(ctx context.Context, merchantID string, req UpdateKioskSettingsRequest) (*KioskSettingsResponse, error) {
	current, err := s.repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	if req.FulfillmentDineIn != nil {
		current.FulfillmentDineIn = *req.FulfillmentDineIn
	}
	if req.FulfillmentTakeAway != nil {
		current.FulfillmentTakeAway = *req.FulfillmentTakeAway
	}
	if req.ForceFulfillmentType != nil {
		current.ForceFulfillmentType = req.ForceFulfillmentType
	}
	if req.PagerNumberRequired != nil {
		current.PagerNumberRequired = *req.PagerNumberRequired
	}
	if req.ShowAllergens != nil {
		current.ShowAllergens = *req.ShowAllergens
	}
	if req.InactivityTimeoutSec != nil {
		current.InactivityTimeoutSec = *req.InactivityTimeoutSec
	}
	if req.UpsellEnabled != nil {
		current.UpsellEnabled = *req.UpsellEnabled
	}
	if req.PayAtCounterEnabled != nil {
		current.PayAtCounterEnabled = *req.PayAtCounterEnabled
	}
	if req.CardPaymentEnabled != nil {
		current.CardPaymentEnabled = *req.CardPaymentEnabled
	}
	if req.PrimaryColor != nil {
		if err := validatePrimaryColor(req.PrimaryColor); err != nil {
			return nil, err
		}
		current.PrimaryColor = req.PrimaryColor
	}

	if err := s.repo.UpsertSettings(ctx, current); err != nil {
		return nil, err
	}

	return s.GetSettings(ctx, merchantID)
}

// validatePrimaryColor n'accepte que nil ou un hex couleur #RRGGBB —
// logo_url/idle_image_url ne passent jamais par UpdateSettings (voir
// UpdateKioskSettingsRequest), donc aucune validation équivalente n'est
// nécessaire ici pour ces champs.
var hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func validatePrimaryColor(color *string) error {
	if color == nil {
		return nil
	}
	if !hexColorPattern.MatchString(*color) {
		return models.ErrKioskInvalidColor
	}
	return nil
}

// SetLogoURL persiste l'URL du logo après upload R2 (voir AdminHandler) —
// upsert sur kiosk_settings, mêmes valeurs par défaut que GetSettings pour
// les champs non encore configurés.
func (s *Service) SetLogoURL(ctx context.Context, merchantID, url string) (*KioskSettingsResponse, error) {
	current, err := s.repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	current.LogoURL = &url
	if err := s.repo.UpsertSettings(ctx, current); err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, merchantID)
}

// SetIdleImageURL persiste l'URL de l'image de veille après upload R2.
func (s *Service) SetIdleImageURL(ctx context.Context, merchantID, url string) (*KioskSettingsResponse, error) {
	current, err := s.repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	current.IdleImageURL = &url
	if err := s.repo.UpsertSettings(ctx, current); err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, merchantID)
}

// SetIdleVideoURL persiste l'URL de la vidéo de veille après upload R2.
func (s *Service) SetIdleVideoURL(ctx context.Context, merchantID, url string) (*KioskSettingsResponse, error) {
	current, err := s.repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	current.IdleVideoURL = &url
	if err := s.repo.UpsertSettings(ctx, current); err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, merchantID)
}

// ---- Incrément 2 : menu, upsell, pricing, commandes ----

// GetMenu retourne le menu filtré sur is_available_on_kiosk, avec un ETag
// (MD5 du JSON sérialisé) permettant au handler de répondre 304 sur un
// If-None-Match identique.
func (s *Service) GetMenu(ctx context.Context, merchantID string) (*KioskMenuResponse, error) {
	rawMenu, err := s.menuService.GetMenuFromMerchantIdWithMarketing(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	availability, err := s.repo.GetKioskProductAvailabilityMap(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	categories := make([]KioskCategory, 0, len(rawMenu.ProductsTypes))
	for _, pt := range rawMenu.ProductsTypes {
		products := flattenKioskProducts(pt.Products, availability)
		if len(products) == 0 {
			continue
		}

		categoryID := ""
		if pt.CategoryID != nil {
			categoryID = *pt.CategoryID
		}

		categories = append(categories, KioskCategory{
			ID:        categoryID,
			Name:      pt.CategoryName,
			SortOrder: pt.Order,
			Products:  products,
		})
	}

	resp := &KioskMenuResponse{Categories: categories}
	if serialized, err := json.Marshal(categories); err == nil {
		sum := md5.Sum(serialized)
		resp.ETag = fmt.Sprintf("%x", sum)
	}

	return resp, nil
}

// flattenKioskProducts reproduit la logique de scannorder.ComputeGetMenu :
// un groupe de produits non disponible est remplacé par ses sous-produits
// disponibles, sauf qu'ici la disponibilité regardée est is_available_on_kiosk
// (table products, colonne dédiée — migration 038) au lieu de
// is_available_on_sno. Implémenté ici plutôt que dans menuService pour ne
// jamais modifier ce module existant (voir docs/KIOSK_DECISIONS.md).
func flattenKioskProducts(products []models.ProductEntry, availability map[string]bool) []KioskProduct {
	out := make([]KioskProduct, 0, len(products))

	var toAdd []models.ProductEntry
	for _, p := range products {
		isGroup := p.IsProductGroup != nil && *p.IsProductGroup
		if !isGroup && availability[p.ProductID] {
			out = append(out, mapProductEntryToKioskProduct(&p))
			continue
		}
		if len(p.SubProducts) > 0 {
			toAdd = append(toAdd, p.SubProducts...)
		}
	}

	for _, sp := range toAdd {
		if availability[sp.ProductID] {
			out = append(out, mapProductEntryToKioskProduct(&sp))
		}
	}

	return out
}

func mapProductEntryToKioskProduct(p *models.ProductEntry) KioskProduct {
	description := ""
	if p.Description != nil {
		description = *p.Description
	}
	imageURL := ""
	if p.ImageURL != nil {
		imageURL = *p.ImageURL
	}
	available := p.Available == nil || *p.Available

	allergens := make([]string, 0, len(p.Allergens))
	for _, a := range p.Allergens {
		allergens = append(allergens, a.Name)
	}
	tags := make([]string, 0, len(p.Tags))
	for _, t := range p.Tags {
		tags = append(tags, t.Name)
	}

	var modifierGroups []KioskModifierGroup
	for _, attr := range p.Configuration.Attributes {
		options := make([]KioskModifierOption, 0, len(attr.Options))
		for _, opt := range attr.Options {
			options = append(options, KioskModifierOption{
				ID:              opt.ID,
				Name:            opt.Title,
				PriceDeltaCents: opt.ExtraPrice,
			})
		}
		modifierGroups = append(modifierGroups, KioskModifierGroup{
			ID:       attr.ID,
			Name:     attr.Title,
			Min:      attr.MinOptions,
			Max:      attr.MaxOptions,
			Required: attr.MinOptions > 0,
			Options:  options,
		})
	}

	return KioskProduct{
		ID:               p.ProductID,
		Name:             p.Name,
		Description:      description,
		PriceCents:       p.Price,
		ImageURL:         imageURL,
		Available:        available,
		AvailableOnKiosk: true,
		Allergens:        allergens,
		Tags:             tags,
		ModifierGroups:   modifierGroups,
	}
}

// GetProduct retourne le détail d'un produit, en rejetant explicitement les
// produits désactivés sur la borne (is_available_on_kiosk = FALSE), même
// s'ils existent et sont visibles sur d'autres canaux.
func (s *Service) GetProduct(ctx context.Context, merchantID, productID string) (*KioskProduct, error) {
	available, err := s.repo.GetAvailableKioskProductIDs(ctx, merchantID, []string{productID})
	if err != nil {
		return nil, err
	}
	if !available[productID] {
		return nil, models.ErrKioskProductUnavailable
	}

	product, err := s.menuService.GetProductFromMerchantId(ctx, merchantID, productID)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return nil, models.ErrKioskProductNotFound
	}

	mapped := mapProductEntryToKioskProduct(product)
	return &mapped, nil
}

// GetUpsellSuggestions délègue au service Apriori existant (upsell.Service,
// même moteur que orders.GetUpsell) puis filtre is_available_on_kiosk et
// l'appartenance au panier en cours, avant de plafonner à 3 suggestions.
func (s *Service) GetUpsellSuggestions(ctx context.Context, merchantID string, cartProductIDs []string) (*KioskUpsellResponse, error) {
	if len(cartProductIDs) == 0 {
		return &KioskUpsellResponse{Suggestions: []KioskUpsellSuggestion{}, Source: upsell.SourceDisabled}, nil
	}

	cartProducts := make([]models.ProductEntry, 0, len(cartProductIDs))
	inCart := make(map[string]bool, len(cartProductIDs))
	for _, id := range cartProductIDs {
		qty := 1
		cartProducts = append(cartProducts, models.ProductEntry{ProductID: id, Quantity: &qty})
		inCart[id] = true
	}

	result, err := s.upsellService.GenerateUpsell(ctx, merchantID, cartProducts)
	if err != nil {
		return &KioskUpsellResponse{Suggestions: []KioskUpsellSuggestion{}, Source: "error_fallback"}, nil
	}

	candidateIDs := make([]string, 0, len(result.Suggestions))
	for _, sugg := range result.Suggestions {
		if !inCart[sugg.ProductID] {
			candidateIDs = append(candidateIDs, sugg.ProductID)
		}
	}

	available, err := s.repo.GetAvailableKioskProductIDs(ctx, merchantID, candidateIDs)
	if err != nil {
		return nil, err
	}

	source := "featured_fallback"
	if result.Source == upsell.SourcePattern || result.Source == upsell.SourceCachedPattern || result.Source == upsell.SourceLLM || result.Source == upsell.SourceCachedLLM {
		source = "apriori"
	}

	suggestions := make([]KioskUpsellSuggestion, 0, 3)
	for _, sugg := range result.Suggestions {
		if len(suggestions) >= 3 {
			break
		}
		if inCart[sugg.ProductID] || !available[sugg.ProductID] {
			continue
		}

		product, err := s.menuService.GetProductFromMerchantId(ctx, merchantID, sugg.ProductID)
		if err != nil || product == nil {
			continue
		}

		imageURL := ""
		if product.ImageURL != nil {
			imageURL = *product.ImageURL
		}

		suggestions = append(suggestions, KioskUpsellSuggestion{
			ProductID:  sugg.ProductID,
			Name:       product.Name,
			PriceCents: product.Price,
			ImageURL:   imageURL,
			Reason:     sugg.Title,
		})
	}

	return &KioskUpsellResponse{Suggestions: suggestions, Source: source}, nil
}

// kioskFulfillmentToOrderType traduit le fulfillment_type Kiosk
// (DINE_IN/TAKE_AWAY) vers order_type ("IN"/"TAKE_AWAY"), la convention déjà
// utilisée par scannorder/orders. Distinct de orders.fulfillment_type
// (DELIVERY_BY_RESTAURANT, etc.), qui ne concerne que la livraison.
func kioskFulfillmentToOrderType(fulfillmentType string) (string, error) {
	switch fulfillmentType {
	case "DINE_IN":
		return "IN", nil
	case "TAKE_AWAY":
		return "TAKE_AWAY", nil
	default:
		return "", models.ErrKioskFulfillmentTypeInvalid
	}
}

// checkFulfillmentEnabled vérifie que le mode demandé est activé dans
// kiosk_settings pour ce merchant.
func checkFulfillmentEnabled(settings *KioskSettingsRow, fulfillmentType string) error {
	switch fulfillmentType {
	case "DINE_IN":
		if !settings.FulfillmentDineIn {
			return models.ErrKioskFulfillmentTypeDisabled
		}
	case "TAKE_AWAY":
		if !settings.FulfillmentTakeAway {
			return models.ErrKioskFulfillmentTypeDisabled
		}
	}
	return nil
}

// buildOrderProducts valide chaque item (existence + is_available_on_kiosk
// + options réellement configurées) puis construit les OrderProductPayload
// envoyés à ordersService.ComputePricing — qui recalculera lui-même prix et
// TVA depuis la base (jamais depuis le client, voir
// orders.OrdersService.buildSelectedProducts). C'est l'équivalent Kiosk de
// scannorder.validateAndCleanPricingPayload : ce qui change ici, c'est qu'on
// valide l'existence/disponibilité AVANT envoi plutôt que de nettoyer après,
// parce que ComputePricing ne connaît pas la notion de canal Kiosk et
// attribuerait silencieusement un prix de 0 à un product_id inconnu.
func (s *Service) buildOrderProducts(ctx context.Context, merchantID string, items []KioskOrderItem) ([]models.OrderProductPayload, error) {
	if len(items) == 0 {
		return nil, models.ErrCartEmpty
	}

	productIDs := make([]string, 0, len(items))
	optionIDSet := map[string]bool{}
	for _, item := range items {
		if item.Quantity <= 0 {
			return nil, models.ErrInvalidInput
		}
		productIDs = append(productIDs, item.ProductID)
		for _, optID := range item.SelectedOptionIDs {
			optionIDSet[optID] = true
		}
	}

	availableProducts, err := s.repo.GetAvailableKioskProductIDs(ctx, merchantID, productIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range productIDs {
		if !availableProducts[id] {
			return nil, models.ErrKioskProductUnavailable
		}
	}

	optionIDs := make([]string, 0, len(optionIDSet))
	for id := range optionIDSet {
		optionIDs = append(optionIDs, id)
	}
	existingOptions, err := s.repo.GetExistingConfigurationOptionIDs(ctx, optionIDs)
	if err != nil {
		return nil, err
	}

	payloads := make([]models.OrderProductPayload, 0, len(items))
	for _, item := range items {
		var config *models.ProductConfiguration
		if len(item.SelectedOptionIDs) > 0 {
			options := make([]models.ConfigurationOption, 0, len(item.SelectedOptionIDs))
			for _, optID := range item.SelectedOptionIDs {
				if !existingOptions[optID] {
					return nil, models.ErrInvalidInput
				}
				options = append(options, models.ConfigurationOption{ID: optID, Selected: true})
			}
			config = &models.ProductConfiguration{
				Attributes: []models.ConfigurationAttribute{{ID: "kiosk-options", Options: options}},
			}
		}

		var comment *models.OrderItemCommentPayload
		if item.Notes != "" {
			comment = &models.OrderItemCommentPayload{UserID: kioskCreatedBy, Content: item.Notes}
		}

		var without []*models.OrderWithoutPayload
		for _, componentID := range item.WithoutComponentIDs {
			without = append(without, &models.OrderWithoutPayload{ComponentID: componentID})
		}

		payloads = append(payloads, models.OrderProductPayload{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			Config:    config,
			Comment:   comment,
			Without:   without,
		})
	}

	return payloads, nil
}

// computeOrderPricing construit la requête de pricing puis délègue
// entièrement à ordersService.ComputePricing — seule source de vérité pour
// les prix/TVA/remises (voir internal/modules/orders/service.go).
func (s *Service) computeOrderPricing(ctx context.Context, merchantID, fulfillmentType string, items []KioskOrderItem, discountCode *string) (*models.PricingResponse, error) {
	orderType, err := kioskFulfillmentToOrderType(fulfillmentType)
	if err != nil {
		return nil, err
	}

	products, err := s.buildOrderProducts(ctx, merchantID, items)
	if err != nil {
		return nil, err
	}

	pricingReq := &models.PricingRequest{
		MerchantID: merchantID,
		Order: &models.OrderRequest{
			OrderType: orderType,
			Products:  products,
		},
	}
	if discountCode != nil {
		pricingReq.DiscountCode = *discountCode
	}

	return s.ordersService.ComputePricing(ctx, pricingReq)
}

// ComputePricing prévisualise le total d'un panier (écran borne avant
// validation), sans créer de commande.
func (s *Service) ComputePricing(ctx context.Context, merchantID string, req KioskPricingRequest) (*KioskPricingResponse, error) {
	items := make([]KioskOrderItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, KioskOrderItem{ProductID: item.ProductID, Quantity: item.Quantity, SelectedOptionIDs: item.SelectedOptionIDs, Notes: item.Notes})
	}

	pricing, err := s.computeOrderPricing(ctx, merchantID, req.FulfillmentType, items, req.DiscountCode)
	if err != nil {
		return nil, err
	}
	if pricing.Status != "success" {
		return nil, models.ErrInvalidInput
	}

	return pricingResponseToKiosk(pricing), nil
}

func pricingResponseToKiosk(pricing *models.PricingResponse) *KioskPricingResponse {
	var itemsTotal int64
	for _, p := range pricing.OrderRequest.Order.Products {
		itemsTotal += int64(p.Price) * int64(p.Quantity)
	}

	totalCents := int64(pricing.OrderRequest.Order.TTC)
	taxCents := int64(pricing.OrderRequest.Order.TVA)
	discountCents := itemsTotal - totalCents
	if discountCents < 0 {
		discountCents = 0
	}

	return &KioskPricingResponse{
		ItemsTotalCents: itemsTotal,
		DiscountCents:   discountCents,
		TaxCents:        taxCents,
		TotalCents:      totalCents,
	}
}

// mapMerchantApprovalToKioskStatus traduit orders.merchant_approval — déjà
// utilisé partout ailleurs dans le projet (KDS, rapports, back-office) — en
// un statut Kiosk lisible côté borne. "pending_counter_payment" n'est PAS
// une nouvelle valeur stockée en base : c'est le nom Kiosk de la valeur
// existante "PENDING_APPROVAL" (voir docs/KIOSK_DECISIONS.md, incrément 2).
func mapMerchantApprovalToKioskStatus(order *models.Order) string {
	if order.State != nil && (*order.State == "CLOSED" || *order.State == "DONE") {
		return "closed"
	}
	switch order.MerchantApproval {
	case "PENDING_APPROVAL":
		return "pending_counter_payment"
	case "ACCEPTED":
		return "accepted"
	default:
		return strings.ToLower(order.MerchantApproval)
	}
}

// CreateKioskOrder crée une commande borne en mode "payer au comptoir" :
// pas de paiement en ligne, MerchantApproval reste PENDING_APPROVAL jusqu'à
// ce que le staff encaisse au comptoir (voir ConfirmCounterPayment).
// Idempotent via idempotency_key (Redis, TTL 10 min, scope par borne).
func (s *Service) CreateKioskOrder(ctx context.Context, req CreateKioskOrderRequest, kiosk AuthenticatedKiosk) (*CreateKioskOrderResponse, error) {
	if req.PaymentMethod != "" && req.PaymentMethod != "pay_at_counter" {
		return nil, models.ErrInvalidInput
	}

	idemKey := ""
	if req.IdempotencyKey != "" {
		idemKey = fmt.Sprintf("kiosk:idempotency:%s:%s", kiosk.KioskID, req.IdempotencyKey)
		if s.redis != nil {
			if cached, found := s.redis.Get(ctx, idemKey); found {
				var resp CreateKioskOrderResponse
				if err := json.Unmarshal([]byte(cached), &resp); err == nil {
					return &resp, nil
				}
			}
		}
	}

	settings, err := s.repo.GetKioskSettings(ctx, kiosk.MerchantID)
	if err != nil {
		return nil, err
	}
	if !settings.PayAtCounterEnabled {
		return nil, models.ErrKioskPayAtCounterDisabled
	}
	if err := checkFulfillmentEnabled(settings, req.FulfillmentType); err != nil {
		return nil, err
	}

	pricing, err := s.computeOrderPricing(ctx, kiosk.MerchantID, req.FulfillmentType, req.Items, req.DiscountCode)
	if err != nil {
		return nil, err
	}
	if pricing.Status != "success" {
		return nil, models.ErrInvalidInput
	}

	orderType, _ := kioskFulfillmentToOrderType(req.FulfillmentType)
	orderReq := pricing.OrderRequest.Order
	orderReq.OrderType = orderType
	orderReq.MerchantApproval = "ACCEPTED"
	orderReq.CreatedBy = strPtr(kioskCreatedBy)
	orderReq.CashRegisterId = strPtr(kioskCashRegister)
	orderReq.OnlinePayment = false
	orderReq.Payments = []models.PaymentPayload{}
	if req.OrderNotes != "" {
		orderReq.Comment = strPtr(req.OrderNotes)
	}

	newOrder, err := s.ordersLifeCycleSvc.CreateOrder(ctx, &models.RequestObject{
		MerchantID: kiosk.MerchantID,
		Order:      *orderReq,
	})
	if err != nil {
		return nil, err
	}
	if newOrder.Status != "success" {
		return nil, models.ErrInvalidInput
	}

	if err := s.repo.SetKioskIDOnOrder(ctx, newOrder.OrderID, kiosk.KioskID); err != nil {
		return nil, err
	}

	displayNumber := ""
	if newOrder.OrderNum != nil {
		displayNumber = *newOrder.OrderNum
	}

	resp := &CreateKioskOrderResponse{
		OrderID:       newOrder.OrderID,
		DisplayNumber: displayNumber,
		Status:        "pending_counter_payment",
		TotalCents:    int64(orderReq.TTC),
	}

	if idemKey != "" && s.redis != nil {
		if serialized, err := json.Marshal(resp); err == nil {
			s.redis.Set(ctx, idemKey, string(serialized), 10*time.Minute)
		}
	}

	return resp, nil
}

func strPtr(s string) *string { return &s }

// ConfirmCounterPayment génère le code de retrait et le QR à afficher à
// l'écran de la borne, et notifie le merchant en temps réel qu'une commande
// attend d'être encaissée au comptoir. N'altère pas merchant_approval : la
// commande est déjà en PENDING_APPROVAL depuis sa création (voir
// CreateKioskOrder) — cet appel rend cet état visible/actionnable côté
// borne et back-office, il ne le déclenche pas une seconde fois.
func (s *Service) ConfirmCounterPayment(ctx context.Context, orderID string, kiosk AuthenticatedKiosk) (*CounterPaymentResponse, error) {
	orders, err := s.ordersService.ComputeGetOrder(ctx, kiosk.MerchantID, orderID)
	if err != nil {
		return nil, err
	}
	if orders == nil || len(orders.Orders) == 0 {
		return nil, models.ErrKioskOrderNotFound
	}
	order := orders.Orders[0]

	pickupCode := ""
	if order.OrderNum != nil {
		pickupCode = *order.OrderNum
	}

	resp := &CounterPaymentResponse{
		OrderID:       order.OrderID,
		DisplayNumber: pickupCode,
		Status:        mapMerchantApprovalToKioskStatus(&order),
		PickupCode:    pickupCode,
		QRPayload:     fmt.Sprintf("KIOSK:%s:%s", order.OrderID, pickupCode),
	}

	if s.notificationSvc != nil {
		_ = s.notificationSvc.SendNotificationAsync(kiosk.MerchantID, order.OrderID, notification.NotificationTypeOrderUpdate)
	}

	return resp, nil
}

// CancelKioskOrder annule une commande borne tant qu'elle n'est pas encore
// passée en préparation (toujours PENDING_APPROVAL). Utilise
// OrdersLifeCycleService.DeleteOrder directement (pas SetOrderDeleted, qui
// exige un user humain via middleware.UserFromContext — inadapté à un
// device). deletion_reason_id n'est pas une FK stricte dans ce projet (voir
// OrdersLifeCycleRepository.DeleteOrderLocal) : une valeur littérale dédiée
// suffit, sans dépendre d'une ligne deletion_reasons pré-configurée par
// chaque merchant.
func (s *Service) CancelKioskOrder(ctx context.Context, orderID string, kiosk AuthenticatedKiosk) error {
	orderResp, err := s.ordersService.ComputeGetOrder(ctx, kiosk.MerchantID, orderID)
	if err != nil {
		return err
	}
	if orderResp == nil || len(orderResp.Orders) == 0 {
		return models.ErrKioskOrderNotFound
	}
	order := orderResp.Orders[0]

	if order.MerchantApproval != "PENDING_APPROVAL" {
		return models.ErrKioskOrderNotCancellable
	}

	return s.ordersLifeCycleSvc.DeleteOrder(ctx, models.DenyOrderInput{
		OrderID:            orderID,
		MerchantID:         kiosk.MerchantID,
		UserID:             kioskCreatedBy,
		DeletionReasonID:   "KIOSK_CUSTOMER_CANCELLED",
		DeletionReasonType: "kiosk",
		DeletionComment:    "Annulée depuis la borne",
	})
}

// GetKioskOrder retourne le statut de suivi d'une commande pour la borne qui
// l'a créée (le scoping marchand vient toujours de kiosk.MerchantID, jamais
// d'un paramètre client).
func (s *Service) GetKioskOrder(ctx context.Context, orderID string, kiosk AuthenticatedKiosk) (*KioskOrderResponse, error) {
	orderResp, err := s.ordersService.ComputeGetOrder(ctx, kiosk.MerchantID, orderID)
	if err != nil {
		return nil, err
	}
	if orderResp == nil || len(orderResp.Orders) == 0 {
		return nil, models.ErrKioskOrderNotFound
	}
	order := orderResp.Orders[0]

	displayNumber := ""
	if order.OrderNum != nil {
		displayNumber = *order.OrderNum
	}
	fulfillmentType := ""
	if order.OrderType != nil {
		fulfillmentType = *order.OrderType
	}

	return &KioskOrderResponse{
		OrderID:         order.OrderID,
		DisplayNumber:   displayNumber,
		Status:          mapMerchantApprovalToKioskStatus(&order),
		FulfillmentType: fulfillmentType,
		TotalCents:      order.TTC,
		CreatedAt:       time.Unix(order.CreationDate, 0).UTC().Format(time.RFC3339),
	}, nil
}

// ---- Access token : signé HMAC-SHA256, non persisté ----

func (s *Service) generateAccessToken(kioskID, merchantID string) (string, time.Time, error) {
	expiresAt := time.Now().UTC().Add(time.Duration(s.cfg.AccessTokenTTLMinutes) * time.Minute)
	payload := fmt.Sprintf("%s|%s|%d", kioskID, merchantID, expiresAt.Unix())
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	signature := s.signPayload(encodedPayload)
	return encodedPayload + "." + signature, expiresAt, nil
}

func (s *Service) parseAccessToken(token string) (kioskID, merchantID string, expiresAt time.Time, err error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", "", time.Time{}, errors.New("malformed access token")
	}
	encodedPayload, signature := parts[0], parts[1]

	expectedSignature := s.signPayload(encodedPayload)
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return "", "", time.Time{}, errors.New("invalid access token signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return "", "", time.Time{}, err
	}

	fields := strings.SplitN(string(payloadBytes), "|", 3)
	if len(fields) != 3 {
		return "", "", time.Time{}, errors.New("malformed access token payload")
	}

	expiresUnix, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return "", "", time.Time{}, err
	}

	return fields[0], fields[1], time.Unix(expiresUnix, 0).UTC(), nil
}

func (s *Service) signPayload(encodedPayload string) string {
	h := hmac.New(sha256.New, []byte(s.cfg.Pepper))
	h.Write([]byte(encodedPayload))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// generateEnrollmentCode produit un code d'enrôlement à afficher en
// back-office : 8 caractères alphanumériques majuscules, sans 0/O/1/I pour
// éviter les confusions de lecture sur l'écran de la borne.
func generateEnrollmentCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := make([]byte, 8)
	for i, b := range raw {
		code[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(code), nil
}
