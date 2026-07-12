package kiosk

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"welloresto-api/internal/helpers"
	redisclient "welloresto-api/internal/infrastructure/redis"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/logger"
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
	terminal           TerminalGateway
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
	terminal TerminalGateway,
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
		terminal:           terminal,
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
	if err := validateKioskName(req.Name); err != nil {
		return nil, err
	}
	req.Name = strings.TrimSpace(req.Name)

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

	adminPin, err := generateAdminPin()
	if err != nil {
		return nil, fmt.Errorf("kiosk enroll: generate admin pin: %w", err)
	}
	adminPinEncrypted, err := helpers.Encrypt(adminPin)
	if err != nil {
		return nil, fmt.Errorf("kiosk enroll: encrypt admin pin: %w", err)
	}

	var kiosk *KioskRow
	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		kiosk, err = s.repo.CreateKiosk(txCtx, kioskID, code.MerchantID, req.Name, req.HardwareModel, req.OSVersion, adminPinEncrypted)
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
		AdminPin:     adminPin,
	}, nil
}

// validateKioskName impose un nom non vide et <= 100 caractères (colonne
// kiosks.name VARCHAR(100)) — comptage en runes, pas en octets, pour rester
// correct avec des noms accentués (utf8mb4).
func validateKioskName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > 100 {
		return models.ErrKioskNameInvalid
	}
	return nil
}

// generateAdminPin produit un PIN numérique à 4 chiffres — même esprit que
// generateEnrollmentCode (crypto/rand, pas math/rand), mais alphabet limité
// à 0-9 puisque le PIN est saisi sur le pavé numérique de l'écran admin de la
// borne.
func generateAdminPin() (string, error) {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	digits := make([]byte, 4)
	for i, b := range raw {
		digits[i] = '0' + b%10
	}
	return string(digits), nil
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

	return &HeartbeatResponse{Status: "ok", KioskStatus: row.Status, Enabled: row.Enabled}, nil
}

// ---- PIN admin (déverrouillage de l'écran admin local de la borne) ----

const (
	adminPinMaxAttempts     = 5
	adminPinLockoutSeconds  = 30
	adminPinLockoutStateTTL = 24 * time.Hour
)

const adminPinLockoutKeyPrefix = "kiosk:admin_pin:lockout:"

// AdminPinLockoutError carries the remaining lockout delay for the caller —
// même pattern que auth.PINLockoutError.
type AdminPinLockoutError struct {
	DelaySeconds int
}

func (e *AdminPinLockoutError) Error() string {
	return fmt.Sprintf("kiosk_admin_pin_locked: retry in %ds", e.DelaySeconds)
}

// adminPinLockoutState est sérialisé en JSON dans Redis sous
// adminPinLockoutKeyPrefix+kioskID.
type adminPinLockoutState struct {
	Count       int   `json:"count"`
	LockedUntil int64 `json:"locked_until"`
}

func (s *Service) checkAdminPinLockout(ctx context.Context, kioskID string) time.Duration {
	if s.redis == nil {
		return 0
	}
	val, found := s.redis.Get(ctx, adminPinLockoutKeyPrefix+kioskID)
	if !found {
		return 0
	}
	var state adminPinLockoutState
	if err := json.Unmarshal([]byte(val), &state); err != nil || state.LockedUntil == 0 {
		return 0
	}
	remaining := time.Until(time.Unix(state.LockedUntil, 0))
	if remaining <= 0 {
		return 0
	}
	return remaining
}

func (s *Service) incrementAdminPinLockout(ctx context.Context, kioskID string) {
	if s.redis == nil {
		return
	}
	key := adminPinLockoutKeyPrefix + kioskID
	var state adminPinLockoutState
	if val, found := s.redis.Get(ctx, key); found {
		json.Unmarshal([]byte(val), &state) //nolint:errcheck
	}
	state.Count++
	if state.Count >= adminPinMaxAttempts {
		state.LockedUntil = time.Now().Add(adminPinLockoutSeconds * time.Second).Unix()
	}
	data, _ := json.Marshal(state)
	s.redis.Set(ctx, key, string(data), adminPinLockoutStateTTL)
}

func (s *Service) resetAdminPinLockout(ctx context.Context, kioskID string) {
	if s.redis == nil {
		return
	}
	s.redis.Delete(ctx, adminPinLockoutKeyPrefix+kioskID)
}

// VerifyAdminPin déchiffre admin_pin_encrypted et compare en temps constant
// au PIN fourni — la borne est déjà authentifiée (KioskAuth), ce PIN ne fait
// que déverrouiller l'écran admin local. Rate-limité par borne (pas par
// merchant) : 5 tentatives puis 30s de lockout, voir docs/KIOSK_DECISIONS.md.
// Une erreur de déchiffrement (clé KIOSK_PIN_ENCRYPTION_KEY absente/invalide,
// ciphertext corrompu) est propagée telle quelle — ce n'est pas un PIN
// invalide, c'est une erreur de configuration serveur, jamais comptée dans
// le lockout.
func (s *Service) VerifyAdminPin(ctx context.Context, kiosk *AuthenticatedKiosk, pin string) (*VerifyAdminPinResponse, error) {
	if delay := s.checkAdminPinLockout(ctx, kiosk.KioskID); delay > 0 {
		return nil, &AdminPinLockoutError{DelaySeconds: int(delay.Seconds())}
	}

	row, err := s.repo.GetKioskByIDForMerchant(ctx, kiosk.MerchantID, kiosk.KioskID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, models.ErrKioskNotFound
	}
	if len(row.AdminPinEncrypted) == 0 {
		s.incrementAdminPinLockout(ctx, kiosk.KioskID)
		return nil, models.ErrKioskAdminPinInvalid
	}

	decrypted, err := helpers.Decrypt(row.AdminPinEncrypted)
	if err != nil {
		return nil, fmt.Errorf("kiosk verify admin pin: decrypt: %w", err)
	}

	if subtle.ConstantTimeCompare([]byte(decrypted), []byte(pin)) != 1 {
		s.incrementAdminPinLockout(ctx, kiosk.KioskID)
		return nil, models.ErrKioskAdminPinInvalid
	}

	s.resetAdminPinLockout(ctx, kiosk.KioskID)
	return &VerifyAdminPinResponse{Valid: true}, nil
}

