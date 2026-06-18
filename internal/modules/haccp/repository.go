package haccp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	db *sql.DB
}

type readingCorrectiveActionCreate struct {
	ReadingID     string
	ActionID      string
	Note          *string
	PhotoURL      *string
	FollowUpValue *float64
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListTemperatureZones(ctx context.Context, merchantID string) ([]Zone, error) {
	db := dbutils.GetDB(ctx, r.db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, name, target_temp_min, target_temp_max, created_at, updated_at, enabled, deleted_at
		FROM temperature_zones
		WHERE merchant_id = ? AND enabled = 1
		ORDER BY created_at DESC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	zones := make([]Zone, 0)
	for rows.Next() {
		var z Zone
		if err := rows.Scan(
			&z.ID,
			&z.MerchantID,
			&z.Name,
			&z.TargetTempMin,
			&z.TargetTempMax,
			&z.CreatedAt,
			&z.UpdatedAt,
			&z.Enabled,
			&z.DeletedAt,
		); err != nil {
			return nil, err
		}
		zones = append(zones, z)
	}

	return zones, rows.Err()
}

func (r *Repository) CreateTemperatureZone(ctx context.Context, merchantID string, req CreateZoneRequest) (*Zone, error) {
	db := dbutils.GetDB(ctx, r.db)

	id := helpers.GeneratePrefixedID(helpers.HACCPTemperatureZoneIDPrefix)
	now := time.Now().UTC()

	_, err := db.ExecContext(ctx, `
		INSERT INTO temperature_zones (id, merchant_id, name, target_temp_min, target_temp_max, created_at, updated_at, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1)
	`, id, merchantID, req.Name, req.TargetTempMin, req.TargetTempMax, now, now)
	if err != nil {
		return nil, err
	}

	return &Zone{
		ID:            id,
		MerchantID:    merchantID,
		Name:          req.Name,
		TargetTempMin: req.TargetTempMin,
		TargetTempMax: req.TargetTempMax,
		CreatedAt:     now,
		UpdatedAt:     now,
		Enabled:       true,
	}, nil
}

func (r *Repository) ReplaceTemperatureZone(ctx context.Context, merchantID, zoneID string, req ReplaceZoneRequest) (*Zone, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()

	res, err := db.ExecContext(ctx, `
		UPDATE temperature_zones
		SET name = ?, target_temp_min = ?, target_temp_max = ?, updated_at = ?, enabled = 1, deleted_at = NULL
		WHERE id = ? AND merchant_id = ?
	`, req.Name, req.TargetTempMin, req.TargetTempMax, now, zoneID, merchantID)
	if err != nil {
		return nil, err
	}

	ra, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if ra == 0 {
		return nil, sql.ErrNoRows
	}

	return &Zone{
		ID:            zoneID,
		MerchantID:    merchantID,
		Name:          req.Name,
		TargetTempMin: req.TargetTempMin,
		TargetTempMax: req.TargetTempMax,
		UpdatedAt:     now,
		Enabled:       true,
	}, nil
}

func (r *Repository) SoftDeleteTemperatureZone(ctx context.Context, merchantID, zoneID string) error {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()

	res, err := db.ExecContext(ctx, `
		UPDATE temperature_zones
		SET enabled = 0, deleted_at = ?, updated_at = ?
		WHERE id = ? AND merchant_id = ? AND enabled = 1
	`, now, now, zoneID, merchantID)
	if err != nil {
		return err
	}

	ra, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if ra == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (r *Repository) FindZonesByIDs(ctx context.Context, merchantID string, zoneIDs []string) (map[string]Zone, error) {
	db := dbutils.GetDB(ctx, r.db)

	if len(zoneIDs) == 0 {
		return map[string]Zone{}, nil
	}

	placeholders := strings.Repeat("?,", len(zoneIDs))
	placeholders = strings.TrimSuffix(placeholders, ",")

	args := make([]interface{}, 0, len(zoneIDs)+1)
	args = append(args, merchantID)
	for _, id := range zoneIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT id, merchant_id, name, target_temp_min, target_temp_max, created_at, updated_at, enabled, deleted_at
		FROM temperature_zones
		WHERE merchant_id = ? AND enabled = 1 AND id IN (%s)
	`, placeholders)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]Zone, len(zoneIDs))
	for rows.Next() {
		var z Zone
		if err := rows.Scan(
			&z.ID,
			&z.MerchantID,
			&z.Name,
			&z.TargetTempMin,
			&z.TargetTempMax,
			&z.CreatedAt,
			&z.UpdatedAt,
			&z.Enabled,
			&z.DeletedAt,
		); err != nil {
			return nil, err
		}
		result[z.ID] = z
	}

	return result, rows.Err()
}

func (r *Repository) ListCorrectiveActions(ctx context.Context) ([]CorrectiveAction, error) {
	db := dbutils.GetDB(ctx, r.db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, code, label, description, severity_scope, active
		FROM haccp_corrective_actions
		WHERE active = 1
		ORDER BY label ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CorrectiveAction, 0)
	for rows.Next() {
		var action CorrectiveAction
		var description sql.NullString
		var severityScope sql.NullString
		if err := rows.Scan(&action.ID, &action.Code, &action.Label, &description, &severityScope, &action.Active); err != nil {
			return nil, err
		}
		if description.Valid {
			action.Description = description.String
		}
		if severityScope.Valid {
			action.SeverityScope = severityScope.String
		}
		out = append(out, action)
	}

	return out, rows.Err()
}

func (r *Repository) FindCorrectiveActionsByIDs(ctx context.Context, actionIDs []string) (map[string]CorrectiveAction, error) {
	db := dbutils.GetDB(ctx, r.db)

	if len(actionIDs) == 0 {
		return map[string]CorrectiveAction{}, nil
	}

	placeholders := strings.Repeat("?,", len(actionIDs))
	placeholders = strings.TrimSuffix(placeholders, ",")

	args := make([]interface{}, 0, len(actionIDs))
	for _, id := range actionIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT id, code, label, description, severity_scope, active
		FROM haccp_corrective_actions
		WHERE active = 1 AND id IN (%s)
	`, placeholders)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]CorrectiveAction, len(actionIDs))
	for rows.Next() {
		var action CorrectiveAction
		var description sql.NullString
		var severityScope sql.NullString
		if err := rows.Scan(&action.ID, &action.Code, &action.Label, &description, &severityScope, &action.Active); err != nil {
			return nil, err
		}
		if description.Valid {
			action.Description = description.String
		}
		if severityScope.Valid {
			action.SeverityScope = severityScope.String
		}
		out[action.ID] = action
	}

	return out, rows.Err()
}

