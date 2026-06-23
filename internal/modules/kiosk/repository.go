package kiosk

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// GetEnrollmentCodeByHash récupère un code d'enrôlement par son hash.
// Retourne (nil, nil) si aucun code ne correspond.
func (r *Repository) GetEnrollmentCodeByHash(ctx context.Context, codeHash string) (*EnrollmentCodeRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, merchant_id, code_hash, kiosk_id, expires_at, used_at, created_by_user_id, created_at
	FROM kiosk_enrollment_codes
	WHERE code_hash = ?`

	row := EnrollmentCodeRow{}
	err := db.QueryRowContext(ctx, query, codeHash).Scan(
		&row.ID, &row.MerchantID, &row.CodeHash, &row.KioskID, &row.ExpiresAt, &row.UsedAt, &row.CreatedByUserID, &row.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// CreateKiosk insère une nouvelle borne en statut "active". kioskID est
// généré par l'appelant (Service.EnrollDevice, via
// helpers.GeneratePrefixedID(helpers.KioskIDPrefix)) — le repository ne
// génère plus d'identifiant. adminPinEncrypted est déjà chiffré par
// l'appelant (helpers.Encrypt, AES-256-GCM) — le PIN en clair n'existe jamais
// côté repository.
func (r *Repository) CreateKiosk(ctx context.Context, kioskID, merchantID, name, hardwareModel, osVersion string, adminPinEncrypted []byte) (*KioskRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	INSERT INTO kiosks (id, merchant_id, name, hardware_model, os_version, admin_pin_encrypted, status)
	VALUES (?, ?, ?, ?, ?, ?, 'active')`

	if _, err := db.ExecContext(ctx, query, kioskID, merchantID, name, hardwareModel, osVersion, adminPinEncrypted); err != nil {
		return nil, err
	}

	return &KioskRow{
		ID:                kioskID,
		MerchantID:        merchantID,
		Name:              name,
		Status:            "active",
		HardwareModel:     &hardwareModel,
		OSVersion:         &osVersion,
		AdminPinEncrypted: adminPinEncrypted,
		Enabled:           true,
	}, nil
}

// MarkEnrollmentCodeUsed marque un code comme utilisé et le lie à la borne créée.
func (r *Repository) MarkEnrollmentCodeUsed(ctx context.Context, codeID string, kioskID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE kiosk_enrollment_codes SET used_at = UTC_TIMESTAMP(), kiosk_id = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, kioskID, codeID)
	return err
}