// GetAdminPin (back-office) déchiffre et retourne le PIN admin courant d'une
// borne — consultation depuis le POS, sans le régénérer. 404 dédié si la
// borne n'a jamais eu de PIN chiffré en base (créée avant cette
// fonctionnalité, ou régénération jamais effectuée).
func (s *Service) GetAdminPin(ctx context.Context, merchantID, kioskID string) (*AdminPinResponse, error) {
	kiosk, err := s.repo.GetKioskByIDForMerchant(ctx, merchantID, kioskID)
	if err != nil {
		return nil, err
	}
	if kiosk == nil {
		return nil, models.ErrKioskNotFound
	}
	if len(kiosk.AdminPinEncrypted) == 0 {
		return nil, models.ErrKioskAdminPinNotConfigured
	}

	adminPin, err := helpers.Decrypt(kiosk.AdminPinEncrypted)
	if err != nil {
		return nil, fmt.Errorf("kiosk get admin pin: decrypt: %w", err)
	}

	return &AdminPinResponse{AdminPin: adminPin}, nil
}

// RegenerateAdminPin (back-office) génère un nouveau PIN admin pour une
// borne — utile si le technicien a perdu le PIN initial reçu une seule fois
// à l'enrôlement, ou si la borne est suspectée compromise. Retourné en clair
// une seule fois, même pattern que EnrollResponse.AdminPin.
func (s *Service) RegenerateAdminPin(ctx context.Context, merchantID, kioskID string) (*AdminPinResponse, error) {
	kiosk, err := s.repo.GetKioskByIDForMerchant(ctx, merchantID, kioskID)
	if err != nil {
		return nil, err
	}
	if kiosk == nil {
		return nil, models.ErrKioskNotFound
	}

	adminPin, err := generateAdminPin()
	if err != nil {
		return nil, fmt.Errorf("kiosk admin: generate admin pin: %w", err)
	}
	adminPinEncrypted, err := helpers.Encrypt(adminPin)
	if err != nil {
		return nil, fmt.Errorf("kiosk admin: encrypt admin pin: %w", err)
	}

	if err := s.repo.UpdateKioskAdminPinEncrypted(ctx, kiosk.ID, adminPinEncrypted); err != nil {
		return nil, err
	}

	return &AdminPinResponse{AdminPin: adminPin}, nil
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

	if err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.RevokeAllDeviceTokens(txCtx, kiosk.ID); err != nil {
			return err
		}
		return s.repo.UpdateKioskStatus(txCtx, kiosk.ID, "revoked")
	}); err != nil {
		return err
	}

	// Best-effort : ferme la connexion /ws-kiosk active de la borne sans
	// attendre l'expiration naturelle de son access token déjà émis.
	if s.notificationSvc != nil {
		s.notificationSvc.CloseKioskConnection(merchantID, kiosk.ID)
	}

	return nil
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
	if err := validateKioskName(name); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)

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
	s.broadcastKioskStatus(merchantID, kiosk.ID, true, "backoffice")
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
	s.broadcastKioskStatus(merchantID, kiosk.ID, false, "backoffice")
	resp := toKioskDeviceResponse(kiosk)
	return &resp, nil
}

// SetKioskStatusFromPOS active/désactive une borne depuis l'app POS Flutter
// (staff en salle, pas le back-office web) — même mécanique que
// Enable/DisableKioskDevice (quota vérifié uniquement à l'activation), avec
// triggered_by = "pos" dans l'event diffusé.
func (s *Service) SetKioskStatusFromPOS(ctx context.Context, merchantID, kioskID string, enabled bool) (*KioskDeviceResponse, error) {
	kiosk, err := s.repo.GetKioskByIDForMerchant(ctx, merchantID, kioskID)
	if err != nil {
		return nil, err
	}
	if kiosk == nil {
		return nil, models.ErrKioskNotFound
	}

	status := "inactive"
	if enabled {
		status = "active"
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
	}

	if err := s.repo.SetKioskStatusEnabled(ctx, kiosk.ID, status, enabled); err != nil {
		return nil, err
	}
	kiosk.Status, kiosk.Enabled = status, enabled
	s.broadcastKioskStatus(merchantID, kiosk.ID, enabled, "pos")
	resp := toKioskDeviceResponse(kiosk)
	return &resp, nil
}

// validKioskUnavailableReasons — voir docs/KIOSK_DECISIONS.md, payload
// kiosk_unavailable.
var validKioskUnavailableReasons = map[string]bool{
	"connection_lost": true,
	"app_error":       true,
	"manual":          true,
}

