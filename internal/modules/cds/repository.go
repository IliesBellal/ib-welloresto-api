package cds

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// ---- Codes d'enrôlement ----

// GetEnrollmentCodeByHash récupère un code par son hash.
// Retourne (nil, nil) si aucun code ne correspond.
func (r *Repository) GetEnrollmentCodeByHash(ctx context.Context, codeHash string) (*EnrollmentCodeRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
	SELECT id, merchant_id, code_hash, display_name, display_id, expires_at, used_at, created_by_user_id, created_at
	FROM cds_enrollment_codes
	WHERE code_hash = ?`

	row := EnrollmentCodeRow{}
	err := db.QueryRowContext(ctx, query, codeHash).Scan(
		&row.ID, &row.MerchantID, &row.CodeHash, &row.DisplayName, &row.DisplayID,
		&row.ExpiresAt, &row.UsedAt, &row.CreatedByUserID, &row.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// CreateEnrollmentCode insère un code. displayName est nil quand le
// restaurateur n'a pas nommé l'écran : la colonne reste alors NULL (et non une
// chaîne vide), ce qui est ce que EnrollDevice teste pour retomber sur le nom
// envoyé par l'appareil.
func (r *Repository) CreateEnrollmentCode(ctx context.Context, codeID, merchantID, codeHash string, displayName *string, expiresAt time.Time, createdByUserID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := `
	INSERT INTO cds_enrollment_codes (id, merchant_id, code_hash, display_name, expires_at, created_by_user_id)
	VALUES (?, ?, ?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query, codeID, merchantID, codeHash, displayName, expiresAt, createdByUserID)
	return err
}

func (r *Repository) MarkEnrollmentCodeUsed(ctx context.Context, codeID, displayID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE cds_enrollment_codes SET used_at = %s, display_id = ? WHERE id = ?`, dbx.UTCNow())
	_, err := db.ExecContext(ctx, query, displayID, codeID)
	return err
}

// ListPendingEnrollmentCodes liste les codes encore utilisables — jamais le
// code en clair ni son hash.
func (r *Repository) ListPendingEnrollmentCodes(ctx context.Context, merchantID string) ([]EnrollmentCodeRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
	SELECT id, merchant_id, code_hash, display_name, display_id, expires_at, used_at, created_by_user_id, created_at
	FROM cds_enrollment_codes
	WHERE merchant_id = ? AND used_at IS NULL AND expires_at > %s
	ORDER BY created_at DESC`, dbx.UTCNow())

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	codes := []EnrollmentCodeRow{}
	for rows.Next() {
		row := EnrollmentCodeRow{}
		if err := rows.Scan(
			&row.ID, &row.MerchantID, &row.CodeHash, &row.DisplayName, &row.DisplayID,
			&row.ExpiresAt, &row.UsedAt, &row.CreatedByUserID, &row.CreatedAt,
		); err != nil {
			return nil, err
		}
		codes = append(codes, row)
	}
	return codes, rows.Err()
}

func (r *Repository) DeleteEnrollmentCode(ctx context.Context, merchantID, codeID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`DELETE FROM cds_enrollment_codes WHERE id = ? AND merchant_id = ? AND used_at IS NULL`,
		codeID, merchantID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ---- Écrans ----

// GetActiveDisplayCount compte les écrans qui occupent une place au regard du
// plafond de CDS_DECISIONS.md D4.
//
// Critère `status <> 'revoked'`, et non celui du module kiosk
// (`status IN ('pending','active') AND enabled = TRUE`) : il n'existe pas
// d'état désactivé côté CDS (D15), donc seule la révocation libère une place.
// Sans cela, désactiver quatre écrans pour en enrôler quatre autres viderait
// le plafond de son sens.
func (r *Repository) GetActiveDisplayCount(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)

	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cds_displays WHERE merchant_id = ? AND status <> 'revoked'`,
		merchantID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// CreateDisplay insère un écran en statut 'active'. displayID est produit par
// l'appelant.
func (r *Repository) CreateDisplay(ctx context.Context, displayID, merchantID, name, hardwareModel, osVersion string, deviceID *string) (*DisplayRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
	INSERT INTO cds_displays (id, merchant_id, name, hardware_model, os_version, device_id, status)
	VALUES (?, ?, ?, ?, ?, ?, 'active')`

	if _, err := db.ExecContext(ctx, query, displayID, merchantID, name, hardwareModel, osVersion, deviceID); err != nil {
		return nil, err
	}

	return &DisplayRow{
		ID:            displayID,
		MerchantID:    merchantID,
		Name:          name,
		Status:        "active",
		HardwareModel: &hardwareModel,
		OSVersion:     &osVersion,
		DeviceID:      deviceID,
	}, nil
}

