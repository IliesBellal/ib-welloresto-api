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

// CreateKiosk insère une nouvelle borne en statut "pending".
func (r *Repository) CreateKiosk(ctx context.Context, merchantID, publicID, name, hardwareModel, osVersion string) (*KioskRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	INSERT INTO kiosks (public_id, merchant_id, name, hardware_model, os_version, status)
	VALUES (?, ?, ?, ?, ?, 'active')`

	res, err := db.ExecContext(ctx, query, publicID, merchantID, name, hardwareModel, osVersion)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &KioskRow{
		ID:            fmt.Sprintf("%d", id),
		PublicID:      publicID,
		MerchantID:    merchantID,
		Name:          name,
		Status:        "active",
		HardwareModel: &hardwareModel,
		OSVersion:     &osVersion,
		Enabled:       true,
	}, nil
}

// MarkEnrollmentCodeUsed marque un code comme utilisé et le lie à la borne créée.
func (r *Repository) MarkEnrollmentCodeUsed(ctx context.Context, codeID string, kioskID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE kiosk_enrollment_codes SET used_at = UTC_TIMESTAMP(), kiosk_id = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, kioskID, codeID)
	return err
}

// CreateDeviceToken insère un nouveau refresh token pour une borne. id est
// BIGINT AUTO_INCREMENT (migration 037) — jamais de public_id généré côté Go
// pour cette table (le token opaque lui-même, déjà généré par
// helpers.GenerateToken, est la seule valeur exposée au client).
func (r *Repository) CreateDeviceToken(ctx context.Context, kioskID string, tokenHash string, expiresAt time.Time) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `INSERT INTO kiosk_device_tokens (kiosk_id, token_hash, expires_at) VALUES (?, ?, ?)`
	_, err := db.ExecContext(ctx, query, kioskID, tokenHash, expiresAt)
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
func (r *Repository) RotateDeviceToken(ctx context.Context, oldTokenID string, kioskID string, newTokenHash string, newExpiresAt time.Time) error {
	db := dbutils.GetDB(ctx, r.database)

	if _, err := db.ExecContext(ctx, `UPDATE kiosk_device_tokens SET revoked_at = UTC_TIMESTAMP() WHERE id = ?`, oldTokenID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO kiosk_device_tokens (kiosk_id, token_hash, expires_at) VALUES (?, ?, ?)`, kioskID, newTokenHash, newExpiresAt)
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

// UpdateKioskStatus met à jour le statut d'une borne (ex. "revoked").
func (r *Repository) UpdateKioskStatus(ctx context.Context, kioskID string, status string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE kiosks SET status = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, status, kioskID)
	return err
}

// GetKioskByID récupère une borne par sa clé technique.
// Retourne (nil, nil) si aucune borne ne correspond.
func (r *Repository) GetKioskByID(ctx context.Context, kioskID string) (*KioskRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, public_id, merchant_id, name, location_id, status, app_version, hardware_model, os_version,
	       last_heartbeat_at, last_ip, last_error, last_error_at, enabled, created_at, updated_at
	FROM kiosks
	WHERE id = ?`

	row := KioskRow{}
	err := db.QueryRowContext(ctx, query, kioskID).Scan(
		&row.ID, &row.PublicID, &row.MerchantID, &row.Name, &row.LocationID, &row.Status, &row.AppVersion, &row.HardwareModel, &row.OSVersion,
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

// GetKioskByPublicID récupère une borne par son identifiant public.
// Retourne (nil, nil) si aucune borne ne correspond.
func (r *Repository) GetKioskByPublicID(ctx context.Context, merchantID, publicID string) (*KioskRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, public_id, merchant_id, name, location_id, status, app_version, hardware_model, os_version,
	       last_heartbeat_at, last_ip, last_error, last_error_at, enabled, created_at, updated_at
	FROM kiosks
	WHERE merchant_id = ? AND public_id = ?`

	row := KioskRow{}
	err := db.QueryRowContext(ctx, query, merchantID, publicID).Scan(
		&row.ID, &row.PublicID, &row.MerchantID, &row.Name, &row.LocationID, &row.Status, &row.AppVersion, &row.HardwareModel, &row.OSVersion,
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
	SELECT id, public_id, merchant_id, name, location_id, status, app_version, hardware_model, os_version,
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
			&row.ID, &row.PublicID, &row.MerchantID, &row.Name, &row.LocationID, &row.Status, &row.AppVersion, &row.HardwareModel, &row.OSVersion,
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

// CreateEnrollmentCode insère un nouveau code d'enrôlement. id est BIGINT
// AUTO_INCREMENT (migration 037) — l'identifiant "public" pertinent ici est
// le code lisible humainement (généré par generateEnrollmentCode), jamais
// stocké en clair ; la ligne elle-même n'a pas besoin de public_id.
func (r *Repository) CreateEnrollmentCode(ctx context.Context, merchantID, codeHash string, expiresAt time.Time, createdByUserID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `INSERT INTO kiosk_enrollment_codes (merchant_id, code_hash, expires_at, created_by_user_id) VALUES (?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query, merchantID, codeHash, expiresAt, createdByUserID)
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
func (r *Repository) GetKioskSettings(ctx context.Context, merchantID string) (*KioskSettingsRow, error) {
	row, err := r.GetSettingsByMerchant(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return defaultKioskSettingsRow(merchantID), nil
	}
	return row, nil
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

// GetExistingConfigurationOptionIDs vérifie l'existence réelle d'options de
// configuration (rejette les IDs fabriqués côté client — même esprit que
// GetProductPricesForSNO dans scannorder).
func (r *Repository) GetExistingConfigurationOptionIDs(ctx context.Context, optionIDs []string) (map[string]bool, error) {
	result := make(map[string]bool)
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

	query := fmt.Sprintf(`SELECT id FROM configurable_attribute_options WHERE id IN (%s)`, placeholders)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query configuration option existence: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan configuration option id: %w", err)
		}
		result[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error during configuration option existence fetch: %w", err)
	}
	return result, nil
}

// SetKioskIDOnOrder renseigne orders.kiosk_id après création — la création
// elle-même passe par OrdersLifeCycleService.CreateOrder (non modifiable),
// qui ne connaît pas la notion de borne ; ce point UPDATE ciblé referme la
// boucle sans toucher à ce service.
func (r *Repository) SetKioskIDOnOrder(ctx context.Context, orderID, kioskPublicID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE orders SET kiosk_id = ? WHERE order_id = ?`
	_, err := db.ExecContext(ctx, query, kioskPublicID, orderID)
	return err
}