// ReportUnavailable est appelé par la borne elle-même (pas le POS/back-office)
// quand elle détecte un problème (ex. perte réseau récupérée, erreur
// critique côté app). Persiste last_error/last_error_at pour le support
// distant (table kiosks) et diffuse kiosk_unavailable sur le hub WebSocket du
// merchant — best-effort, ça n'altère jamais kiosks.status/enabled (la borne
// reste "active" : elle signale un souci, elle ne se désactive pas elle-même).
func (s *Service) ReportUnavailable(ctx context.Context, kiosk *AuthenticatedKiosk, reason string) error {
	if !validKioskUnavailableReasons[reason] {
		return models.ErrInvalidInput
	}

	row, err := s.repo.GetKioskByIDForMerchant(ctx, kiosk.MerchantID, kiosk.KioskID)
	if err != nil {
		return err
	}
	if row == nil {
		return models.ErrKioskNotFound
	}

	if err := s.repo.UpdateKioskLastError(ctx, row.ID, reason); err != nil {
		return err
	}

	if s.notificationSvc != nil {
		s.notificationSvc.BroadcastToMerchant(kiosk.MerchantID, map[string]interface{}{
			"type":     notification.WSEventKioskUnavailable,
			"kiosk_id": kiosk.KioskID,
			"reason":   reason,
		})
	}

	return nil
}

// broadcastKioskStatus diffuse kiosk_status_changed sur le hub WebSocket du
// merchant (voir docs/KIOSK_DECISIONS.md) — extrait pour être appelé aussi
// bien par le flux POS que par le flux back-office, sans dupliquer le
// payload. Best-effort : le hub WebSocket n'est qu'un canal temps réel, le
// heartbeat (RecordHeartbeat) reste le mécanisme de fallback fiable.
func (s *Service) broadcastKioskStatus(merchantID, kioskID string, enabled bool, triggeredBy string) {
	if s.notificationSvc == nil {
		return
	}
	status := "inactive"
	if enabled {
		status = "active"
	}
	s.notificationSvc.BroadcastToMerchant(merchantID, map[string]interface{}{
		"type":         notification.WSEventKioskStatusChanged,
		"kiosk_id":     kioskID,
		"status":       status,
		"enabled":      enabled,
		"triggered_by": triggeredBy,
	})
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

	// terminal_location_id vit dans stripe_accounts (pas kiosk_settings) : lu à
	// part, null si Terminal non activé pour ce merchant — jamais une erreur.
	terminalLocationID, err := s.repo.GetTerminalLocationID(ctx, merchantID)
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
		BusinessName:         row.BusinessName,
		Slug:                 row.Slug,
		TerminalLocationID:   terminalLocationID,
	}, nil
}

