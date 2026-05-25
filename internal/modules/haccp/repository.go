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

func (r *Repository) ListCleaningTasks(ctx context.Context, merchantID string) ([]CleaningTaskWithComputed, error) {
	db := dbutils.GetDB(ctx, r.db)

	rows, err := db.QueryContext(ctx, `
		SELECT
			t.id,
			t.zone,
			t.name,
			t.frequency_unit,
			t.frequency_count,
			t.active,
			MAX(e.created_at) AS last_execution_at
		FROM cleaning_tasks t
		LEFT JOIN cleaning_executions e
			ON e.task_id = t.id
			AND e.merchant_id = t.merchant_id
			AND e.enabled = 1
		WHERE t.merchant_id = ?
		  AND t.enabled = 1
		GROUP BY t.id, t.zone, t.name, t.frequency_unit, t.frequency_count, t.active
		ORDER BY t.created_at DESC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CleaningTaskWithComputed, 0)
	for rows.Next() {
		var item CleaningTaskWithComputed
		var lastExec sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.Zone,
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

func (r *Repository) CreateCleaningTask(ctx context.Context, merchantID string, req CreateCleaningTaskRequest) (*CleaningTask, error) {
	db := dbutils.GetDB(ctx, r.db)
	id := helpers.GeneratePrefixedID(helpers.HACCPCleaningTaskIDPrefix)
	now := time.Now().UTC()
	active := true
	if req.Active != nil {
		active = *req.Active
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO cleaning_tasks (
			id, merchant_id, zone, name, frequency_unit, frequency_count, active, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, id, merchantID, req.Zone, req.Name, req.FrequencyUnit, req.FrequencyCount, active, now, now)
	if err != nil {
		return nil, err
	}

	return &CleaningTask{
		ID:             id,
		MerchantID:     merchantID,
		Zone:           req.Zone,
		Name:           req.Name,
		FrequencyUnit:  req.FrequencyUnit,
		FrequencyCount: req.FrequencyCount,
		Active:         active,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (r *Repository) UpdateCleaningTask(ctx context.Context, merchantID, taskID string, req UpdateCleaningTaskRequest) (*CleaningTask, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	active := true
	if req.Active != nil {
		active = *req.Active
	}

	res, err := db.ExecContext(ctx, `
		UPDATE cleaning_tasks
		SET zone = ?, name = ?, frequency_unit = ?, frequency_count = ?, active = ?, updated_at = ?
		WHERE id = ? AND merchant_id = ? AND enabled = 1
	`, req.Zone, req.Name, req.FrequencyUnit, req.FrequencyCount, active, now, taskID, merchantID)
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

	return &CleaningTask{
		ID:             taskID,
		MerchantID:     merchantID,
		Zone:           req.Zone,
		Name:           req.Name,
		FrequencyUnit:  req.FrequencyUnit,
		FrequencyCount: req.FrequencyCount,
		Active:         active,
		Enabled:        true,
		UpdatedAt:      now,
	}, nil
}

func (r *Repository) SoftDeleteCleaningTask(ctx context.Context, merchantID, taskID string) error {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()

	res, err := db.ExecContext(ctx, `
		UPDATE cleaning_tasks
		SET enabled = 0, deleted_at = ?, updated_at = ?
		WHERE id = ? AND merchant_id = ? AND enabled = 1
	`, now, now, taskID, merchantID)
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

func (r *Repository) ListCleaningExecutions(ctx context.Context, merchantID, taskID string, page, pageSize int) ([]CleaningExecution, int, error) {
	db := dbutils.GetDB(ctx, r.db)

	countQuery := `
		SELECT COUNT(*)
		FROM cleaning_executions
		WHERE merchant_id = ?
		  AND enabled = 1
	`
	countArgs := []interface{}{merchantID}
	if strings.TrimSpace(taskID) != "" {
		countQuery += ` AND task_id = ?`
		countArgs = append(countArgs, taskID)
	}

	var totalItems int
	if err := db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalItems); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT ce.id, ce.task_id, ce.merchant_id, ce.comment, ce.photo_url, ce.status, ce.created_by, COALESCE(u.name, ce.created_by), ce.created_at, ce.updated_at
		FROM cleaning_executions ce
		LEFT JOIN users u ON u.user_id = ce.created_by
		WHERE ce.merchant_id = ?
		  AND ce.enabled = 1
	`
	args := []interface{}{merchantID}

	if strings.TrimSpace(taskID) != "" {
		query += ` AND task_id = ?`
		args = append(args, taskID)
	}
	offset := (page - 1) * pageSize

	query += `
		ORDER BY ce.created_at DESC, ce.id DESC
		LIMIT ? OFFSET ?
	`
	args = append(args, pageSize, offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]CleaningExecution, 0, pageSize)
	for rows.Next() {
		var e CleaningExecution
		var performedByName sql.NullString
		if err := rows.Scan(
			&e.ID,
			&e.TaskID,
			&e.MerchantID,
			&e.Comment,
			&e.PhotoURL,
			&e.Status,
			&e.CreatedBy,
			&performedByName,
			&e.CreatedAt,
			&e.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		e.PerformedBy = ActivityPerformedBy{ID: e.CreatedBy}
		if performedByName.Valid {
			e.PerformedBy.Name = performedByName.String
		} else {
			e.PerformedBy.Name = e.CreatedBy
		}
		out = append(out, e)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return out, totalItems, nil
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
	return &session, nil
}

func (r *Repository) GetCleaningExecutionDetail(ctx context.Context, merchantID, executionID string) (*CleaningExecutionDetail, error) {
	db := dbutils.GetDB(ctx, r.db)

	var detail CleaningExecutionDetail
	err := db.QueryRowContext(ctx, `
		SELECT ce.id, ce.task_id, ce.merchant_id, ce.comment, ce.photo_url, ce.status, ce.created_at, ce.created_by, COALESCE(u.name, ce.created_by), ct.id, ct.zone, ct.name
		FROM cleaning_executions ce
		JOIN cleaning_tasks ct ON ct.id = ce.task_id
		LEFT JOIN users u ON u.user_id = ce.created_by
		WHERE ce.merchant_id = ?
		  AND ce.id = ?
		  AND ce.enabled = 1
		LIMIT 1
	`, merchantID, executionID).Scan(
		&detail.ID,
		&detail.TaskID,
		&detail.MerchantID,
		&detail.Comment,
		&detail.PhotoURL,
		&detail.Status,
		&detail.PerformedAt,
		&detail.PerformedBy.ID,
		&detail.PerformedBy.Name,
		&detail.Task.ID,
		&detail.Task.Zone,
		&detail.Task.Name,
	)
	if err != nil {
		return nil, err
	}

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
			NULL AS task_id,
			NULL AS task_name,
			NULL AS task_zone
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
			ce.id AS id,
			'cleanings' AS activity_type,
			ce.status AS status,
			ce.created_by AS performed_by_id,
			COALESCE(u.name, ce.created_by) AS performed_by_name,
			ce.created_at AS performed_at,
			'Nettoyage effectue' AS title,
			CONCAT(ct.zone, ' - ', ct.name) AS subtitle,
			NULL AS readings_count,
			ct.id AS task_id,
			ct.name AS task_name,
			ct.zone AS task_zone
		FROM cleaning_executions ce
		JOIN cleaning_tasks ct ON ct.id = ce.task_id
		LEFT JOIN users u ON u.user_id = ce.created_by
		WHERE ce.merchant_id = ?
		  AND ce.enabled = 1
		  AND ce.created_at >= ?
		  AND ce.created_at < ?
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
		var taskID sql.NullString
		var taskName sql.NullString
		var taskZone sql.NullString

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
			&taskID,
			&taskName,
			&taskZone,
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
			if taskID.Valid {
				metadata["task_id"] = taskID.String
			}
			if taskName.Valid {
				metadata["task_name"] = taskName.String
			}
			if taskZone.Valid {
				metadata["zone"] = taskZone.String
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

func (r *Repository) GetCleaningTaskByID(ctx context.Context, merchantID, taskID string) (*CleaningTask, error) {
	db := dbutils.GetDB(ctx, r.db)

	var t CleaningTask
	err := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, zone, name, frequency_unit, frequency_count, active, enabled, created_at, updated_at, deleted_at
		FROM cleaning_tasks
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`, merchantID, taskID).Scan(
		&t.ID,
		&t.MerchantID,
		&t.Zone,
		&t.Name,
		&t.FrequencyUnit,
		&t.FrequencyCount,
		&t.Active,
		&t.Enabled,
		&t.CreatedAt,
		&t.UpdatedAt,
		&t.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	return &t, nil
}

func (r *Repository) CreateCleaningExecution(ctx context.Context, merchantID, createdBy string, req CreateCleaningExecutionRequest) (*CleaningExecution, error) {
	db := dbutils.GetDB(ctx, r.db)
	id := helpers.GeneratePrefixedID(helpers.HACCPCleaningExecutionIDPrefix)
	now := time.Now().UTC()

	_, err := db.ExecContext(ctx, `
		INSERT INTO cleaning_executions (
			id, merchant_id, task_id, comment, photo_url, status, created_by, created_at, updated_at, enabled
		) VALUES (?, ?, ?, ?, ?, 'done', ?, ?, ?, 1)
	`, id, merchantID, req.TaskID, req.Comment, req.PhotoURL, createdBy, now, now)
	if err != nil {
		return nil, err
	}

	status := "done"
	return &CleaningExecution{
		ID:         id,
		TaskID:     req.TaskID,
		MerchantID: merchantID,
		Comment:    req.Comment,
		PhotoURL:   req.PhotoURL,
		Status:     status,
		CreatedBy:  createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
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