// CreateDeviceToken insère un nouveau refresh token pour une borne. tokenID
// est généré par l'appelant (Service, via
// helpers.GeneratePrefixedID(helpers.KioskDeviceTokenIDPrefix)) — le token
// opaque lui-même (tokenHash, déjà généré par helpers.GenerateToken) reste
// la seule valeur exposée au client, tokenID n'est qu'une clé technique.
func (r *Repository) CreateDeviceToken(ctx context.Context, tokenID, kioskID, tokenHash string, expiresAt time.Time) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `INSERT INTO kiosk_device_tokens (id, kiosk_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query, tokenID, kioskID, tokenHash, expiresAt)
	return err
}

// GetDeviceTokenByHash récupère un refresh token par son hash.
// Retourne (nil, nil) si aucun token ne correspond.
func (r *Repository) GetDeviceTokenByHash(ctx context.Context, tokenHash string) (*KioskDeviceTokenRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, kiosk_id, token_hash, expires_at, revoked_at, last_used_at, created_at
	FROM kiosk_device_tokens
	WHERE token_hash = ?`

	row := KioskDeviceTokenRow{}
	err := db.QueryRowContext(ctx, query, tokenHash).Scan(
		&row.ID, &row.KioskID, &row.TokenHash, &row.ExpiresAt, &row.RevokedAt, &row.LastUsedAt, &row.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// RotateDeviceToken révoque l'ancien refresh token et insère le nouveau.
// newTokenID est généré par l'appelant, même convention que CreateDeviceToken.
func (r *Repository) RotateDeviceToken(ctx context.Context, oldTokenID, newTokenID, kioskID, newTokenHash string, newExpiresAt time.Time) error {
	db := dbutils.GetDB(ctx, r.database)

	if _, err := db.ExecContext(ctx, `UPDATE kiosk_device_tokens SET revoked_at = UTC_TIMESTAMP() WHERE id = ?`, oldTokenID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO kiosk_device_tokens (id, kiosk_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`, newTokenID, kioskID, newTokenHash, newExpiresAt)
	return err
}

// RevokeAllDeviceTokens révoque immédiatement tous les refresh tokens d'une borne.
func (r *Repository) RevokeAllDeviceTokens(ctx context.Context, kioskID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE kiosk_device_tokens SET revoked_at = UTC_TIMESTAMP() WHERE kiosk_id = ? AND revoked_at IS NULL`
	_, err := db.ExecContext(ctx, query, kioskID)
	return err
}

// UpdateDeviceTokenLastUsed marque un refresh token comme utilisé (heartbeat/refresh).
func (r *Repository) UpdateDeviceTokenLastUsed(ctx context.Context, tokenID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE kiosk_device_tokens SET last_used_at = UTC_TIMESTAMP() WHERE id = ?`
	_, err := db.ExecContext(ctx, query, tokenID)
	return err
}

// UpdateKioskHeartbeat met à jour le dernier heartbeat reçu d'une borne.
func (r *Repository) UpdateKioskHeartbeat(ctx context.Context, kioskID string, appVersion, ip string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE kiosks SET last_heartbeat_at = UTC_TIMESTAMP(), app_version = ?, last_ip = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, appVersion, ip, kioskID)
	return err
}

// UpdateKioskLastError enregistre la dernière erreur signalée par la borne
// elle-même (kiosk_unavailable) — visibilité support distant, voir
// docs/KIOSK_DECISIONS.md table kiosks.
func (r *Repository) UpdateKioskLastError(ctx context.Context, kioskID, reason string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE kiosks SET last_error = ?, last_error_at = UTC_TIMESTAMP() WHERE id = ?`
	_, err := db.ExecContext(ctx, query, reason, kioskID)
	return err
}

// UpdateKioskStatus met à jour le statut d'une borne (ex. "revoked").
func (r *Repository) UpdateKioskStatus(ctx context.Context, kioskID string, status string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE kiosks SET status = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, status, kioskID)
	return err
}

// GetKioskByID récupère une borne par sa clé technique, sans scope merchant
// — utilisé uniquement quand l'appartenance est déjà garantie autrement (ex.
// via un refresh token déjà résolu pour cette borne, RefreshDeviceToken).
// Retourne (nil, nil) si aucune borne ne correspond.
func (r *Repository) GetKioskByID(ctx context.Context, kioskID string) (*KioskRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, merchant_id, name, location_id, status, app_version, hardware_model, admin_pin_encrypted, os_version,
	       last_heartbeat_at, last_ip, last_error, last_error_at, enabled, created_at, updated_at
	FROM kiosks
	WHERE id = ?`

	row := KioskRow{}
	err := db.QueryRowContext(ctx, query, kioskID).Scan(
		&row.ID, &row.MerchantID, &row.Name, &row.LocationID, &row.Status, &row.AppVersion, &row.HardwareModel, &row.AdminPinEncrypted, &row.OSVersion,
		&row.LastHeartbeatAt, &row.LastIP, &row.LastError, &row.LastErrorAt, &row.Enabled, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetKioskByIDForMerchant récupère une borne par id, scopée au merchant —
// utilisé par toutes les routes back-office (jamais d'accès cross-merchant).
// Retourne (nil, nil) si aucune borne ne correspond ou appartient à un autre
// merchant.
func (r *Repository) GetKioskByIDForMerchant(ctx context.Context, merchantID, kioskID string) (*KioskRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, merchant_id, name, location_id, status, app_version, hardware_model, admin_pin_encrypted, os_version,
	       last_heartbeat_at, last_ip, last_error, last_error_at, enabled, created_at, updated_at
	FROM kiosks
	WHERE merchant_id = ? AND id = ?`

	row := KioskRow{}
	err := db.QueryRowContext(ctx, query, merchantID, kioskID).Scan(
		&row.ID, &row.MerchantID, &row.Name, &row.LocationID, &row.Status, &row.AppVersion, &row.HardwareModel, &row.AdminPinEncrypted, &row.OSVersion,
		&row.LastHeartbeatAt, &row.LastIP, &row.LastError, &row.LastErrorAt, &row.Enabled, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ListKiosksByMerchant liste les bornes d'un merchant.
func (r *Repository) ListKiosksByMerchant(ctx context.Context, merchantID string) ([]KioskRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, merchant_id, name, location_id, status, app_version, hardware_model, os_version,
	       last_heartbeat_at, last_ip, last_error, last_error_at, enabled, created_at, updated_at
	FROM kiosks
	WHERE merchant_id = ?
	ORDER BY created_at DESC`

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []KioskRow
	for rows.Next() {
		row := KioskRow{}
		if err := rows.Scan(
			&row.ID, &row.MerchantID, &row.Name, &row.LocationID, &row.Status, &row.AppVersion, &row.HardwareModel, &row.OSVersion,
			&row.LastHeartbeatAt, &row.LastIP, &row.LastError, &row.LastErrorAt, &row.Enabled, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// UpdateKioskName renomme une borne (seul champ éditable pour l'instant côté back-office).
func (r *Repository) UpdateKioskName(ctx context.Context, kioskID, name string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `UPDATE kiosks SET name = ? WHERE id = ?`, name, kioskID)
	return err
}

// UpdateKioskAdminPinEncrypted remplace le PIN admin chiffré d'une borne
// (régénération back-office, voir Service.RegenerateAdminPin). Le
// chiffrement est déjà fait par l'appelant (helpers.Encrypt) — le repository
// ne manipule jamais le PIN en clair.
func (r *Repository) UpdateKioskAdminPinEncrypted(ctx context.Context, kioskID string, adminPinEncrypted []byte) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `UPDATE kiosks SET admin_pin_encrypted = ? WHERE id = ?`, adminPinEncrypted, kioskID)
	return err
}

// SetKioskStatusEnabled met à jour status et enabled ensemble (enable/disable).
func (r *Repository) SetKioskStatusEnabled(ctx context.Context, kioskID, status string, enabled bool) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `UPDATE kiosks SET status = ?, enabled = ? WHERE id = ?`, status, enabled, kioskID)
	return err
}

// GetActiveKioskCount compte les bornes actives/pending d'un merchant (hors révoquées/inactives).
func (r *Repository) GetActiveKioskCount(ctx context.Context, merchantID string) (int, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `SELECT COUNT(*) FROM kiosks WHERE merchant_id = ? AND status IN ('pending', 'active') AND enabled = TRUE`
	var count int
	if err := db.QueryRowContext(ctx, query, merchantID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// GetMerchantMaxKiosks récupère le quota de bornes du merchant depuis subscriptions.
func (r *Repository) GetMerchantMaxKiosks(ctx context.Context, merchantID string) (int, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `SELECT max_kiosks FROM subscriptions WHERE merchant_id = ?`
	var maxKiosks int
	err := db.QueryRowContext(ctx, query, merchantID).Scan(&maxKiosks)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return maxKiosks, nil
}

// GetSettingsByMerchant récupère les paramètres Kiosk d'un merchant.
// Retourne (nil, nil) si aucune ligne n'existe encore.
func (r *Repository) GetSettingsByMerchant(ctx context.Context, merchantID string) (*KioskSettingsRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT merchant_id, fulfillment_dine_in, fulfillment_take_away, force_fulfillment_type, pager_number_required,
	       show_allergens, inactivity_timeout_sec, upsell_enabled, pay_at_counter_enabled, card_payment_enabled,
	       logo_url, idle_image_url, idle_video_url, primary_color, created_at, updated_at
	FROM kiosk_settings
	WHERE merchant_id = ?`

	row := KioskSettingsRow{}
	err := db.QueryRowContext(ctx, query, merchantID).Scan(
		&row.MerchantID, &row.FulfillmentDineIn, &row.FulfillmentTakeAway, &row.ForceFulfillmentType, &row.PagerNumberRequired,
		&row.ShowAllergens, &row.InactivityTimeoutSec, &row.UpsellEnabled, &row.PayAtCounterEnabled, &row.CardPaymentEnabled,
		&row.LogoURL, &row.IdleImageURL, &row.IdleVideoURL, &row.PrimaryColor, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpsertSettings crée ou met à jour les paramètres Kiosk d'un merchant.
func (r *Repository) UpsertSettings(ctx context.Context, s *KioskSettingsRow) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	INSERT INTO kiosk_settings (
		merchant_id, fulfillment_dine_in, fulfillment_take_away, force_fulfillment_type, pager_number_required,
		show_allergens, inactivity_timeout_sec, upsell_enabled, pay_at_counter_enabled, card_payment_enabled,
		logo_url, idle_image_url, idle_video_url, primary_color
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		fulfillment_dine_in = VALUES(fulfillment_dine_in),
		fulfillment_take_away = VALUES(fulfillment_take_away),
		force_fulfillment_type = VALUES(force_fulfillment_type),
		pager_number_required = VALUES(pager_number_required),
		show_allergens = VALUES(show_allergens),
		inactivity_timeout_sec = VALUES(inactivity_timeout_sec),
		upsell_enabled = VALUES(upsell_enabled),
		pay_at_counter_enabled = VALUES(pay_at_counter_enabled),
		card_payment_enabled = VALUES(card_payment_enabled),
		logo_url = VALUES(logo_url),
		idle_image_url = VALUES(idle_image_url),
		idle_video_url = VALUES(idle_video_url),
		primary_color = VALUES(primary_color)`

	_, err := db.ExecContext(ctx, query,
		s.MerchantID, s.FulfillmentDineIn, s.FulfillmentTakeAway, s.ForceFulfillmentType, s.PagerNumberRequired,
		s.ShowAllergens, s.InactivityTimeoutSec, s.UpsellEnabled, s.PayAtCounterEnabled, s.CardPaymentEnabled,
		s.LogoURL, s.IdleImageURL, s.IdleVideoURL, s.PrimaryColor,
	)
	return err
}

// CreateEnrollmentCode insère un nouveau code d'enrôlement. id est généré
// par l'appelant (Service, via
// helpers.GeneratePrefixedID(helpers.KioskEnrollmentCodeIDPrefix)) — c'est
// une clé technique interne, jamais exposée seule : l'identifiant pertinent
// pour le restaurateur reste le code lisible humainement (généré par
// generateEnrollmentCode), jamais stocké en clair.
func (r *Repository) CreateEnrollmentCode(ctx context.Context, id, merchantID, codeHash string, expiresAt time.Time, createdByUserID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `INSERT INTO kiosk_enrollment_codes (id, merchant_id, code_hash, expires_at, created_by_user_id) VALUES (?, ?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query, id, merchantID, codeHash, expiresAt, createdByUserID)
	return err
}

// ListPendingEnrollmentCodes liste les codes d'enrôlement non encore
// utilisés et non expirés d'un merchant — utile pour le back-office
// ("un code est en attente depuis N min").
func (r *Repository) ListPendingEnrollmentCodes(ctx context.Context, merchantID string) ([]EnrollmentCodeRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, merchant_id, code_hash, kiosk_id, expires_at, used_at, created_by_user_id, created_at
	FROM kiosk_enrollment_codes
	WHERE merchant_id = ? AND used_at IS NULL AND expires_at > UTC_TIMESTAMP()
	ORDER BY created_at DESC`

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []EnrollmentCodeRow
	for rows.Next() {
		row := EnrollmentCodeRow{}
		if err := rows.Scan(
			&row.ID, &row.MerchantID, &row.CodeHash, &row.KioskID, &row.ExpiresAt, &row.UsedAt, &row.CreatedByUserID, &row.CreatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetEnrollmentCodeByID récupère un code d'enrôlement par son id interne,
// scopé au merchant. Retourne (nil, nil) si non trouvé ou appartenant à un
// autre merchant.
func (r *Repository) GetEnrollmentCodeByID(ctx context.Context, merchantID, id string) (*EnrollmentCodeRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, merchant_id, code_hash, kiosk_id, expires_at, used_at, created_by_user_id, created_at
	FROM kiosk_enrollment_codes
	WHERE id = ? AND merchant_id = ?`

	row := EnrollmentCodeRow{}
	err := db.QueryRowContext(ctx, query, id, merchantID).Scan(
		&row.ID, &row.MerchantID, &row.CodeHash, &row.KioskID, &row.ExpiresAt, &row.UsedAt, &row.CreatedByUserID, &row.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteEnrollmentCode supprime définitivement un code d'enrôlement non
// utilisé (révocation avant usage, voir Service.RevokeEnrollmentCode).
func (r *Repository) DeleteEnrollmentCode(ctx context.Context, id string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `DELETE FROM kiosk_enrollment_codes WHERE id = ?`, id)
	return err
}

// ---- Incrément 2 : menu, pricing, commandes ----

// defaultKioskSettingsRow renvoie les valeurs par défaut appliquées tant
// qu'un merchant n'a pas encore sa propre ligne dans kiosk_settings — une
// borne doit fonctionner sans configuration préalable.
func defaultKioskSettingsRow(merchantID string) *KioskSettingsRow {
	return &KioskSettingsRow{
		MerchantID:           merchantID,
		FulfillmentDineIn:    true,
		FulfillmentTakeAway:  true,
		ShowAllergens:        true,
		InactivityTimeoutSec: 90,
		UpsellEnabled:        true,
		PayAtCounterEnabled:  true,
	}
}

// GetKioskSettings récupère les paramètres Kiosk d'un merchant, ou les
// valeurs par défaut si la ligne n'existe pas encore — jamais sql.ErrNoRows.
// BusinessName vient toujours de la table merchant (jamais de kiosk_settings),
// donc il est attaché que la ligne kiosk_settings existe ou non.
func (r *Repository) GetKioskSettings(ctx context.Context, merchantID string) (*KioskSettingsRow, error) {
	row, err := r.GetSettingsByMerchant(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		row = defaultKioskSettingsRow(merchantID)
	}

	businessName, err := r.getMerchantBusinessName(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	row.BusinessName = businessName

	return row, nil
}

// getMerchantBusinessName récupère merchant.fullName — utilisé pour le
// bandeau d'accueil du Menu côté borne (kiosk_settings n'a pas cette
// information, voir docs/KIOSK_DECISIONS.md).
func (r *Repository) getMerchantBusinessName(ctx context.Context, merchantID string) (*string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var fullName *string
	err := db.QueryRowContext(ctx, `SELECT fullName FROM merchant WHERE id = ?`, merchantID).Scan(&fullName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return fullName, nil
}

// GetAvailableKioskProductIDs filtre productIDs et ne retourne que ceux qui
// existent pour ce merchant ET ont is_available_on_kiosk = TRUE. Un produit
// absent du résultat doit être traité comme invalide par l'appelant (qu'il
// n'existe pas ou qu'il soit simplement masqué sur la borne ne change rien
// côté sécurité : dans les deux cas, la commande ne doit pas être acceptée).
func (r *Repository) GetAvailableKioskProductIDs(ctx context.Context, merchantID string, productIDs []string) (map[string]bool, error) {
	result := make(map[string]bool)
	if len(productIDs) == 0 {
		return result, nil
	}

	db := dbutils.GetDB(ctx, r.database)

	placeholders := ""
	args := []interface{}{merchantID}
	for i, id := range productIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
	SELECT product_id FROM products
	WHERE merchant_id = ? AND is_available_on_kiosk = TRUE AND product_id IN (%s)`, placeholders)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query kiosk product availability: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var productID string
		if err := rows.Scan(&productID); err != nil {
			return nil, fmt.Errorf("failed to scan kiosk product availability: %w", err)
		}
		result[productID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error during kiosk product availability fetch: %w", err)
	}
	return result, nil
}

// GetKioskProductAvailabilityMap retourne is_available_on_kiosk pour tous
// les produits d'un merchant — utilisé pour filtrer le menu complet renvoyé
// par menuService sans avoir à modifier ce module.
func (r *Repository) GetKioskProductAvailabilityMap(ctx context.Context, merchantID string) (map[string]bool, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `SELECT product_id, is_available_on_kiosk FROM products WHERE merchant_id = ?`
	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query kiosk product availability map: %w", err)
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var productID string
		var available bool
		if err := rows.Scan(&productID, &available); err != nil {
			return nil, fmt.Errorf("failed to scan kiosk product availability map: %w", err)
		}
		result[productID] = available
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error during kiosk product availability map fetch: %w", err)
	}
	return result, nil
}

// GetConfigurationOptionAttributeIDs vérifie l'existence réelle des options de
// configuration (rejette les IDs fabriqués côté client — même esprit que
// GetProductPricesForSNO dans scannorder) ET retourne le vrai
// configurable_attribute_id de chacune, pour regrouper correctement les
// options sélectionnées par groupe de modificateur (voir
// docs/KIOSK_VS_SCANNORDER_STRUCTS.md écart #2 — buildOrderProducts utilisait
// auparavant un id fictif "kiosk-options" pour tout regrouper).
func (r *Repository) GetConfigurationOptionAttributeIDs(ctx context.Context, optionIDs []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(optionIDs) == 0 {
		return result, nil
	}

	db := dbutils.GetDB(ctx, r.database)

	placeholders := ""
	args := []interface{}{}
	for i, id := range optionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`SELECT id, configurable_attribute_id FROM configurable_attribute_options WHERE id IN (%s)`, placeholders)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query configuration option attribute ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, attributeID string
		if err := rows.Scan(&id, &attributeID); err != nil {
			return nil, fmt.Errorf("failed to scan configuration option attribute id: %w", err)
		}
		result[id] = attributeID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error during configuration option attribute id fetch: %w", err)
	}
	return result, nil
}

// SetKioskIDOnOrder renseigne orders.kiosk_id après création — la création
// elle-même passe par OrdersLifeCycleService.CreateOrder (non modifiable),
// qui ne connaît pas la notion de borne ; ce point UPDATE ciblé referme la
// boucle sans toucher à ce service.
func (r *Repository) SetKioskIDOnOrder(ctx context.Context, orderID, kioskID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE orders SET kiosk_id = ? WHERE order_id = ?`
	_, err := db.ExecContext(ctx, query, kioskID, orderID)
	return err
}

// GetMerchantTimezone récupère le fuseau horaire du merchant, nécessaire pour
// calculer le jour de la semaine courant côté GetDiscounts (les promotions
// peuvent être limitées à certains jours via discounts_schedules). Le module
// kiosk connaît déjà le merchant_id (via KioskAuth, pas de QR code à
// résoudre comme scannorder.GetMerchantIDAndTZFromQR) — cette requête isolée
// évite d'importer le module scannorder juste pour ce champ.
func (r *Repository) GetMerchantTimezone(ctx context.Context, merchantID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var tz string
	err := db.QueryRowContext(ctx, `SELECT timezone FROM merchant WHERE id = ?`, merchantID).Scan(&tz)
	if err != nil {
		return "", err
	}
	return tz, nil
}

// GetDiscounts liste les promotions actives du merchant, valides à l'instant
// présent et pour le jour de la semaine donné (1=lundi...7=dimanche, même
// convention que scannorder.Repository.GetDiscounts). orderType filtre sur
// discounts.discount_order_type via LIKE — passer "" pour ne filtrer sur
// aucun type (voir kiosk.Service.GetDiscounts : la borne n'a pas toujours un
// fulfillment_type connu au moment de l'affichage des promotions, à la
// différence de ScanNOrder qui reçoit ?order_type= en query).
func (r *Repository) GetDiscounts(ctx context.Context, merchantID string, orderType string, dow int) ([]KioskDiscount, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT DISTINCT
		d.discount_id,
		d.discount_order_type,
		d.discount_code,
		d.discount_desc,
		d.discount_name,
		d.discount_value,
		d.discount_unit,
		d.min_order_value,
		d.min_order_unit,
		d.max_discount_value,
		d.max_discount_unit,
		d.discounted_quantity,
		d.is_cumulative,
		d.available
	FROM discounts d
	LEFT JOIN discounts_schedules ds ON ds.discount_id = d.discount_id AND ds.enabled = true
	WHERE d.merchant_id = ?
	AND d.discount_order_type LIKE ?
	AND (d.valid_from < UTC_TIMESTAMP()
		AND (d.valid_to > UTC_TIMESTAMP() OR d.valid_to IS NULL))
	AND (
		(ds.available_from < UTC_TIMESTAMP()
		 AND ds.available_to > UTC_TIMESTAMP()
		 AND ds.day_of_week = ?)
		OR NOT d.is_time_limited
	)
	AND d.available = true
	AND d.enabled = true
	`

	rows, err := db.QueryContext(ctx, query, merchantID, "%"+orderType+"%", dow)
	if err != nil {
		return nil, fmt.Errorf("failed to query kiosk discounts: %w", err)
	}
	defer rows.Close()

	discounts := []KioskDiscount{}

	for rows.Next() {
		var d KioskDiscount
		var isCumulative int
		var available int

		err := rows.Scan(
			&d.DiscountID,
			&d.DiscountOrderType,
			&d.DiscountCode,
			&d.DiscountDesc,
			&d.DiscountName,
			&d.DiscountValue,
			&d.DiscountUnit,
			&d.MinOrderValue,
			&d.MinOrderUnit,
			&d.MaxDiscountValue,
			&d.MaxDiscountUnit,
			&d.DiscountedQuantity,
			&isCumulative,
			&available,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan kiosk discount: %w", err)
		}

		d.IsCumulative = isCumulative == 1
		d.Available = available == 1

		discounts = append(discounts, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error during kiosk discounts fetch: %w", err)
	}

	return discounts, nil
}