// GetDiscounts liste les promotions actives du merchant, valides à l'instant
// présent. Contrairement à scannorder.Service.GetDiscounts (qui reçoit
// ?order_type= en query et filtre dessus), GET /kiosk/discounts n'a pas de
// fulfillment_type connu au moment de l'appel (écran d'accueil, avant tout
// choix client) — orderType est donc "%" (aucun filtre par type de commande,
// seulement validité temporelle + jour de la semaine).
func (s *Service) GetDiscounts(ctx context.Context, merchantID string) (*KioskDiscountsResponse, error) {
	tz, err := s.repo.GetMerchantTimezone(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	dow := int(now.Weekday())
	if dow == 0 {
		dow = 7
	}

	discounts, err := s.repo.GetDiscounts(ctx, merchantID, "", dow)
	if err != nil {
		return nil, err
	}
	if discounts == nil {
		discounts = []KioskDiscount{}
	}

	return &KioskDiscountsResponse{Discounts: discounts}, nil
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

// ClearIdleVideoURL retire la vidéo de veille des settings (champ mis à NULL).
func (s *Service) ClearIdleVideoURL(ctx context.Context, merchantID string) (*KioskSettingsResponse, error) {
	current, err := s.repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	current.IdleVideoURL = nil
	if err := s.repo.UpsertSettings(ctx, current); err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, merchantID)
}

// ---- Incrément 2 : menu, upsell, pricing, commandes ----

// GetMenu retourne le menu filtré sur is_available_on_kiosk, avec un ETag
// (MD5 du JSON sérialisé) permettant au handler de répondre 304 sur un
// If-None-Match identique.
//
// orderType (IN/TAKE_AWAY) adapte le prix affiché par produit — même
// vocabulaire et même principe que scannorder.ComputeGetMenu avec son
// paramètre deliveryType (internal/modules/scannorder/service.go), reçu
// directement depuis la query string sans traduction intermédiaire (à la
// différence de Pricing/CreateOrder qui restent sur le vocabulaire
// IN/TAKE_AWAY propre au Kiosk, traduit via kioskFulfillmentToOrderType).
// Comme scannorder, une valeur absente ou inconnue ne fait pas échouer la
// requête : seul "TAKE_AWAY" dévie du prix de base (voir
// cleanProductPricesForKiosk) ; le menu Kiosk n'a pas de notion de DELIVERY.
//
// La réponse est cachée dans Redis par merchantID + orderType (ETag inclus —
// le flux 304/If-None-Match du handler reste fonctionnel sur un hit), même
// pattern que scannorder.GetMenu. Invalidation active à chaque mutation du
// menu via redis.Client.InvalidateMerchantMenuCaches, TTL en filet de
// sécurité.
func (s *Service) GetMenu(ctx context.Context, merchantID, orderType string) (*KioskMenuResponse, error) {
	// Si Redis est absent, direct BDD
	if s.redis == nil {
		return s.computeGetMenu(ctx, merchantID, orderType)
	}

	log := logger.FromContext(ctx)
	cacheKey := fmt.Sprintf("%s%s:%s", models.KioskMerchantMenu, merchantID, orderType)

	// --- ÉTAPE 1 : Chercher dans Redis ---
	if cached, found := s.redis.Get(ctx, cacheKey); found {
		var menu KioskMenuResponse
		if err := json.Unmarshal([]byte(cached), &menu); err == nil {
			log.Info(fmt.Sprintf("🧠📖 Kiosk menu (%s) found in Redis cache 📖🧠", orderType))
			return &menu, nil
		}
	}

	log.Info(fmt.Sprintf("🧠🚫 Kiosk menu (%s) not found in Redis cache 🚫🧠", orderType))

	// --- ÉTAPE 2 : Appel BDD (calcul lourd) ---
	menu, err := s.computeGetMenu(ctx, merchantID, orderType)
	if err != nil {
		return nil, err
	}

	// --- ÉTAPE 3 : Stocker dans Redis ---
	if serialized, err := json.Marshal(menu); err == nil {
		if saved := s.redis.Set(ctx, cacheKey, string(serialized), models.ScannorderKioskMerchantMenuTTL); !saved {
			log.Warn("Warning Redis Set (Kiosk menu): save failed")
		} else {
			log.Info("🧠📌 Kiosk menu saved in Redis cache 📌🧠")
		}
	}

	return menu, nil
}

func (s *Service) computeGetMenu(ctx context.Context, merchantID, orderType string) (*KioskMenuResponse, error) {
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

		products := flattenKioskProducts(pt.Products, availability, orderType)
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
			Available: pt.Available,
			Products:  products,
		})
	}

	resp := &KioskMenuResponse{Categories: categories}
	// orderType préfixe le hash : deux modes dont les prix seraient identiques
	// produiraient sinon le même ETag, ce qui reste correct (contenu réellement
	// identique) mais on le rend explicite pour ne jamais dépendre de cette
	// coïncidence.
	if serialized, err := json.Marshal(categories); err == nil {
		sum := md5.Sum(append([]byte(orderType+"|"), serialized...))
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
func flattenKioskProducts(products []models.ProductEntry, availability map[string]bool, orderType string) []KioskProduct {
	out := make([]KioskProduct, 0, len(products))

	var toAdd []models.ProductEntry
	for _, p := range products {
		isGroup := p.IsProductGroup != nil && *p.IsProductGroup
		if !isGroup && availability[p.ProductID] {
			out = append(out, mapProductEntryToKioskProduct(&p, orderType))
			continue
		}
		if len(p.SubProducts) > 0 {
			toAdd = append(toAdd, p.SubProducts...)
		}
	}

	for _, sp := range toAdd {
		if availability[sp.ProductID] {
			out = append(out, mapProductEntryToKioskProduct(&sp, orderType))
		}
	}

	return out
}

// cleanProductPricesForKiosk adapte le prix affiché au mode de commande Kiosk,
// même principe que scannorder.cleanProductPricesForSNO (internal/modules/
// scannorder/service.go) mais sans DELIVERY (inexistant sur Kiosk) et sans
// déréférencer un pointeur nil : si PriceTakeAway n'est pas configuré pour ce
// produit, on garde le prix de base (DINE_IN/"IN") plutôt que de paniquer.
func cleanProductPricesForKiosk(p *models.ProductEntry, orderType string) {
	if orderType == models.OrderTypeTakeAway && p.PriceTakeAway != nil {
		p.Price = *p.PriceTakeAway
	}
}

// cleanProductForKiosk nettoie un ProductEntry brut avant de l'exposer tel
// quel au client Kiosk (utilisé par GetUpsellSuggestions pour le champ
// SuggestedItem.Product — le produit y est sérialisé directement, contrairement
// à GetProduct/GetMenu qui le convertissent en KioskProduct). Collapse le prix
// selon fulfillmentType et retire les champs internes/sensibles (coûts, marges,
// indicateurs de sync, prix des autres canaux) — mêmes principes que
// scannorder.Service.cleanProductForSNO, adaptés à la convention Kiosk
// (IN/TAKE_AWAY, pas de DELIVERY).
func cleanProductForKiosk(product *models.ProductEntry, fulfillmentType string) {
	if fulfillmentType == models.OrderTypeTakeAway && product.PriceTakeAway != nil {
		product.Price = *product.PriceTakeAway
	}
	product.PriceTakeAway = nil
	product.PriceDelivery = nil
	product.PriceUberEats = nil
	product.PriceDeliveroo = nil

	product.MerchantID = nil
	product.CostPrice = nil
	product.FoodCostPercent = nil
	product.MarginPercent = nil
	product.BgColor = nil
	product.Category = nil
	product.TVAIn = nil
	product.TVADelivery = nil
	product.TVATakeAway = nil
	product.IsAvailableOnSNO = nil
	product.IsProductGroup = nil
	product.SubProducts = nil
	product.SyncDeliveroo = nil
	product.SyncUberEats = nil
	product.Available = nil
	product.AvailableIn = nil
	product.AvailableDelivery = nil
	product.AvailableTakeAway = nil
	product.IsDistributed = nil
	product.ProductionColor = nil
}

func mapProductEntryToKioskProduct(p *models.ProductEntry, orderType string) KioskProduct {
	cleanProductPricesForKiosk(p, orderType)

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
			imageURL := ""
			if opt.ImageURL != nil {
				imageURL = *opt.ImageURL
			}
			options = append(options, KioskModifierOption{
				ID:                      opt.ID,
				Title:                   opt.Title,
				ImageURL:                imageURL,
				ExtraPrice:              opt.ExtraPrice,
				MaxQuantity:             opt.MaxQuantity,
				ConfigurableAttributeID: attr.ID,
				Selected:                opt.Selected,
			})
		}
		modifierGroups = append(modifierGroups, KioskModifierGroup{
			ID:            attr.ID,
			Title:         attr.Title,
			MinOptions:    attr.MinOptions,
			MaxOptions:    attr.MaxOptions,
			AttributeType: attr.AttributeType,
			Options:       options,
		})
	}

	return KioskProduct{
		ID:                 p.ProductID,
		Name:               p.Name,
		Description:        description,
		PriceCents:         p.Price,
		PriceTakeAwayCents: p.PriceTakeAway,
		ImageURL:           imageURL,
		Available:          available,
		AvailableOnKiosk:   true,
		Allergens:          allergens,
		Tags:               tags,
		ModifierGroups:     modifierGroups,
		IsPopular:          p.IsPopular,
		TVARate:            p.TVARate,
		// MaxQuantity : ProductEntry (internal/models/menu_models.go) n'a pas
		// de limite de quantité par produit côté commande — seul
		// ConfigurableOption.MaxQuantity existe (limite par option d'un
		// modificateur, déjà porté par KioskModifierOption.MaxQuantity).
		// Champ laissé nil tant qu'aucune colonne DB n'existe pour ça.
		MaxQuantity:  nil,
		DisplayOrder: p.DisplayOrder,
		Status:       p.Status,
	}
}