func (r *Repository) CreateTemperatureSession(ctx context.Context, merchantID, createdBy string) (*TemperatureSession, error) {
	db := dbutils.GetDB(ctx, r.db)
	id := helpers.GeneratePrefixedID(helpers.HACCPTemperatureSessionIDPrefix)
	now := time.Now().UTC()

	_, err := db.ExecContext(ctx, `
		INSERT INTO temperature_sessions (id, merchant_id, created_by, created_at, updated_at, enabled)
		VALUES (?, ?, ?, ?, ?, 1)
	`, id, merchantID, createdBy, now, now)
	if err != nil {
		return nil, err
	}

	return &TemperatureSession{
		ID:         id,
		MerchantID: merchantID,
		CreatedBy:  createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func (r *Repository) InsertTemperatureReadingsBatch(ctx context.Context, merchantID, createdBy, sessionID string, readings []Reading) error {
	db := dbutils.GetDB(ctx, r.db)

	if len(readings) == 0 {
		return nil
	}

	prefix := `INSERT INTO temperature_readings (
		id, session_id, merchant_id, zone_id, value, status, photo_url, signature, comment, created_by, created_at, updated_at, enabled
	) VALUES `

	values := make([]string, 0, len(readings))
	args := make([]interface{}, 0, len(readings)*13)
	now := time.Now().UTC()

	for i := range readings {
		readings[i].ID = helpers.GeneratePrefixedID(helpers.HACCPTemperatureReadingIDPrefix)
		readings[i].SessionID = sessionID
		readings[i].MerchantID = merchantID
		readings[i].CreatedBy = createdBy
		readings[i].CreatedAt = now
		readings[i].UpdatedAt = now
		values = append(values, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)")
		args = append(args,
			readings[i].ID,
			sessionID,
			merchantID,
			readings[i].ZoneID,
			readings[i].Value,
			readings[i].Status,
			readings[i].PhotoURL,
			readings[i].Signature,
			readings[i].Comment,
			createdBy,
			now,
			now,
		)
	}

	query := prefix + strings.Join(values, ",")
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *Repository) InsertTemperatureReadingCorrectiveActionsBatch(ctx context.Context, merchantID, createdBy string, entries []readingCorrectiveActionCreate) error {
	db := dbutils.GetDB(ctx, r.db)

	if len(entries) == 0 {
		return nil
	}

	prefix := `INSERT INTO temperature_reading_corrective_actions (
		id, reading_id, action_id, merchant_id, note, photo_url, follow_up_value, created_by, created_at, updated_at, enabled
	) VALUES `

	values := make([]string, 0, len(entries))
	args := make([]interface{}, 0, len(entries)*11)
	now := time.Now().UTC()

	for i := range entries {
		values = append(values, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)")
		args = append(args,
			helpers.GeneratePrefixedID(helpers.HACCPReadingCorrectiveActionIDPrefix),
			entries[i].ReadingID,
			entries[i].ActionID,
			merchantID,
			entries[i].Note,
			entries[i].PhotoURL,
			entries[i].FollowUpValue,
			createdBy,
			now,
			now,
		)
	}

	query := prefix + strings.Join(values, ",")
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *Repository) ListTemperatureReadings(ctx context.Context, merchantID, dateValue, zoneID string) ([]Reading, error) {
	db := dbutils.GetDB(ctx, r.db)

	startAt, err := time.Parse("2006-01-02 15:04:05", dateValue)
	if err != nil {
		return nil, err
	}
	endAt := startAt.Add(24 * time.Hour)

	query := `
		SELECT id, session_id, merchant_id, zone_id, value, status, photo_url, signature, comment, created_by, created_at, updated_at
		FROM temperature_readings
		WHERE merchant_id = ?
		  AND created_at >= ?
		  AND created_at < ?
		  AND enabled = 1
	`
	args := []interface{}{merchantID, startAt.UTC(), endAt.UTC()}

	if strings.TrimSpace(zoneID) != "" {
		query += ` AND zone_id = ?`
		args = append(args, zoneID)
	}

	query += ` ORDER BY created_at ASC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	readings := make([]Reading, 0)
	for rows.Next() {
		var rd Reading
		if err := rows.Scan(
			&rd.ID,
			&rd.SessionID,
			&rd.MerchantID,
			&rd.ZoneID,
			&rd.Value,
			&rd.Status,
			&rd.PhotoURL,
			&rd.Signature,
			&rd.Comment,
			&rd.CreatedBy,
			&rd.CreatedAt,
			&rd.UpdatedAt,
		); err != nil {
			return nil, err
		}
		readings = append(readings, rd)
	}

	return readings, rows.Err()
}

func (r *Repository) GetLatestTemperatureSessionSummary(ctx context.Context, merchantID string, startAt, endAt time.Time) (*TemperatureSessionSummary, error) {
	db := dbutils.GetDB(ctx, r.db)

	var summary TemperatureSessionSummary
	err := db.QueryRowContext(ctx, `
		SELECT
			ts.id,
			ts.created_at,
			CASE
				WHEN SUM(CASE WHEN tr.status = 'critical' THEN 1 ELSE 0 END) > 0 THEN 'critical'
				WHEN SUM(CASE WHEN tr.status = 'alert' THEN 1 ELSE 0 END) > 0 THEN 'alert'
				ELSE 'ok'
			END AS status
		FROM temperature_sessions ts
		JOIN temperature_readings tr
			ON tr.session_id = ts.id
			AND tr.merchant_id = ts.merchant_id
			AND tr.enabled = 1
		WHERE ts.merchant_id = ?
		  AND ts.enabled = 1
		  AND ts.created_at >= ?
		  AND ts.created_at < ?
		GROUP BY ts.id, ts.created_at
		ORDER BY ts.created_at DESC, ts.id DESC
		LIMIT 1
	`, merchantID, startAt.UTC(), endAt.UTC()).Scan(&summary.ID, &summary.PerformedAt, &summary.Status)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &summary, nil
}

func (r *Repository) ListCompletedCleaningSurfaceIDs(ctx context.Context, merchantID string, startAt, endAt time.Time) ([]string, error) {
	db := dbutils.GetDB(ctx, r.db)

	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT ce.surface_id
		FROM cleaning_sessions cs
		JOIN cleaning_executions ce
			ON ce.session_id = cs.id
			AND ce.merchant_id = cs.merchant_id
			AND ce.enabled = 1
		JOIN cleaning_surfaces s
			ON s.id = ce.surface_id
			AND s.merchant_id = ce.merchant_id
			AND s.enabled = 1
		WHERE cs.merchant_id = ?
		  AND cs.enabled = 1
		  AND cs.created_at >= ?
		  AND cs.created_at < ?
	`, merchantID, startAt.UTC(), endAt.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

func (r *Repository) GetOrCreateSettings(ctx context.Context, merchantID string) (*HACCPSettings, error) {
	db := dbutils.GetDB(ctx, r.db)

	s, err := r.getSettings(ctx, merchantID)
	if err == nil {
		return s, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO haccp_settings (merchant_id, created_at, updated_at)
		VALUES (?, UTC_TIMESTAMP(), UTC_TIMESTAMP())
	`, merchantID)
	if err != nil {
		return nil, err
	}

	return r.getSettings(ctx, merchantID)
}

func (r *Repository) getSettings(ctx context.Context, merchantID string) (*HACCPSettings, error) {
	db := dbutils.GetDB(ctx, r.db)

	var s HACCPSettings
	err := db.QueryRowContext(ctx, `
		SELECT
			merchant_id,
			temp_entry_required,
			temp_corrective_actions,
			temp_failure_photo_required,
			temp_block_past_dates,
			traceability_product_name,
			traceability_block_past_dates,
			cleaning_photo,
			cleaning_block_past_dates,
			reception_other_products,
			reception_control_sample,
			reception_block_past_dates,
			reception_photo,
			reception_non_conformities,
			oils_block_past_dates,
			oils_polar_compound_rate,
			oils_photo,
			production_block_past_dates,
			production_traceability,
			cooling_block_past_dates,
			freezing_block_past_dates,
			reheating_block_past_dates,
			holding_block_past_dates,
			holding_corrective_actions,
			notif_authorization,
			notif_security
		FROM haccp_settings
		WHERE merchant_id = ?
		LIMIT 1
	`, merchantID).Scan(
		&s.MerchantID,
		&s.TempEntryRequired,
		&s.TempCorrectiveActions,
		&s.TempFailurePhotoRequired,
		&s.TempBlockPastDates,
		&s.TraceabilityProductName,
		&s.TraceabilityBlockPastDates,
		&s.CleaningPhoto,
		&s.CleaningBlockPastDates,
		&s.ReceptionOtherProducts,
		&s.ReceptionControlSample,
		&s.ReceptionBlockPastDates,
		&s.ReceptionPhoto,
		&s.ReceptionNonConformities,
		&s.OilsBlockPastDates,
		&s.OilsPolarCompoundRate,
		&s.OilsPhoto,
		&s.ProductionBlockPastDates,
		&s.ProductionTraceability,
		&s.CoolingBlockPastDates,
		&s.FreezingBlockPastDates,
		&s.ReheatingBlockPastDates,
		&s.HoldingBlockPastDates,
		&s.HoldingCorrectiveActions,
		&s.NotifAuthorization,
		&s.NotifSecurity,
	)
	if err != nil {
		return nil, err
	}

	return &s, nil
}

func (r *Repository) ReplaceSettings(ctx context.Context, merchantID string, req HACCPSettings) (*HACCPSettings, error) {
	db := dbutils.GetDB(ctx, r.db)

	_, err := db.ExecContext(ctx, `
		UPDATE haccp_settings
		SET
			temp_entry_required = ?,
			temp_corrective_actions = ?,
			temp_failure_photo_required = ?,
			temp_block_past_dates = ?,
			traceability_product_name = ?,
			traceability_block_past_dates = ?,
			cleaning_photo = ?,
			cleaning_block_past_dates = ?,
			reception_other_products = ?,
			reception_control_sample = ?,
			reception_block_past_dates = ?,
			reception_photo = ?,
			reception_non_conformities = ?,
			oils_block_past_dates = ?,
			oils_polar_compound_rate = ?,
			oils_photo = ?,
			production_block_past_dates = ?,
			production_traceability = ?,
			cooling_block_past_dates = ?,
			freezing_block_past_dates = ?,
			reheating_block_past_dates = ?,
			holding_block_past_dates = ?,
			holding_corrective_actions = ?,
			notif_authorization = ?,
			notif_security = ?,
			updated_at = UTC_TIMESTAMP()
		WHERE merchant_id = ?
	`,
		req.TempEntryRequired,
		req.TempCorrectiveActions,
		req.TempFailurePhotoRequired,
		req.TempBlockPastDates,
		req.TraceabilityProductName,
		req.TraceabilityBlockPastDates,
		req.CleaningPhoto,
		req.CleaningBlockPastDates,
		req.ReceptionOtherProducts,
		req.ReceptionControlSample,
		req.ReceptionBlockPastDates,
		req.ReceptionPhoto,
		req.ReceptionNonConformities,
		req.OilsBlockPastDates,
		req.OilsPolarCompoundRate,
		req.OilsPhoto,
		req.ProductionBlockPastDates,
		req.ProductionTraceability,
		req.CoolingBlockPastDates,
		req.FreezingBlockPastDates,
		req.ReheatingBlockPastDates,
		req.HoldingBlockPastDates,
		req.HoldingCorrectiveActions,
		req.NotifAuthorization,
		req.NotifSecurity,
		merchantID,
	)
	if err != nil {
		return nil, err
	}

	return r.getSettings(ctx, merchantID)
}

func (r *Repository) ListCleaningZones(ctx context.Context, merchantID string) ([]CleaningZone, error) {
	db := dbutils.GetDB(ctx, r.db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, name, enabled, created_at, updated_at, deleted_at
		FROM cleaning_zones
		WHERE merchant_id = ? AND enabled = 1
		ORDER BY created_at DESC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CleaningZone, 0)
	for rows.Next() {
		var zone CleaningZone
		if err := rows.Scan(&zone.ID, &zone.MerchantID, &zone.Name, &zone.Enabled, &zone.CreatedAt, &zone.UpdatedAt, &zone.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, zone)
	}

	return out, rows.Err()
}

func (r *Repository) CreateCleaningZone(ctx context.Context, merchantID string, req CreateCleaningZoneRequest) (*CleaningZone, error) {
	db := dbutils.GetDB(ctx, r.db)
	id := helpers.GeneratePrefixedID(helpers.HACCPCleaningZoneIDPrefix)
	now := time.Now().UTC()

	_, err := db.ExecContext(ctx, `
		INSERT INTO cleaning_zones (id, merchant_id, name, enabled, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
	`, id, merchantID, req.Name, now, now)
	if err != nil {
		return nil, err
	}

	return &CleaningZone{
		ID:         id,
		MerchantID: merchantID,
		Name:       req.Name,
		Enabled:    true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func (r *Repository) UpdateCleaningZone(ctx context.Context, merchantID, zoneID string, req UpdateCleaningZoneRequest) (*CleaningZone, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()

	res, err := db.ExecContext(ctx, `
		UPDATE cleaning_zones
		SET name = ?, updated_at = ?, enabled = 1, deleted_at = NULL
		WHERE id = ? AND merchant_id = ?
	`, req.Name, now, zoneID, merchantID)
	if err != nil {
		return nil, err
	}

	ra, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if ra == 0 {
		return nil, sql.ErrNoRows
	}

	return &CleaningZone{
		ID:         zoneID,
		MerchantID: merchantID,
		Name:       req.Name,
		Enabled:    true,
		UpdatedAt:  now,
	}, nil
}

func (r *Repository) SoftDeleteCleaningZone(ctx context.Context, merchantID, zoneID string) error {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()

	res, err := db.ExecContext(ctx, `
		UPDATE cleaning_zones
		SET enabled = 0, deleted_at = ?, updated_at = ?
		WHERE id = ? AND merchant_id = ? AND enabled = 1
	`, now, now, zoneID, merchantID)
	if err != nil {
		return err
	}

	ra, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if ra == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (r *Repository) GetCleaningZoneByID(ctx context.Context, merchantID, zoneID string) (*CleaningZone, error) {
	db := dbutils.GetDB(ctx, r.db)

	var zone CleaningZone
	err := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, name, enabled, created_at, updated_at, deleted_at
		FROM cleaning_zones
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`, merchantID, zoneID).Scan(&zone.ID, &zone.MerchantID, &zone.Name, &zone.Enabled, &zone.CreatedAt, &zone.UpdatedAt, &zone.DeletedAt)
	if err != nil {
		return nil, err
	}

	return &zone, nil
}

func (r *Repository) ListCleaningSurfaces(ctx context.Context, merchantID, zoneID string) ([]CleaningSurfaceWithComputed, error) {
	db := dbutils.GetDB(ctx, r.db)

	query := `
		SELECT
			s.id,
			s.zone_id,
			z.name,
			s.name,
			s.frequency_unit,
			s.frequency_count,
			s.active,
			MAX(e.created_at) AS last_execution_at
		FROM cleaning_surfaces s
		JOIN cleaning_zones z ON z.id = s.zone_id AND z.enabled = 1
		LEFT JOIN cleaning_executions e
			ON e.surface_id = s.id
			AND e.merchant_id = s.merchant_id
			AND e.enabled = 1
		WHERE s.merchant_id = ?
		  AND s.enabled = 1
	`
	args := []interface{}{merchantID}
	if strings.TrimSpace(zoneID) != "" {
		query += ` AND s.zone_id = ?`
		args = append(args, zoneID)
	}
	query += `
		GROUP BY s.id, s.zone_id, z.name, s.name, s.frequency_unit, s.frequency_count, s.active
		ORDER BY z.name ASC, s.name ASC
	`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CleaningSurfaceWithComputed, 0)
	for rows.Next() {
		var item CleaningSurfaceWithComputed
		var lastExec sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.ZoneID,
			&item.ZoneName,
			&item.Name,
			&item.FrequencyUnit,
			&item.FrequencyCount,
			&item.Active,
			&lastExec,
		); err != nil {
			return nil, err
		}
		if lastExec.Valid {
			v := lastExec.Time.UTC()
			item.Computed.LastExecutionAt = &v
		}
		out = append(out, item)
	}

	return out, rows.Err()
}

func (r *Repository) CreateCleaningSurface(ctx context.Context, merchantID string, req CreateCleaningSurfaceRequest) (*CleaningSurface, error) {
	db := dbutils.GetDB(ctx, r.db)
	id := helpers.GeneratePrefixedID(helpers.HACCPCleaningSurfaceIDPrefix)
	now := time.Now().UTC()
	active := true
	if req.Active != nil {
		active = *req.Active
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO cleaning_surfaces (
			id, merchant_id, zone_id, name, frequency_unit, frequency_count, active, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, id, merchantID, req.ZoneID, req.Name, req.FrequencyUnit, req.FrequencyCount, active, now, now)
	if err != nil {
		return nil, err
	}

	zone, err := r.GetCleaningZoneByID(ctx, merchantID, req.ZoneID)
	if err != nil {
		return nil, err
	}

	return &CleaningSurface{
		ID:             id,
		MerchantID:     merchantID,
		ZoneID:         req.ZoneID,
		ZoneName:       zone.Name,
		Name:           req.Name,
		FrequencyUnit:  req.FrequencyUnit,
		FrequencyCount: req.FrequencyCount,
		Active:         active,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (r *Repository) UpdateCleaningSurface(ctx context.Context, merchantID, surfaceID string, req UpdateCleaningSurfaceRequest) (*CleaningSurface, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	active := true
	if req.Active != nil {
		active = *req.Active
	}

	res, err := db.ExecContext(ctx, `
		UPDATE cleaning_surfaces
		SET zone_id = ?, name = ?, frequency_unit = ?, frequency_count = ?, active = ?, updated_at = ?, enabled = 1, deleted_at = NULL
		WHERE id = ? AND merchant_id = ?
	`, req.ZoneID, req.Name, req.FrequencyUnit, req.FrequencyCount, active, now, surfaceID, merchantID)
	if err != nil {
		return nil, err
	}

	ra, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if ra == 0 {
		return nil, sql.ErrNoRows
	}

	zone, err := r.GetCleaningZoneByID(ctx, merchantID, req.ZoneID)
	if err != nil {
		return nil, err
	}

	return &CleaningSurface{
		ID:             surfaceID,
		MerchantID:     merchantID,
		ZoneID:         req.ZoneID,
		ZoneName:       zone.Name,
		Name:           req.Name,
		FrequencyUnit:  req.FrequencyUnit,
		FrequencyCount: req.FrequencyCount,
		Active:         active,
		Enabled:        true,
		UpdatedAt:      now,
	}, nil
}

func (r *Repository) SoftDeleteCleaningSurface(ctx context.Context, merchantID, surfaceID string) error {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()

	res, err := db.ExecContext(ctx, `
		UPDATE cleaning_surfaces
		SET enabled = 0, deleted_at = ?, updated_at = ?
		WHERE id = ? AND merchant_id = ? AND enabled = 1
	`, now, now, surfaceID, merchantID)
	if err != nil {
		return err
	}

	ra, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if ra == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (r *Repository) GetCleaningSurfaceByID(ctx context.Context, merchantID, surfaceID string) (*CleaningSurface, error) {
	db := dbutils.GetDB(ctx, r.db)

	var surface CleaningSurface
	err := db.QueryRowContext(ctx, `
		SELECT s.id, s.merchant_id, s.zone_id, z.name, s.name, s.frequency_unit, s.frequency_count, s.active, s.enabled, s.created_at, s.updated_at, s.deleted_at
		FROM cleaning_surfaces s
		JOIN cleaning_zones z ON z.id = s.zone_id
		WHERE s.merchant_id = ? AND s.id = ? AND s.enabled = 1
		LIMIT 1
	`, merchantID, surfaceID).Scan(
		&surface.ID,
		&surface.MerchantID,
		&surface.ZoneID,
		&surface.ZoneName,
		&surface.Name,
		&surface.FrequencyUnit,
		&surface.FrequencyCount,
		&surface.Active,
		&surface.Enabled,
		&surface.CreatedAt,
		&surface.UpdatedAt,
		&surface.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	return &surface, nil
}

func (r *Repository) FindCleaningSurfacesByIDs(ctx context.Context, merchantID string, surfaceIDs []string) (map[string]CleaningSurface, error) {
	db := dbutils.GetDB(ctx, r.db)
	if len(surfaceIDs) == 0 {
		return map[string]CleaningSurface{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(surfaceIDs)), ",")
	args := make([]interface{}, 0, len(surfaceIDs)+1)
	args = append(args, merchantID)
	for _, id := range surfaceIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT s.id, s.merchant_id, s.zone_id, z.name, s.name, s.frequency_unit, s.frequency_count, s.active, s.enabled, s.created_at, s.updated_at, s.deleted_at
		FROM cleaning_surfaces s
		JOIN cleaning_zones z ON z.id = s.zone_id AND z.enabled = 1
		WHERE s.merchant_id = ? AND s.enabled = 1 AND s.id IN (%s)
	`, placeholders)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]CleaningSurface, len(surfaceIDs))
	for rows.Next() {
		var surface CleaningSurface
		if err := rows.Scan(
			&surface.ID,
			&surface.MerchantID,
			&surface.ZoneID,
			&surface.ZoneName,
			&surface.Name,
			&surface.FrequencyUnit,
			&surface.FrequencyCount,
			&surface.Active,
			&surface.Enabled,
			&surface.CreatedAt,
			&surface.UpdatedAt,
			&surface.DeletedAt,
		); err != nil {
			return nil, err
		}
		result[surface.ID] = surface
	}

	return result, rows.Err()
}

func (r *Repository) CreateCleaningSession(ctx context.Context, merchantID, createdBy string) (*CleaningSession, error) {
	db := dbutils.GetDB(ctx, r.db)
	id := helpers.GeneratePrefixedID(helpers.HACCPCleaningSessionIDPrefix)
	now := time.Now().UTC()

	_, err := db.ExecContext(ctx, `
		INSERT INTO cleaning_sessions (id, merchant_id, status, created_by, created_at, updated_at, enabled)
		VALUES (?, ?, 'done', ?, ?, ?, 1)
	`, id, merchantID, createdBy, now, now)
	if err != nil {
		return nil, err
	}

	return &CleaningSession{
		ID:         id,
		MerchantID: merchantID,
		Status:     "done",
		CreatedBy:  createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func (r *Repository) InsertCleaningExecutionsBatch(ctx context.Context, merchantID, createdBy, sessionID string, executions []CleaningExecution) error {
	db := dbutils.GetDB(ctx, r.db)
	if len(executions) == 0 {
		return nil
	}

	prefix := `INSERT INTO cleaning_executions (
		id, session_id, merchant_id, surface_id, comment, photo_url, status, created_by, created_at, updated_at, enabled
	) VALUES `
	values := make([]string, 0, len(executions))
	args := make([]interface{}, 0, len(executions)*10)
	now := time.Now().UTC()

	for i := range executions {
		executions[i].ID = helpers.GeneratePrefixedID(helpers.HACCPCleaningExecutionIDPrefix)
		executions[i].SessionID = sessionID
		executions[i].MerchantID = merchantID
		executions[i].Status = "done"
		executions[i].CreatedBy = createdBy
		executions[i].CreatedAt = now
		executions[i].UpdatedAt = now
		values = append(values, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)")
		args = append(args,
			executions[i].ID,
			sessionID,
			merchantID,
			executions[i].SurfaceID,
			executions[i].Comment,
			executions[i].PhotoURL,
			executions[i].Status,
			createdBy,
			now,
			now,
		)
	}

	_, err := db.ExecContext(ctx, prefix+strings.Join(values, ","), args...)
	return err
}

func (r *Repository) ListCleaningSessions(ctx context.Context, merchantID, dateValue, zoneID string) ([]CleaningSessionListItem, error) {
	db := dbutils.GetDB(ctx, r.db)

	startAt, err := time.Parse("2006-01-02 15:04:05", dateValue)
	if err != nil {
		return nil, err
	}
	endAt := startAt.Add(24 * time.Hour)

	query := `
		SELECT
			cs.id,
			cs.status,
			cs.created_at,
			cs.created_by,
			COALESCE(u.name, cs.created_by),
			COUNT(ce.id)
		FROM cleaning_sessions cs
		JOIN cleaning_executions ce ON ce.session_id = cs.id AND ce.enabled = 1
		JOIN cleaning_surfaces s ON s.id = ce.surface_id AND s.enabled = 1
		LEFT JOIN users u ON u.user_id = cs.created_by
		WHERE cs.merchant_id = ?
		  AND cs.enabled = 1
		  AND cs.created_at >= ?
		  AND cs.created_at < ?
	`
	args := []interface{}{merchantID, startAt.UTC(), endAt.UTC()}
	if strings.TrimSpace(zoneID) != "" {
		query += ` AND s.zone_id = ?`
		args = append(args, zoneID)
	}
	query += `
		GROUP BY cs.id, cs.status, cs.created_at, cs.created_by, u.name
		ORDER BY cs.created_at DESC, cs.id DESC
	`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CleaningSessionListItem, 0)
	for rows.Next() {
		var item CleaningSessionListItem
		if err := rows.Scan(
			&item.ID,
			&item.Status,
			&item.PerformedAt,
			&item.PerformedBy.ID,
			&item.PerformedBy.Name,
			&item.ExecutionsCount,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}

	return out, rows.Err()
}

func (r *Repository) GetTemperatureSessionDetail(ctx context.Context, merchantID, sessionID string) (*TemperatureSessionDetail, error) {
	db := dbutils.GetDB(ctx, r.db)

	var session TemperatureSessionDetail
	err := db.QueryRowContext(ctx, `
		SELECT ts.id, ts.merchant_id, ts.created_by, COALESCE(u.name, ts.created_by), ts.created_at
		FROM temperature_sessions ts
		LEFT JOIN users u ON u.user_id = ts.created_by
		WHERE ts.merchant_id = ?
		  AND ts.id = ?
		  AND ts.enabled = 1
		LIMIT 1
	`, merchantID, sessionID).Scan(
		&session.ID,
		&session.MerchantID,
		&session.PerformedBy.ID,
		&session.PerformedBy.Name,
		&session.PerformedAt,
	)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT tr.id, tr.session_id, tr.merchant_id, tr.zone_id, tz.name, tr.value, tr.status, tr.photo_url, tr.signature, tr.comment, tr.created_by, tr.created_at, tr.updated_at
		FROM temperature_readings tr
		LEFT JOIN temperature_zones tz ON tz.id = tr.zone_id
		WHERE tr.merchant_id = ?
		  AND tr.session_id = ?
		  AND tr.enabled = 1
		ORDER BY tr.created_at ASC, tr.id ASC
	`, merchantID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	readings := make([]Reading, 0)
	for rows.Next() {
		var rd Reading
		var zoneName sql.NullString
		if err := rows.Scan(
			&rd.ID,
			&rd.SessionID,
			&rd.MerchantID,
			&rd.ZoneID,
			&zoneName,
			&rd.Value,
			&rd.Status,
			&rd.PhotoURL,
			&rd.Signature,
			&rd.Comment,
			&rd.CreatedBy,
			&rd.CreatedAt,
			&rd.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if zoneName.Valid {
			name := zoneName.String
			rd.ZoneName = &name
		}
		readings = append(readings, rd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	session.Readings = readings

	if len(session.Readings) == 0 {
		return &session, nil
	}

	correctiveRows, err := db.QueryContext(ctx, `
		SELECT
			rca.id,
			rca.reading_id,
			rca.action_id,
			ca.code,
			ca.label,
			rca.note,
			rca.photo_url,
			rca.follow_up_value
		FROM temperature_reading_corrective_actions rca
		JOIN haccp_corrective_actions ca
			ON ca.id = rca.action_id
		WHERE rca.merchant_id = ?
		  AND rca.enabled = 1
		  AND rca.reading_id IN (
			SELECT tr.id
			FROM temperature_readings tr
			WHERE tr.merchant_id = ?
			  AND tr.session_id = ?
			  AND tr.enabled = 1
		  )
		ORDER BY rca.created_at ASC, rca.id ASC
	`, merchantID, merchantID, sessionID)
	if err != nil {
		return nil, err
	}
	defer correctiveRows.Close()

	byReading := make(map[string][]ReadingCorrectiveAction)
	for correctiveRows.Next() {
		var item ReadingCorrectiveAction
		var readingID string
		var note sql.NullString
		var followUp sql.NullFloat64
		if err := correctiveRows.Scan(&item.ID, &readingID, &item.ActionID, &item.Code, &item.Label, &note, &item.PhotoURL, &followUp); err != nil {
			return nil, err
		}
		if note.Valid {
			v := note.String
			item.Note = &v
		}
		if followUp.Valid {
			v := followUp.Float64
			item.FollowUpValue = &v
		}
		byReading[readingID] = append(byReading[readingID], item)
	}
	if err := correctiveRows.Err(); err != nil {
		return nil, err
	}

	for i := range session.Readings {
		session.Readings[i].CorrectiveActions = byReading[session.Readings[i].ID]
	}

	return &session, nil
}

func (r *Repository) GetCleaningSessionDetail(ctx context.Context, merchantID, sessionID string) (*CleaningSessionDetail, error) {
	db := dbutils.GetDB(ctx, r.db)

	var detail CleaningSessionDetail
	err := db.QueryRowContext(ctx, `
		SELECT cs.id, cs.merchant_id, cs.status, cs.created_at, cs.created_by, COALESCE(u.name, cs.created_by)
		FROM cleaning_sessions cs
		LEFT JOIN users u ON u.user_id = cs.created_by
		WHERE cs.merchant_id = ?
		  AND cs.id = ?
		  AND cs.enabled = 1
		LIMIT 1
	`, merchantID, sessionID).Scan(
		&detail.ID,
		&detail.MerchantID,
		&detail.Status,
		&detail.PerformedAt,
		&detail.PerformedBy.ID,
		&detail.PerformedBy.Name,
	)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT ce.id, ce.session_id, ce.surface_id, s.name, s.zone_id, z.name, ce.merchant_id, ce.comment, ce.photo_url, ce.status, ce.created_by, COALESCE(u.name, ce.created_by), ce.created_at, ce.updated_at
		FROM cleaning_executions ce
		JOIN cleaning_surfaces s ON s.id = ce.surface_id
		JOIN cleaning_zones z ON z.id = s.zone_id
		LEFT JOIN users u ON u.user_id = ce.created_by
		WHERE ce.merchant_id = ?
		  AND ce.session_id = ?
		  AND ce.enabled = 1
		ORDER BY z.name ASC, s.name ASC, ce.created_at ASC, ce.id ASC
	`, merchantID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	executions := make([]CleaningExecution, 0)
	for rows.Next() {
		var execution CleaningExecution
		if err := rows.Scan(
			&execution.ID,
			&execution.SessionID,
			&execution.SurfaceID,
			&execution.SurfaceName,
			&execution.ZoneID,
			&execution.ZoneName,
			&execution.MerchantID,
			&execution.Comment,
			&execution.PhotoURL,
			&execution.Status,
			&execution.CreatedBy,
			&execution.PerformedBy.Name,
			&execution.CreatedAt,
			&execution.UpdatedAt,
		); err != nil {
			return nil, err
		}
		execution.PerformedBy.ID = execution.CreatedBy
		executions = append(executions, execution)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	detail.Executions = executions

	return &detail, nil
}

func (r *Repository) ListActivities(ctx context.Context, merchantID string, startAt, endAt time.Time, activityType, activityStatus string, page, pageSize int) ([]ActivityItem, int, error) {
	db := dbutils.GetDB(ctx, r.db)

	temperatureQuery := `
		SELECT
			ts.id AS id,
			'temperatures' AS activity_type,
			CASE
				WHEN SUM(CASE WHEN tr.status = 'critical' THEN 1 ELSE 0 END) > 0 THEN 'critical'
				WHEN SUM(CASE WHEN tr.status = 'alert' THEN 1 ELSE 0 END) > 0 THEN 'alert'
				ELSE 'ok'
			END AS status,
			ts.created_by AS performed_by_id,
			COALESCE(u.name, ts.created_by) AS performed_by_name,
			ts.created_at AS performed_at,
			'Releve de temperatures' AS title,
			CONCAT(COUNT(tr.id), ' zones controlees') AS subtitle,
			COUNT(tr.id) AS readings_count,
			NULL AS executions_count
		FROM temperature_sessions ts
		JOIN temperature_readings tr
			ON tr.session_id = ts.id
			AND tr.merchant_id = ts.merchant_id
			AND tr.enabled = 1
		LEFT JOIN users u ON u.user_id = ts.created_by
		WHERE ts.merchant_id = ?
		  AND ts.enabled = 1
		  AND ts.created_at >= ?
		  AND ts.created_at < ?
		GROUP BY ts.id, ts.created_by, u.name, ts.created_at
	`
	temperatureArgs := []interface{}{merchantID, startAt.UTC(), endAt.UTC()}

	cleaningQuery := `
		SELECT
			cs.id AS id,
			'cleanings' AS activity_type,
			cs.status AS status,
			cs.created_by AS performed_by_id,
			COALESCE(u.name, cs.created_by) AS performed_by_name,
			cs.created_at AS performed_at,
			'Session de nettoyage' AS title,
			CONCAT(COUNT(ce.id), ' surfaces nettoyees') AS subtitle,
			NULL AS readings_count,
			COUNT(ce.id) AS executions_count
		FROM cleaning_sessions cs
		JOIN cleaning_executions ce
			ON ce.session_id = cs.id
			AND ce.merchant_id = cs.merchant_id
			AND ce.enabled = 1
		LEFT JOIN users u ON u.user_id = cs.created_by
		WHERE cs.merchant_id = ?
		  AND cs.enabled = 1
		  AND cs.created_at >= ?
		  AND cs.created_at < ?
		GROUP BY cs.id, cs.status, cs.created_by, u.name, cs.created_at
	`
	cleaningArgs := []interface{}{merchantID, startAt.UTC(), endAt.UTC()}

	var unionParts []string
	args := make([]interface{}, 0, 6)

	switch activityType {
	case ActivityTypeTemperatures:
		unionParts = append(unionParts, temperatureQuery)
		args = append(args, temperatureArgs...)
	case ActivityTypeCleanings:
		unionParts = append(unionParts, cleaningQuery)
		args = append(args, cleaningArgs...)
	default:
		unionParts = append(unionParts, temperatureQuery, cleaningQuery)
		args = append(args, temperatureArgs...)
		args = append(args, cleaningArgs...)
	}

	baseQuery := strings.Join(unionParts, " UNION ALL ")
	wrappedBaseQuery := "SELECT * FROM (" + baseQuery + ") AS haccp_activities"

	countArgs := append([]interface{}{}, args...)
	if strings.TrimSpace(activityStatus) != "" {
		wrappedBaseQuery += " WHERE status = ?"
		countArgs = append(countArgs, activityStatus)
	}

	countQuery := "SELECT COUNT(*) FROM (" + wrappedBaseQuery + ") AS filtered_haccp_activities"

	var totalItems int
	if err := db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalItems); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	dataQuery := wrappedBaseQuery + " ORDER BY performed_at DESC, id DESC LIMIT ? OFFSET ?"
	dataArgs := append(countArgs, pageSize, offset)

	rows, err := db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ActivityItem, 0, pageSize)
	for rows.Next() {
		var item ActivityItem
		var status sql.NullString
		var readingsCount sql.NullInt64
		var executionsCount sql.NullInt64

		if err := rows.Scan(
			&item.ID,
			&item.Type,
			&status,
			&item.PerformedBy.ID,
			&item.PerformedBy.Name,
			&item.PerformedAt,
			&item.Title,
			&item.Subtitle,
			&readingsCount,
			&executionsCount,
		); err != nil {
			return nil, 0, err
		}

		if status.Valid {
			item.Status = &status.String
		}

		metadata := make(map[string]any)
		switch item.Type {
		case ActivityTypeTemperatures:
			metadata["session_id"] = item.ID
			if readingsCount.Valid {
				metadata["readings_count"] = readingsCount.Int64
			}
		case ActivityTypeCleanings:
			metadata["session_id"] = item.ID
			if executionsCount.Valid {
				metadata["executions_count"] = executionsCount.Int64
			}
		}
		if len(metadata) > 0 {
			item.Metadata = metadata
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, totalItems, nil
}

func (r *Repository) CreateGoodsReceipt(ctx context.Context, merchantID, createdBy string, req CreateGoodsReceiptRequest) (*GoodsReceipt, error) {
	db := dbutils.GetDB(ctx, r.db)
	id := helpers.GeneratePrefixedID(helpers.HACCPGoodsReceiptIDPrefix)
	now := time.Now().UTC()

	nonConformitiesJSON, err := json.Marshal(req.NonConformities)
	if err != nil {
		return nil, err
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO goods_receipts (
			id,
			merchant_id,
			supplier,
			product_type,
			batch_number,
			product_temp,
			control_sample,
			quantities_verified,
			non_conformities,
			comment,
			invoice_url,
			created_by,
			created_at,
			updated_at,
			enabled
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	`,
		id,
		merchantID,
		req.Supplier,
		req.ProductType,
		req.BatchNumber,
		req.ProductTemp,
		req.ControlSample,
		req.QuantitiesVerified,
		nonConformitiesJSON,
		req.Comment,
		req.InvoiceURL,
		createdBy,
		now,
		now,
	)
	if err != nil {
		return nil, err
	}

	return &GoodsReceipt{
		ID:                 id,
		MerchantID:         merchantID,
		Supplier:           req.Supplier,
		ProductType:        req.ProductType,
		BatchNumber:        req.BatchNumber,
		ProductTemp:        req.ProductTemp,
		ControlSample:      req.ControlSample,
		QuantitiesVerified: req.QuantitiesVerified,
		NonConformities:    req.NonConformities,
		Comment:            req.Comment,
		InvoiceURL:         req.InvoiceURL,
		CreatedBy:          createdBy,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func (r *Repository) GetHaccpComponents(ctx context.Context, merchantID string) ([]HaccpComponentCategory, error) {
	db := dbutils.GetDB(ctx, r.db)

	type catTmp struct {
		ID    string
		Name  string
		Order int
	}
	var cats []catTmp
	{
		rows, err := db.QueryContext(ctx, `
			SELECT merchant_categ_id, name, categ_order
			FROM component_category
			WHERE merchant_id = ? AND enabled = 1
			ORDER BY categ_order ASC
		`, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var c catTmp
			if err := rows.Scan(&c.ID, &c.Name, &c.Order); err != nil {
				return nil, err
			}
			cats = append(cats, c)
		}
	}

	type compTmp struct {
		ID               string
		Name             string
		CatID            *string
		UnitOfMeasure    sql.NullString
		ConservationDays sql.NullInt64
		ConservationType sql.NullString
		StorageTempMin   sql.NullFloat64
		StorageTempMax   sql.NullFloat64
		Status           string
	}
	var comps []compTmp
	{
		rows, err := db.QueryContext(ctx, `
			SELECT
				c.component_id,
				c.name,
				c.category_id,
				COALESCE(uomd.uom_short_desc, uomd.uom_desc, '') AS uom,
				c.conservation_days,
				c.conservation_type,
				c.storage_temp_min,
				c.storage_temp_max,
				COALESCE(c.status, '') AS status
			FROM components c
			LEFT JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = c.unit_of_measure
			WHERE c.merchant_id = ? AND c.enabled = 1
			ORDER BY c.name ASC
		`, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var c compTmp
			if err := rows.Scan(&c.ID, &c.Name, &c.CatID, &c.UnitOfMeasure, &c.ConservationDays, &c.ConservationType, &c.StorageTempMin, &c.StorageTempMax, &c.Status); err != nil {
				return nil, err
			}
			comps = append(comps, c)
		}
	}

	result := []HaccpComponentCategory{}
	for _, cat := range cats {
		items := []HaccpComponent{}
		for _, comp := range comps {
			if comp.CatID == nil || *comp.CatID != cat.ID {
				continue
			}
			var conservationDays *int
			if comp.ConservationDays.Valid {
				cd := int(comp.ConservationDays.Int64)
				conservationDays = &cd
			}
			conservationType := "froid"
			if comp.ConservationType.Valid && comp.ConservationType.String != "" {
				conservationType = comp.ConservationType.String
			}
			var storageTempMin *float64
			if comp.StorageTempMin.Valid {
				v := comp.StorageTempMin.Float64
				storageTempMin = &v
			}
			var storageTempMax *float64
			if comp.StorageTempMax.Valid {
				v := comp.StorageTempMax.Float64
				storageTempMax = &v
			}
			uom := ""
			if comp.UnitOfMeasure.Valid {
				uom = comp.UnitOfMeasure.String
			}
			items = append(items, HaccpComponent{
				ComponentID:      comp.ID,
				Name:             comp.Name,
				Category:         cat.Name,
				UnitOfMeasure:    uom,
				ConservationDays: conservationDays,
				ConservationType: conservationType,
				StorageTempMin:   storageTempMin,
				StorageTempMax:   storageTempMax,
				Status:           comp.Status,
			})
		}
		result = append(result, HaccpComponentCategory{
			CategoryName: cat.Name,
			CategoryID:   cat.ID,
			Order:        cat.Order,
			Components:   items,
		})
	}
	return result, nil
}