const displaySelectColumns = `id, merchant_id, name, location_id, status, app_version, hardware_model,
	       os_version, device_id, last_heartbeat_at, last_ip,
	       last_error, last_error_at, created_at, updated_at`

func scanDisplay(scanner interface{ Scan(...any) error }) (*DisplayRow, error) {
	row := DisplayRow{}
	err := scanner.Scan(
		&row.ID, &row.MerchantID, &row.Name, &row.LocationID, &row.Status,
		&row.AppVersion, &row.HardwareModel, &row.OSVersion, &row.DeviceID,
		&row.LastHeartbeatAt, &row.LastIP,
		&row.LastError, &row.LastErrorAt, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) GetDisplayByID(ctx context.Context, displayID string) (*DisplayRow, error) {
	db := dbx.GetDB(ctx, r.database)
	query := `SELECT ` + displaySelectColumns + ` FROM cds_displays WHERE id = ?`
	return scanDisplay(db.QueryRowContext(ctx, query, displayID))
}

func (r *Repository) GetDisplayForMerchant(ctx context.Context, merchantID, displayID string) (*DisplayRow, error) {
	db := dbx.GetDB(ctx, r.database)
	query := `SELECT ` + displaySelectColumns + ` FROM cds_displays WHERE id = ? AND merchant_id = ?`
	return scanDisplay(db.QueryRowContext(ctx, query, displayID, merchantID))
}

func (r *Repository) ListDisplaysByMerchant(ctx context.Context, merchantID string) ([]DisplayRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `SELECT ` + displaySelectColumns + `
	FROM cds_displays
	WHERE merchant_id = ?
	ORDER BY created_at DESC`

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	displays := []DisplayRow{}
	for rows.Next() {
		row := DisplayRow{}
		if err := rows.Scan(
			&row.ID, &row.MerchantID, &row.Name, &row.LocationID, &row.Status,
			&row.AppVersion, &row.HardwareModel, &row.OSVersion, &row.DeviceID,
			&row.LastHeartbeatAt, &row.LastIP,
			&row.LastError, &row.LastErrorAt, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		displays = append(displays, row)
	}
	return displays, rows.Err()
}

func (r *Repository) UpdateDisplayName(ctx context.Context, merchantID, displayID, name string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE cds_displays SET name = ?, updated_at = %s WHERE id = ? AND merchant_id = ?`, dbx.UTCNow())
	res, err := db.ExecContext(ctx, query, name, displayID, merchantID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// RevokeDisplay est la seule action de cycle de vie d'un écran (D15).
func (r *Repository) RevokeDisplay(ctx context.Context, merchantID, displayID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE cds_displays SET status = 'revoked', updated_at = %s WHERE id = ? AND merchant_id = ?`, dbx.UTCNow())
	res, err := db.ExecContext(ctx, query, displayID, merchantID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *Repository) UpdateDisplayHeartbeat(ctx context.Context, displayID, appVersion, ip string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
	UPDATE cds_displays
	SET last_heartbeat_at = %s,
	    app_version = COALESCE(NULLIF(?, ''), app_version),
	    last_ip = ?,
	    updated_at = %s
	WHERE id = ?`, dbx.UTCNow(), dbx.UTCNow())

	_, err := db.ExecContext(ctx, query, appVersion, ip, displayID)
	return err
}

// ---- Tokens device ----

func (r *Repository) CreateDeviceToken(ctx context.Context, tokenID, displayID, tokenHash string, expiresAt time.Time) error {
	db := dbx.GetDB(ctx, r.database)

	query := `INSERT INTO cds_device_tokens (id, display_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query, tokenID, displayID, tokenHash, expiresAt)
	return err
}

func (r *Repository) GetDeviceTokenByHash(ctx context.Context, tokenHash string) (*DeviceTokenRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
	SELECT id, display_id, token_hash, expires_at, revoked_at, last_used_at, created_at
	FROM cds_device_tokens
	WHERE token_hash = ?`

	row := DeviceTokenRow{}
	err := db.QueryRowContext(ctx, query, tokenHash).Scan(
		&row.ID, &row.DisplayID, &row.TokenHash, &row.ExpiresAt,
		&row.RevokedAt, &row.LastUsedAt, &row.CreatedAt,
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
// À appeler dans une transaction : les deux écritures doivent être atomiques,
// sinon un échec entre les deux laisserait l'écran sans token valide.
func (r *Repository) RotateDeviceToken(ctx context.Context, oldTokenID, newTokenID, displayID, newTokenHash string, newExpiresAt time.Time) error {
	db := dbx.GetDB(ctx, r.database)

	revokeQuery := fmt.Sprintf(`UPDATE cds_device_tokens SET revoked_at = %s, last_used_at = %s WHERE id = ?`, dbx.UTCNow(), dbx.UTCNow())
	if _, err := db.ExecContext(ctx, revokeQuery, oldTokenID); err != nil {
		return err
	}

	insertQuery := `INSERT INTO cds_device_tokens (id, display_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, insertQuery, newTokenID, displayID, newTokenHash, newExpiresAt)
	return err
}

// RevokeAllDeviceTokens coupe tous les refresh tokens d'un écran — appelé à
// la révocation, pour que la borne ne puisse pas se réémettre un access token
// après la fermeture de sa connexion WebSocket.
func (r *Repository) RevokeAllDeviceTokens(ctx context.Context, displayID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE cds_device_tokens SET revoked_at = %s WHERE display_id = ? AND revoked_at IS NULL`, dbx.UTCNow())
	_, err := db.ExecContext(ctx, query, displayID)
	return err
}

// ---- Paramètres ----

// CreateDefaultSettings insère la ligne de paramètres d'un nouvel écran. Tous
// les défauts sont portés par le schéma (migration 147) : ne pas les
// dupliquer ici, sinon les deux sources divergeront.
func (r *Repository) CreateDefaultSettings(ctx context.Context, displayID string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `INSERT INTO cds_settings (display_id) VALUES (?)`, displayID)
	return err
}

// GetSettings retourne les paramètres d'un écran. Retourne (nil, nil) si la
// ligne n'existe pas.
func (r *Repository) GetSettings(ctx context.Context, displayID string) (*SettingsRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
	SELECT display_id, layout_mode, preparing_zone_ratio, order_types, channels,
	       show_wait_time, default_media_duration_seconds, created_at, updated_at
	FROM cds_settings
	WHERE display_id = ?`

	row := SettingsRow{}
	var orderTypesRaw, channelsRaw []byte
	err := db.QueryRowContext(ctx, query, displayID).Scan(
		&row.DisplayID, &row.LayoutMode, &row.PreparingZoneRatio,
		&orderTypesRaw, &channelsRaw,
		&row.ShowWaitTime, &row.DefaultMediaDurationSeconds, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(orderTypesRaw, &row.OrderTypes); err != nil {
		return nil, fmt.Errorf("cds_settings %s: unmarshal order_types: %w", displayID, err)
	}
	if err := json.Unmarshal(channelsRaw, &row.Channels); err != nil {
		return nil, fmt.Errorf("cds_settings %s: unmarshal channels: %w", displayID, err)
	}
	return &row, nil
}

// UpdateSettings applique une mise à jour partielle : seuls les champs non
// nil de req sont écrits.
func (r *Repository) UpdateSettings(ctx context.Context, displayID string, req UpdateSettingsRequest) error {
	db := dbx.GetDB(ctx, r.database)

	setClauses := []string{}
	args := []interface{}{}

	if req.LayoutMode != nil {
		setClauses = append(setClauses, "layout_mode = ?")
		args = append(args, *req.LayoutMode)
	}
	if req.PreparingZoneRatio != nil {
		setClauses = append(setClauses, "preparing_zone_ratio = ?")
		args = append(args, *req.PreparingZoneRatio)
	}
	if req.OrderTypes != nil {
		raw, err := json.Marshal(*req.OrderTypes)
		if err != nil {
			return err
		}
		setClauses = append(setClauses, "order_types = ?")
		args = append(args, string(raw))
	}
	if req.Channels != nil {
		raw, err := json.Marshal(*req.Channels)
		if err != nil {
			return err
		}
		setClauses = append(setClauses, "channels = ?")
		args = append(args, string(raw))
	}
	if req.ShowWaitTime != nil {
		setClauses = append(setClauses, "show_wait_time = ?")
		args = append(args, *req.ShowWaitTime)
	}
	if req.DefaultMediaDurationSeconds != nil {
		setClauses = append(setClauses, "default_media_duration_seconds = ?")
		args = append(args, *req.DefaultMediaDurationSeconds)
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = "+dbx.UTCNow())
	args = append(args, displayID)

	query := `UPDATE cds_settings SET ` + strings.Join(setClauses, ", ") + ` WHERE display_id = ?`
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

// ---- Médias marketing ----

func (r *Repository) ListMediaItems(ctx context.Context, displayID string, enabledOnly bool) ([]MediaItemRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
	SELECT id, display_id, kind, url, qr_payload, duration_seconds, sort_order, enabled, created_at, updated_at
	FROM cds_media_items
	WHERE display_id = ?`
	if enabledOnly {
		query += ` AND enabled = TRUE`
	}
	query += ` ORDER BY sort_order ASC, created_at ASC`

	rows, err := db.QueryContext(ctx, query, displayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []MediaItemRow{}
	for rows.Next() {
		row := MediaItemRow{}
		if err := rows.Scan(
			&row.ID, &row.DisplayID, &row.Kind, &row.URL, &row.QRPayload,
			&row.DurationSeconds, &row.SortOrder, &row.Enabled, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

// GetMediaItem retourne un média précis. Retourne (nil, nil) s'il n'existe
// pas ou n'appartient pas à cet écran.
func (r *Repository) GetMediaItem(ctx context.Context, displayID, mediaID string) (*MediaItemRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
	SELECT id, display_id, kind, url, qr_payload, duration_seconds, sort_order, enabled, created_at, updated_at
	FROM cds_media_items
	WHERE id = ? AND display_id = ?`

	row := MediaItemRow{}
	err := db.QueryRowContext(ctx, query, mediaID, displayID).Scan(
		&row.ID, &row.DisplayID, &row.Kind, &row.URL, &row.QRPayload,
		&row.DurationSeconds, &row.SortOrder, &row.Enabled, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) CreateMediaItem(ctx context.Context, item MediaItemRow) error {
	db := dbx.GetDB(ctx, r.database)

	query := `
	INSERT INTO cds_media_items (id, display_id, kind, url, qr_payload, duration_seconds, sort_order, enabled)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query,
		item.ID, item.DisplayID, item.Kind, item.URL, item.QRPayload,
		item.DurationSeconds, item.SortOrder, item.Enabled)
	return err
}

// ReorderMediaItems réécrit le sort_order de la liste. orderedIDs porte
// l'ordre voulu ; l'index dans la slice devient le sort_order.
// À appeler dans une transaction : un réordonnancement partiel laisserait
// deux médias sur le même rang.
func (r *Repository) ReorderMediaItems(ctx context.Context, displayID string, orderedIDs []string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE cds_media_items SET sort_order = ?, updated_at = %s WHERE id = ? AND display_id = ?`, dbx.UTCNow())
	for i, id := range orderedIDs {
		if _, err := db.ExecContext(ctx, query, i, id, displayID); err != nil {
			return err
		}
	}
	return nil
}

// UpdateMediaDuration fixe la durée PROPRE d'un média, ou la retire (nil) pour
// qu'il suive à nouveau la durée par défaut de l'écran.
func (r *Repository) UpdateMediaDuration(ctx context.Context, displayID, mediaID string, seconds *int) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE cds_media_items SET duration_seconds = ?, updated_at = %s WHERE id = ? AND display_id = ?`, dbx.UTCNow())
	res, err := db.ExecContext(ctx, query, seconds, mediaID, displayID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ClearMediaDurations retire toutes les durées propres d'un écran : chaque
// média suit alors la durée par défaut. Retourne le nombre de médias touchés,
// pour que le back-office puisse dire « 3 médias réinitialisés ».
func (r *Repository) ClearMediaDurations(ctx context.Context, displayID string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE cds_media_items SET duration_seconds = NULL, updated_at = %s WHERE display_id = ? AND duration_seconds IS NOT NULL`, dbx.UTCNow())
	res, err := db.ExecContext(ctx, query, displayID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *Repository) DeleteMediaItem(ctx context.Context, displayID, mediaID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`DELETE FROM cds_media_items WHERE id = ? AND display_id = ?`, mediaID, displayID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// GetNextMediaSortOrder retourne le rang à attribuer à un nouveau média.
func (r *Repository) GetNextMediaSortOrder(ctx context.Context, displayID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)

	var next sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT MAX(sort_order) + 1 FROM cds_media_items WHERE display_id = ?`, displayID).Scan(&next)
	if err != nil {
		return 0, err
	}
	if !next.Valid {
		return 0, nil
	}
	return int(next.Int64), nil
}

// ---- Projection du plateau de commandes ----

// boardOrderRow porte le résultat brut de GetBoardOrders, avant résolution du
// libellé et du statut (faite dans le service).
type boardOrderRow struct {
	OrderID           string
	OrderNum          sql.NullString
	PagerNumber       sql.NullString
	Brand             sql.NullString
	BrandOrderNum     sql.NullString
	BrandStatus       sql.NullString
	OrderType         sql.NullString
	IsDistributed     bool
	Scheduled         bool
	EstimatedReady    sql.NullTime
	LastUpdate        sql.NullTime
	CustomerFirstName sql.NullString
}

// GetBoardOrders lit les commandes à afficher sur un écran donné.
//
// Requête directe sur `orders`, volontairement SANS passer par
// OrdersRepository.GetPendingOrders / FetchAndBuildOrders : ce dernier
// reconstruit chaque commande complète (produits, extras, options, paiements,
// client, table, session de livraison) alors que l'écran n'a besoin que d'un
// libellé, d'un statut et d'une source. Sur un affichage qui se resynchronise
// à chaque reconnexion réseau, la différence n'est pas cosmétique.
//
// orderTypes et channels viennent de cds_settings : le filtre est appliqué
// ici, en base, et non côté client — un écran ne reçoit jamais les commandes
// qu'il n'a pas à montrer (CDS_DECISIONS.md D2).
func (r *Repository) GetBoardOrders(ctx context.Context, merchantID string, orderTypes, channels []string) ([]boardOrderRow, error) {
	db := dbx.GetDB(ctx, r.database)

	// Un filtre vide n'affiche rien, et c'est voulu : le restaurateur a
	// décoché toutes les cases. Court-circuiter ici évite un `IN ()`
	// syntaxiquement invalide.
	if len(orderTypes) == 0 || len(channels) == 0 {
		return []boardOrderRow{}, nil
	}

	args := []interface{}{merchantID}

	orderTypePlaceholders := make([]string, len(orderTypes))
	for i, t := range orderTypes {
		orderTypePlaceholders[i] = "?"
		args = append(args, t)
	}
	channelPlaceholders := make([]string, len(channels))
	for i, c := range channels {
		channelPlaceholders[i] = "?"
		args = append(args, c)
	}

	// state = 'OPEN' porte à lui seul la durée de vie « jusqu'à la clôture
	// manuelle » (D5) : une commande disparaît de l'écran quand le staff la
	// clôture, l'annule ou la refuse, jamais par ancienneté.
	//
	// Les statuts de paiement en attente sont exclus pour la même raison que
	// dans OrdersRepository.GetPendingOrderIDs : une commande borne dont la
	// carte n'a pas encore été présentée, ou une commande ScanNOrder dont le
	// Checkout Stripe n'est pas terminé, ne doit pas apparaître en cuisine —
	// ni sur un écran client.
	query := fmt.Sprintf(`
	SELECT o.order_id, o.order_num, o.pager_number, o.brand, o.brand_order_num,
	       o.brand_status, o.order_type, o.isDistributed, o.scheduled,
	       o.estimated_ready, o.last_update, c.customer_first_name
	FROM orders o
	LEFT JOIN customer c ON o.customer_id = c.customer_id
	WHERE o.merchant_id = ?
	  AND o.state = 'OPEN'
	  AND o.merchant_approval = 'ACCEPTED'
	  AND o.brand_status NOT IN ('ONLINE_PAYMENT_PENDING', 'PENDING_CARD_PAYMENT', 'CANCELED', 'DENIED')
	  AND o.order_type IN (%s)
	  AND o.brand IN (%s)
	  -- D12 : une commande planifiée n'apparaît qu'à H-1. Le seuil de 60 min
	  -- diverge volontairement des 90 min de distributiontime/estimate.go
	  -- (charge cuisine) et du 0 min de cash_registers/repository.go (clôture
	  -- de caisse) : trois usages distincts, pas une coquille.
	  --
	  -- Noter le double rôle d'estimated_ready : estimation calculée pour une
	  -- commande immédiate, heure demandée par le client pour une commande
	  -- planifiée. resolveIsScheduled (order_life_cycle) garantit que
	  -- scheduled = TRUE implique estimated_ready non NULL, donc ce filtre ne
	  -- peut pas tomber sur un NULL.
	  AND (o.scheduled = FALSE OR o.estimated_ready <= %s + INTERVAL '%d' MINUTE)
	ORDER BY o.last_update DESC`,
		strings.Join(orderTypePlaceholders, ","),
		strings.Join(channelPlaceholders, ","),
		dbx.UTCNow(),
		scheduledLookaheadMinutes,
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("cds board: query orders: %w", err)
	}
	defer rows.Close()

	result := []boardOrderRow{}
	for rows.Next() {
		row := boardOrderRow{}
		if err := rows.Scan(
			&row.OrderID, &row.OrderNum, &row.PagerNumber, &row.Brand, &row.BrandOrderNum,
			&row.BrandStatus, &row.OrderType, &row.IsDistributed, &row.Scheduled,
			&row.EstimatedReady, &row.LastUpdate, &row.CustomerFirstName,
		); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