// GetProduct retourne le détail d'un produit, en rejetant explicitement les
// produits désactivés sur la borne (is_available_on_kiosk = FALSE), même
// s'ils existent et sont visibles sur d'autres canaux. orderType (IN/
// TAKE_AWAY) suit la même convention que GetMenu — voir son commentaire pour
// le détail (équivalent du paramètre order_type de scannorder.GetProduct).
func (s *Service) GetProduct(ctx context.Context, merchantID, productID, orderType string) (*KioskProduct, error) {
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

	mapped := mapProductEntryToKioskProduct(product, orderType)
	return &mapped, nil
}

// GetUpsellSuggestions délègue au service Apriori existant (upsell.Service,
// même moteur que orders.GetUpsell/scannorder.PostUpsell) puis filtre
// is_available_on_kiosk et l'appartenance au panier en cours, avant de
// plafonner à 3 suggestions. Réponse alignée sur /orders/upsell (POS) :
// *upsell.UpsellResult sérialisé directement, suggestions comprises — plus
// de DTO Kiosk dédié (voir docs/KIOSK_DECISIONS.md, homogénéisation upsell).
// fulfillmentType (IN/TAKE_AWAY) n'est pas encore transmis par
// KioskUpsellRequest côté HTTP (dette documentée) : "" tombe sur le prix de
// base (IN), sans erreur.
func (s *Service) GetUpsellSuggestions(ctx context.Context, merchantID string, cartProductIDs []string, fulfillmentType string) (*upsell.UpsellResult, error) {
	if len(cartProductIDs) == 0 {
		return &upsell.UpsellResult{Suggestions: []upsell.SuggestedItem{}, Source: upsell.SourceDisabled}, nil
	}

	cartProducts := make([]models.ProductEntry, 0, len(cartProductIDs))
	inCart := make(map[string]bool, len(cartProductIDs))
	for _, id := range cartProductIDs {
		qty := 1
		cartProducts = append(cartProducts, models.ProductEntry{ProductID: id, Quantity: &qty})
		inCart[id] = true
	}

	result, err := s.upsellService.GenerateUpsell(ctx, merchantID, cartProducts, fulfillmentType, upsell.ChannelKiosk)
	if err != nil {
		return &upsell.UpsellResult{Suggestions: []upsell.SuggestedItem{}, Source: "error_fallback"}, nil
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

	suggestions := make([]upsell.SuggestedItem, 0, 3)
	for _, sugg := range result.Suggestions {
		if len(suggestions) >= 3 {
			break
		}
		if inCart[sugg.ProductID] || !available[sugg.ProductID] || sugg.Product == nil {
			continue
		}

		product := *sugg.Product
		cleanProductForKiosk(&product, fulfillmentType)
		sugg.Product = &product
		suggestions = append(suggestions, sugg)
	}

	return &upsell.UpsellResult{SuggestionID: result.SuggestionID, Suggestions: suggestions, Source: result.Source}, nil
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
// kiosk_settings pour ce merchant. orderType est déjà la valeur traduite
// ("IN"/"TAKE_AWAY", voir kioskFulfillmentToOrderType), pas le vocabulaire
// kiosk brut — pour rester cohérent avec models.OrderRequest.OrderType, seul
// champ que le service manipule désormais.
func checkFulfillmentEnabled(settings *KioskSettingsRow, orderType string) error {
	switch orderType {
	case "IN":
		if !settings.FulfillmentDineIn {
			return models.ErrKioskFulfillmentTypeDisabled
		}
	case "TAKE_AWAY":
		if !settings.FulfillmentTakeAway {
			return models.ErrKioskFulfillmentTypeDisabled
		}
	default:
		return models.ErrKioskFulfillmentTypeInvalid
	}
	return nil
}

// validateKioskProductAvailability vérifie que chaque produit du panier a
// is_available_on_kiosk = TRUE. C'est une règle métier propre au canal Kiosk
// (un produit peut être vendable en salle/POS mais désactivé sur la borne),
// distincte du calcul de prix : on ne fait que filtrer, jamais recalculer un
// prix ou une TVA (laissé entièrement à ordersService.ComputePricing).
func (s *Service) validateKioskProductAvailability(ctx context.Context, merchantID string, products []models.OrderProductPayload) error {
	if len(products) == 0 {
		return models.ErrCartEmpty
	}

	productIDs := make([]string, 0, len(products))
	for _, p := range products {
		if p.Quantity <= 0 {
			return models.ErrInvalidInput
		}
		productIDs = append(productIDs, p.ProductID)
	}

	available, err := s.repo.GetAvailableKioskProductIDs(ctx, merchantID, productIDs)
	if err != nil {
		return err
	}
	for _, id := range productIDs {
		if !available[id] {
			return models.ErrKioskProductUnavailable
		}
	}
	return nil
}

// ComputePricing prévisualise le total d'un panier (écran borne avant
// validation), sans créer de commande. Délègue entièrement à
// ordersService.ComputePricing — même contrat (models.PricingRequest /
// models.PricingResponse) que scannorder.GetPricingSNO. Seul ajout
// kiosk-spécifique : le filtre is_available_on_kiosk.
func (s *Service) ComputePricing(ctx context.Context, req *models.PricingRequest) (*models.PricingResponse, error) {
	if req.Order == nil {
		return nil, models.ErrInvalidInput
	}
	if err := s.validateKioskProductAvailability(ctx, req.MerchantID, req.Order.Products); err != nil {
		return nil, err
	}

	return s.ordersService.ComputePricing(ctx, req)
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
	case models.MerchantApprovalPendingApproval:
		return "pending_counter_payment"
	case models.MerchantApprovalPendingCardPayment:
		return "pending_card_payment"
	case "ACCEPTED":
		return "accepted"
	default:
		return strings.ToLower(order.MerchantApproval)
	}
}

// Modes de paiement acceptés par CreateOrder (champ payment_method du body).
const (
	kioskPaymentMethodCounter = "pay_at_counter"
	kioskPaymentMethodCard    = "card"
)

// CreateOrder crée une commande borne. Le mode de paiement (paymentMethod,
// champ payment_method du body) détermine le statut initial :
//   - "pay_at_counter" (ou vide, rétrocompatible) : comportement existant
//     inchangé — commande créée directement en ACCEPTED, encaissement comptoir
//     géré ensuite par ConfirmCounterPayment.
//   - "card" : commande créée en PENDING_CARD_PAYMENT (Stripe Terminal). Elle ne
//     part pas en cuisine tant que le webhook Stripe n'a pas confirmé le paiement
//     (voir docs/KIOSK_DECISIONS.md). Le paiement carte lui-même passe par les
//     endpoints /kiosk/terminal/*, hors de ce flux de création.
//
// Aucun paiement en ligne (OnlinePayment=false, Payments=[]) dans les deux cas :
// le Terminal est encaissé hors bande, pas via un Checkout web. Idempotent via
// idempotencyKey (Redis, TTL 10 min, scope par borne) — transmis par le handler
// depuis le header HTTP "Idempotency-Key". Délègue le calcul de prix et la
// création à ordersService.ComputePricing / ordersLifeCycleSvc.CreateOrder,
// exactement comme scannorder.CreateOrderSNO.
func (s *Service) CreateOrder(ctx context.Context, req *models.RequestObject, kiosk AuthenticatedKiosk, idempotencyKey, paymentMethod string) (*models.CreateOrderResult, error) {
	idemKey := ""
	if idempotencyKey != "" {
		idemKey = fmt.Sprintf("kiosk:idempotency:%s:%s", kiosk.KioskID, idempotencyKey)
		if s.redis != nil {
			if cached, found := s.redis.Get(ctx, idemKey); found {
				var resp models.CreateOrderResult
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
	if err := checkFulfillmentEnabled(settings, req.Order.OrderType); err != nil {
		return nil, err
	}

	// Le mode de paiement détermine le statut initial et la gate à vérifier.
	var initialApproval string
	switch paymentMethod {
	case kioskPaymentMethodCard:
		if !settings.CardPaymentEnabled {
			return nil, models.ErrKioskCardPaymentDisabled
		}
		initialApproval = models.MerchantApprovalPendingCardPayment
	case "", kioskPaymentMethodCounter:
		if !settings.PayAtCounterEnabled {
			return nil, models.ErrKioskPayAtCounterDisabled
		}
		initialApproval = "ACCEPTED"
	default:
		return nil, models.ErrKioskPaymentMethodInvalid
	}

	req.MerchantID = kiosk.MerchantID
	if err := s.validateKioskProductAvailability(ctx, kiosk.MerchantID, req.Order.Products); err != nil {
		return nil, err
	}

	pricing, err := s.ordersService.ComputePricing(ctx, &models.PricingRequest{
		MerchantID: kiosk.MerchantID,
		Order:      &req.Order,
	})
	if err != nil {
		return nil, err
	}
	if pricing.Status != "success" {
		return nil, models.ErrInvalidInput
	}

	orderReq := pricing.OrderRequest.Order
	orderReq.MerchantApproval = initialApproval
	orderReq.CreatedBy = strPtr(kioskCreatedBy)
	orderReq.CashRegisterId = strPtr(kioskCashRegister)
	orderReq.OnlinePayment = false
	orderReq.IsSNO = false
	orderReq.Payments = []models.PaymentPayload{}

	newOrder, err := s.ordersLifeCycleSvc.CreateOrder(ctx, &models.RequestObject{
		MerchantID:         kiosk.MerchantID,
		Order:              *orderReq,
		UpsellSuggestionID: req.UpsellSuggestionID,
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

	if idemKey != "" && s.redis != nil {
		if serialized, err := json.Marshal(newOrder); err == nil {
			s.redis.Set(ctx, idemKey, string(serialized), 10*time.Minute)
		}
	}

	return newOrder, nil
}

func strPtr(s string) *string { return &s }

// ConfirmCounterPayment marque la commande comme encaissée au comptoir : le
// staff vient de prendre le paiement réel (espèces/CB), donc on fait
// transitionner merchant_approval de PENDING_APPROVAL vers ACCEPTED (voir
// OrdersLifeCycleService.SetOrderAccepted — même mécanisme que
// AcceptOrder/POS, appelé directement ici puisque le kiosk est authentifié
// par device, pas par middleware.UserFromContext). Génère ensuite le code de
// retrait et le QR à afficher à l'écran de la borne, et notifie le merchant
// en temps réel.
func (s *Service) ConfirmCounterPayment(ctx context.Context, orderID string, kiosk AuthenticatedKiosk) (*CounterPaymentResponse, error) {
	orders, err := s.ordersService.ComputeGetOrder(ctx, kiosk.MerchantID, orderID)
	if err != nil {
		return nil, err
	}
	if orders == nil || len(orders.Orders) == 0 {
		return nil, models.ErrKioskOrderNotFound
	}
	order := orders.Orders[0]

	if order.MerchantApproval == "PENDING_APPROVAL" {
		if _, err := s.ordersLifeCycleSvc.SetOrderAccepted(ctx, kioskCreatedBy, kiosk.MerchantID, orderID); err != nil {
			return nil, err
		}
		order.MerchantApproval = "ACCEPTED"
	}

	pickupCode := ""
	if order.OrderNum != nil {
		pickupCode = *order.OrderNum
	}

	slug, err := s.repo.getMerchantSlug(ctx, kiosk.MerchantID)
	if err != nil {
		return nil, err
	}
	qrPayload := fmt.Sprintf("KIOSK:%s:%s", order.OrderID, pickupCode)
	if slug != nil && *slug != "" {
		qrPayload = fmt.Sprintf("https://scannorder.welloresto.fr/restaurants/%s/order/%s", *slug, order.OrderID)
	}

	resp := &CounterPaymentResponse{
		OrderID:       order.OrderID,
		DisplayNumber: pickupCode,
		Status:        mapMerchantApprovalToKioskStatus(&order),
		PickupCode:    pickupCode,
		QRPayload:     qrPayload,
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

	// Deux statuts sont annulables depuis la borne : une commande en attente de
	// paiement comptoir (PENDING_APPROVAL) et une commande en attente de paiement
	// carte (PENDING_CARD_PAYMENT). Toute commande déjà encaissée/acceptée
	// (ACCEPTED, CLOSED, ...) ne l'est pas.
	if order.MerchantApproval != models.MerchantApprovalPendingApproval &&
		order.MerchantApproval != models.MerchantApprovalPendingCardPayment {
		return models.ErrKioskOrderNotCancellable
	}

	// Commande en attente de paiement carte : annuler d'abord le PaymentIntent
	// Stripe actif associé, sinon un PaymentIntent orphelin pourrait être capturé
	// plus tard par erreur (client qui présente sa carte après l'annulation).
	// Best-effort (comme SwitchToCounterPayment) : un échec Stripe ne doit pas
	// empêcher l'annulation de la commande côté borne. CancelActivePaymentIntentForOrder
	// est un no-op s'il n'existe aucun PaymentIntent actif.
	if order.MerchantApproval == models.MerchantApprovalPendingCardPayment && s.terminal != nil {
		if err := s.terminal.CancelActivePaymentIntentForOrder(ctx, kiosk.MerchantID, orderID); err != nil {
			logger.FromContext(ctx).Warn("kiosk cancel order: cancel active payment intent failed: " + err.Error())
		}
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

// ---- Paiement carte (Stripe Terminal) ----

// mapTerminalError traduit les sentinelles de l'infra Stripe Terminal vers les
// erreurs Kiosk exposées au client (mapping HTTP via SendErrorJSON).
func mapTerminalError(err error) error {
	if errors.Is(err, stripeclient.ErrNoStripeAccount) {
		return models.ErrKioskTerminalNotConfigured
	}
	return err
}

// GetTerminalConnectionToken retourne un secret de connexion Stripe Terminal
// scopé au compte connecté du merchant. Gate sur card_payment_enabled : inutile
// d'appairer un lecteur si le paiement carte est désactivé pour ce merchant.
func (s *Service) GetTerminalConnectionToken(ctx context.Context, kiosk AuthenticatedKiosk) (*TerminalConnectionTokenResponse, error) {
	if s.terminal == nil {
		return nil, models.ErrKioskTerminalNotConfigured
	}

	settings, err := s.repo.GetKioskSettings(ctx, kiosk.MerchantID)
	if err != nil {
		return nil, err
	}
	if !settings.CardPaymentEnabled {
		return nil, models.ErrKioskCardPaymentDisabled
	}

	secret, err := s.terminal.CreateConnectionToken(ctx, kiosk.MerchantID)
	if err != nil {
		return nil, mapTerminalError(err)
	}
	return &TerminalConnectionTokenResponse{Secret: secret}, nil
}

// CreateTerminalPaymentIntent crée un PaymentIntent card_present pour une
// commande en attente de paiement carte. La commande doit appartenir au merchant
// et être en PENDING_CARD_PAYMENT. Le montant est re-lu depuis orders.TTC
// (jamais depuis le client) ; amount_cents fourni par la borne n'est accepté que
// s'il correspond, sinon la requête est rejetée (défense en profondeur).
func (s *Service) CreateTerminalPaymentIntent(ctx context.Context, kiosk AuthenticatedKiosk, orderID string, amountCents int64) (*TerminalPaymentIntentResponse, error) {
	if s.terminal == nil {
		return nil, models.ErrKioskTerminalNotConfigured
	}

	order, err := s.getKioskOrder(ctx, kiosk.MerchantID, orderID)
	if err != nil {
		return nil, err
	}
	if order.MerchantApproval != models.MerchantApprovalPendingCardPayment {
		return nil, models.ErrKioskOrderNotCardPending
	}
	if amountCents != int64(order.TTC) {
		return nil, models.ErrKioskAmountMismatch
	}

	variableFees, fixedFees, err := s.repo.GetKioskFees(ctx, kiosk.MerchantID)
	if err != nil {
		return nil, err
	}

	clientSecret, piID, err := s.terminal.CreateTerminalPaymentIntent(ctx, kiosk.MerchantID, orderID, int64(order.TTC), variableFees, fixedFees)
	if err != nil {
		return nil, mapTerminalError(err)
	}
	return &TerminalPaymentIntentResponse{ClientSecret: clientSecret, PaymentIntentID: piID}, nil
}

// CancelTerminalPaymentIntent annule un PaymentIntent en cours (abandon/timeout
// côté borne). La commande reste en PENDING_CARD_PAYMENT : le client peut
// relancer un paiement carte ou basculer vers la caisse
// (SwitchToCounterPayment). Le scoping merchant est appliqué côté infra
// (le mapping Redis porte le merchant_id).
func (s *Service) CancelTerminalPaymentIntent(ctx context.Context, kiosk AuthenticatedKiosk, paymentIntentID string) error {
	if s.terminal == nil {
		return models.ErrKioskTerminalNotConfigured
	}
	if err := s.terminal.CancelTerminalPaymentIntent(ctx, kiosk.MerchantID, paymentIntentID); err != nil {
		return mapTerminalError(err)
	}
	return nil
}

// SwitchToCounterPayment bascule une commande du paiement carte vers le paiement
// caisse après échec/abandon, sans recréer de commande. Annule le PaymentIntent
// actif s'il en existe un, repasse la commande en PENDING_APPROVAL
// (= pending_counter_payment), puis réutilise le flux ConfirmCounterPayment
// existant tel quel (transition vers ACCEPTED + code de retrait + QR +
// notification).
func (s *Service) SwitchToCounterPayment(ctx context.Context, kiosk AuthenticatedKiosk, orderID string) (*CounterPaymentResponse, error) {
	order, err := s.getKioskOrder(ctx, kiosk.MerchantID, orderID)
	if err != nil {
		return nil, err
	}
	if order.MerchantApproval != models.MerchantApprovalPendingCardPayment {
		return nil, models.ErrKioskOrderNotCardPending
	}

	// Annulation best-effort du PaymentIntent actif : un échec ici (ex. PI déjà
	// annulé côté Stripe) ne doit pas empêcher le basculement vers la caisse.
	if s.terminal != nil {
		if err := s.terminal.CancelActivePaymentIntentForOrder(ctx, kiosk.MerchantID, orderID); err != nil {
			logger.FromContext(ctx).Warn("kiosk switch-to-counter: cancel active payment intent failed: " + err.Error())
		}
	}

	if err := s.repo.UpdateOrderMerchantApproval(ctx, kiosk.MerchantID, orderID, models.MerchantApprovalPendingApproval); err != nil {
		return nil, err
	}
	// La commande est cachée dans Redis par ComputeGetOrder : on invalide pour
	// que ConfirmCounterPayment relise l'état PENDING_APPROVAL fraîchement écrit.
	if s.redis != nil {
		s.redis.Delete(ctx, helpers.GetRedisOrderKey(kiosk.MerchantID, orderID))
	}

	return s.ConfirmCounterPayment(ctx, orderID, kiosk)
}

// getKioskOrder factorise la récupération d'une commande scopée au merchant
// (jamais un paramètre client), utilisée par les flux Terminal.
func (s *Service) getKioskOrder(ctx context.Context, merchantID, orderID string) (*models.Order, error) {
	orderResp, err := s.ordersService.ComputeGetOrder(ctx, merchantID, orderID)
	if err != nil {
		return nil, err
	}
	if orderResp == nil || len(orderResp.Orders) == 0 {
		return nil, models.ErrKioskOrderNotFound
	}
	order := orderResp.Orders[0]
	return &order, nil
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
