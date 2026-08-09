package pos

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/openinghours"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	"welloresto-api/internal/utils/dbutils"
)

// posTimeFmt formate une colonne time en texte HH:MM:SS selon le dialecte
// (TIME_FORMAT n'existe pas en Postgres).
func posTimeFmt(col string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "to_char(" + col + ", 'HH24:MI:SS')"
	}
	return "TIME_FORMAT(" + col + ", '%H:%i:%s')"
}

// posDateTimeFmt formate une colonne timestamp en texte YYYY-MM-DD HH:MM:SS
// selon le dialecte (DATE_FORMAT n'existe pas en Postgres).
func posDateTimeFmt(col string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "to_char(" + col + ", 'YYYY-MM-DD HH24:MI:SS')"
	}
	return "DATE_FORMAT(" + col + ", '%Y-%m-%d %H:%i:%s')"
}

// posCastChar caste une expression en texte selon le dialecte — même pattern
// que orders.castChar (CAST AS CHAR sans longueur = char(1) en Postgres).
func posCastChar(expr string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(" + expr + " AS TEXT)"
	}
	return "CAST(" + expr + " AS CHAR)"
}

// posStatusFlag reproduit côté Go la coercition MySQL non-strict d'une chaîne
// client vers un tinyint(1) : '1'/'0' (et tout numérique) sont interprétés,
// toute chaîne non numérique vaut 0 — les colonnes cibles sont boolean en
// Postgres, qui rejetterait une chaîne arbitraire liée directement.
func posStatusFlag(status string) bool {
	f, err := strconv.ParseFloat(strings.TrimSpace(status), 64)
	if err != nil {
		return false
	}
	return f != 0
}

type POSRepository struct {
	database *sql.DB
}

func NewPOSRepository(db *sql.DB) *POSRepository {
	return &POSRepository{database: db}
}

// --------------------
// UPDATE is_open
// --------------------
// UpdatePOSStatus bascule l'ouverture du point de vente pour l'établissement
// de la session. Le marchand vient du contexte d'authentification (donc de
// users_rights) et non plus de users.merchant_id : cette colonne héritée est
// nullable et ne porte qu'un seul établissement, alors qu'un compte peut être
// rattaché à plusieurs marchands — la jointure échouait donc silencieusement
// pour ces comptes.
func (r *POSRepository) UpdatePOSStatus(ctx context.Context, merchantID string, status bool) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// is_open est boolean côté Postgres, tinyint(1) côté MySQL : lier le bool
	// Go directement fonctionne dans les deux dialectes (le driver MySQL
	// encode true/false en 1/0). L'UPDATE mono-table est portable tel quel,
	// plus besoin de variante par dialecte.
	_, err := db.ExecContext(ctx, `
		UPDATE merchant_parameters
		SET is_open = ?
		WHERE merchant_id = ?`, status, merchantID)

	if err != nil {
		log.Error(fmt.Sprintf("Error updating POS status for merchant %s: %v", merchantID, err))
		return err
	}

	return nil
}

func (r *POSRepository) GetDeletionReasons(ctx context.Context, object string) ([]models.DeletionReason, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// l.label_value (varchar) vs dr.deletion_reason_id (integer) : cast texte
	query := `
		SELECT dr.deletion_reason_id,
		       dr.deletion_reason_type,
		       dr.deletion_reason_object,
		       dr.deletion_reason_desc,
		       dr.requires_comment,
		       l.label
		FROM deletion_reasons dr
		INNER JOIN labels l
		       ON l.label_value = ` + posCastChar("dr.deletion_reason_id") + `
		      AND l.lang = 'FR'
		      AND l.label_type = 'deletion_reason'
		WHERE dr.enabled = TRUE
		  AND dr.deletion_reason_object = ?
	`

	rows, err := db.QueryContext(ctx, query, object)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching deletion reasons for object %s: %v", object, err))
		return nil, err
	}
	defer rows.Close()

	var list []models.DeletionReason

	for rows.Next() {
		var d models.DeletionReason
		err := rows.Scan(
			&d.DeletionReasonID,
			&d.DeletionReasonType,
			&d.DeletionReasonObject,
			&d.DeletionReasonDesc,
			&d.RequiresComment,
			&d.Label,
		)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
	}

	return list, nil
}

func (r *POSRepository) GetPOSStatus(ctx context.Context, merchantID string) (*models.POSStatus, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var timezone string

	err := db.QueryRowContext(ctx,
		`SELECT timezone FROM merchant WHERE id = ?`,
		merchantID,
	).Scan(&timezone)

	if err != nil {
		log.Error(fmt.Sprintf("Error fetching merchant timezone for ID %s: %v", merchantID, err))
		return nil, err
	}

	// Convert timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		log.Error(fmt.Sprintf("Error loading timezone for ID %s: %v", merchantID, err))
		return nil, err
	}

	now := time.Now().In(loc)
	currentTime := now.Format("15:04:05")
	currentDay := int(now.Weekday())
	if currentDay == 0 {
		currentDay = 7 // Sunday=7 (1-7 standard)
	}
	holidayDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	holiday, err := settingspkg.NewRepository(r.database).ResolvePlanningHoliday(ctx, merchantID, holidayDate)
	if err != nil {
		log.Error(fmt.Sprintf("Error resolving holiday override for ID %s: %v", merchantID, err))
		return nil, err
	}
	forcedClosed := holiday.IsOpen != nil && !*holiday.IsOpen

	// Statut horaires : ex-procédure GET_POS_STATUS, calculée en Go
	slots, err := openinghours.FetchActiveSlots(ctx, r.database, merchantID, now)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching hours of operation for ID %s: %v", merchantID, err))
		return nil, err
	}
	hoursStatus := openinghours.ComputePOSStatus(now, slots)

	// OPEN/CLOSED based on hours
	var status string
	err = db.QueryRowContext(ctx, `
		SELECT 
		CASE WHEN ? BETWEEN hour_from AND hour_to THEN 'OPEN' ELSE 'CLOSED' END AS s
		FROM hours_of_operation
		WHERE merchant_id = ?
		  AND enabled = TRUE
		  AND day_of_week_from <= ?
		  AND day_of_week_to >= ?
		LIMIT 1`,
		currentTime, merchantID, currentDay, currentDay,
	).Scan(&status)

	if err != nil {
		status = "CLOSED" // default
	}
	if forcedClosed {
		status = "CLOSED"
	}

	// Full POS Status Query
	var result models.POSStatus

	// Le statut calculé n'est plus renvoyé via un placeholder en colonne de
	// SELECT (Postgres ne peut pas typer un paramètre nu en sortie) — il est
	// affecté côté Go après le Scan, résultat identique. is_open (boolean en
	// cible) est scanné dans un int via CASE 1/0, valide dans les deux dialectes.
	err = db.QueryRowContext(ctx, `
		SELECT
			CASE WHEN mp.is_open THEN 1 ELSE 0 END,
			iue.estimated_preparation_time,
			iue.delay_until,
			iue.delay_duration,
			iue.closed_until
		FROM merchant m
		INNER JOIN merchant_parameters mp ON mp.merchant_id = `+posCastChar("m.id")+`
		LEFT JOIN integration_uber_eats iue ON iue.enabled = TRUE AND iue.merchant_id = `+posCastChar("m.id")+`
		WHERE m.id = ?`,
		merchantID,
	).Scan(
		&result.Wello.IsOpen,
		&result.Uber.EstimatedPrepTime,
		&result.Uber.DelayUntil,
		&result.Uber.DelayDuration,
		&result.Uber.ClosedUntil,
	)
	result.Wello.Status = status

	if err != nil {
		log.Error(fmt.Sprintf("Error fetching POS status for ID %s: %v", merchantID, err))
		return nil, err
	}

	// Next schedules
	result.Wello.NextStart = openinghours.FormatDateTime(hoursStatus.NextStart)
	result.Wello.NextEnd = openinghours.FormatDateTime(hoursStatus.NextEnd)
	effectiveOpen := result.Wello.IsOpen == 1 && status == "OPEN" && hoursStatus.IsOpen && !forcedClosed
	if effectiveOpen {
		result.Wello.IsOpen = 1
		result.Wello.Status = "OPEN"
	} else {
		result.Wello.IsOpen = 0
		result.Wello.Status = "CLOSED"
		if forcedClosed {
			result.Wello.NextStart = ""
			result.Wello.NextEnd = ""
		}
	}

	return &result, nil
}

func (r *POSRepository) ToggleProductionPaidOnly(ctx context.Context, merchantID string, status string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	res, err := db.ExecContext(ctx,
		`UPDATE merchant_parameters 
		 SET kitchen_show_only_paid = ?
		 WHERE merchant_id = ?`,
		posStatusFlag(status), merchantID,
	)
	if err != nil {
		log.Error(fmt.Sprintf("Error toggling production paid only for ID %s: %v", merchantID, err))
		return 0, err
	}

	return res.RowsAffected()
}

func (r *POSRepository) GetTVARates(ctx context.Context, merchantID string) ([]ConsumptionType, error) {
	db := dbx.GetDB(ctx, r.database)

	// La requête JOIN récupère le nom traduit (label) et les taux activés.
	// CAST(x AS CHAR) sans longueur vaut char(1) en Postgres (troncature) ->
	// cast texte par dialecte.
	query := `
		SELECT
			` + posCastChar("l.id") + ` as type_id,
			t.delivery_type,
			l.label_value,
			l.label as type_name,
			` + posCastChar("t.tva_id") + ` as rate_id,
			t.tva_title,
			t.tva_desc,
			t.tva_rate
		FROM labels l
		INNER JOIN tva_categories t ON l.label_value = t.delivery_type
		WHERE l.label_type = 'order_type'
		  AND l.lang = 'FR'
		  AND t.enabled = TRUE
		ORDER BY l.id ASC, t.tva_rate ASC`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tva rates with labels: %w", err)
	}
	defer rows.Close()

	// Mapping pour regrouper les taux par type de consommation
	// Key: label_value (IN, TAKE_AWAY, etc.)
	groups := make(map[string]*ConsumptionType)
	// Pour conserver l'ordre d'insertion des types
	var result []ConsumptionType
	var order []string

	for rows.Next() {
		var typeID, labelValue, typeName, rateID, rateLabel, rateDesc, deliveryType string
		var rateValue float64

		err := rows.Scan(&typeID, &deliveryType, &labelValue, &typeName, &rateID, &rateLabel, &rateDesc, &rateValue)
		if err != nil {
			return nil, err
		}

		// Si c'est la première fois qu'on rencontre ce type (ex: IN)
		if _, exists := groups[labelValue]; !exists {
			newType := &ConsumptionType{
				ID:           typeID,   // Utilise l'ID de la table labels (ex: "59")
				Name:         typeName, // Utilise le label traduit (ex: "Sur place")
				DeliveryType: deliveryType,
				Rates:        []Rate{},
			}
			groups[labelValue] = newType
			order = append(order, labelValue)
		}

		// Ajout du taux de TVA au groupe
		groups[labelValue].Rates = append(groups[labelValue].Rates, Rate{
			ID:          rateID,
			Value:       rateValue,
			Label:       rateLabel,
			Description: rateDesc,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// On construit la slice finale en respectant l'ordre de la requête SQL
	for _, key := range order {
		result = append(result, *groups[key])
	}

	// Si aucun résultat, on renvoie une slice vide pour éviter le "null" en JSON
	if result == nil {
		result = []ConsumptionType{}
	}

	return result, nil
}

func (r *POSRepository) ToggleSafetyStock(ctx context.Context, merchantID string, status string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	res, err := db.ExecContext(ctx,
		`UPDATE merchant_parameters 
		 SET disable_components_under_safety_stock = ?
		 WHERE merchant_id = ?`,
		posStatusFlag(status), merchantID,
	)
	if err != nil {
		log.Error(fmt.Sprintf("Error toggling safety stock for ID %s: %v", merchantID, err))
		return 0, err
	}

	return res.RowsAffected()
}

func (r *POSRepository) ToggleScanNOrder(ctx context.Context, merchantID string, status string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	res, err := db.ExecContext(ctx,
		`UPDATE scannorder_settings 
		 SET activated = ?
		 WHERE merchant_id = ?`,
		posStatusFlag(status), merchantID,
	)
	if err != nil {
		log.Error(fmt.Sprintf("Error toggling scan and order for ID %s: %v", merchantID, err))
		return 0, err
	}

	return res.RowsAffected()
}

func (r *POSRepository) GetDeliveryMen(ctx context.Context, merchantID string) ([]models.DeliveryMan, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx, `
        SELECT DISTINCT 
            usv.user_id,
            usv.first_name,
            usv.last_name,
            usv.lat,
            usv.lng,
            usv.status
        FROM user_status_view usv
        INNER JOIN users_rights ur ON ur.user_id = usv.user_id
        INNER JOIN merchant m ON `+posCastChar("m.id")+` = ur.merchant_id
        WHERE ur.merchant_id = ?
          AND ur.enabled = TRUE
    `, merchantID)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching delivery men for ID %s: %v", merchantID, err))
		return nil, err
	}
	defer rows.Close()

	var result []models.DeliveryMan

	for rows.Next() {
		var d models.DeliveryMan
		err := rows.Scan(
			&d.UserID,
			&d.FirstName,
			&d.LastName,
			&d.Lat,
			&d.Lng,
			&d.Status,
		)
		if err != nil {
			log.Error(fmt.Sprintf("Error scanning delivery men for ID %s: %v", merchantID, err))
			return nil, err
		}

		result = append(result, d)
	}

	return result, nil
}

func (r *POSRepository) IsTicketUsed(ctx context.Context, code string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var exists bool

	err := db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM restaurant_ticket WHERE barcode = ?
		)`,
		code,
	).Scan(&exists)

	if err != nil {
		log.Error(fmt.Sprintf("Error checking if ticket is used for code %s: %v", code, err))
		return false, err
	}

	return exists, nil
}

func (s *POSRepository) UpdateMerchantSettings(ctx context.Context, merchantID string, req *models.UpdateMerchantSettingsRequest) error {
	log := logger.FromContext(ctx)

	if req.Merchant != nil {
		if err := s.UpdateMerchant(ctx, merchantID, req.Merchant); err != nil {
			log.Error(fmt.Sprintf("Error updating merchant info for ID %s: %v", merchantID, err))
			return err
		}
	}

	if req.Parameters != nil {
		if err := s.UpdateMerchantParameters(ctx, merchantID, req.Parameters); err != nil {
			log.Error(fmt.Sprintf("Error updating merchant parameters for ID %s: %v", merchantID, err))
			return err
		}
	}

	if req.Marketing != nil {
		if err := s.UpdateMerchantMarketing(ctx, merchantID, req.Marketing); err != nil {
			log.Error(fmt.Sprintf("Error updating merchant marketing for ID %s: %v", merchantID, err))
			return err
		}
	}

	if req.Scannorder != nil {
		if err := s.UpdateScannorderSettings(ctx, merchantID, req.Scannorder); err != nil {
			log.Error(fmt.Sprintf("Error updating scannorder settings for ID %s: %v", merchantID, err))
			return err
		}
	}

	if req.HoursOfOperations != nil {
		if err := s.UpsertHoursOfOperations(ctx, merchantID, *req.HoursOfOperations); err != nil {
			log.Error(fmt.Sprintf("Error updating hours_of_operation for ID %s: %v", merchantID, err))
			return err
		}
	}

	return nil
}

func (r *POSRepository) GetHoursOfOperations(ctx context.Context, merchantID string) ([]models.POSHoursOfOperation, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
		SELECT
			hoo.id,
			hoo.day_of_week_from,
			hoo.day_of_week_to,
			` + posTimeFmt("hoo.hour_from") + ` AS hour_from,
			` + posTimeFmt("hoo.hour_to") + ` AS hour_to,
			hoo.booking_capacity,
			CASE WHEN hoo.first_booking_time IS NULL THEN NULL ELSE ` + posTimeFmt("hoo.first_booking_time") + ` END AS first_booking_time,
			CASE WHEN hoo.last_booking_time IS NULL THEN NULL ELSE ` + posTimeFmt("hoo.last_booking_time") + ` END AS last_booking_time,
			CASE WHEN hoo.valid_from IS NULL THEN NULL ELSE ` + posDateTimeFmt("hoo.valid_from") + ` END AS valid_from,
			CASE WHEN hoo.valid_to IS NULL THEN NULL ELSE ` + posDateTimeFmt("hoo.valid_to") + ` END AS valid_to,
			CASE WHEN hoo.enabled THEN 1 ELSE 0 END
		FROM hours_of_operation hoo
		WHERE hoo.merchant_id = ?
		AND hoo.enabled
		ORDER BY hoo.day_of_week_from, hoo.day_of_week_to, hoo.hour_from, hoo.id
	`

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]models.POSHoursOfOperation, 0)
	for rows.Next() {
		var item models.POSHoursOfOperation
		var bookingCapacity sql.NullInt64
		var firstBookingTime sql.NullString
		var lastBookingTime sql.NullString
		var validFrom sql.NullString
		var validTo sql.NullString
		var enabledInt int

		err := rows.Scan(
			&item.ID,
			&item.DayOfWeekFrom,
			&item.DayOfWeekTo,
			&item.HourFrom,
			&item.HourTo,
			&bookingCapacity,
			&firstBookingTime,
			&lastBookingTime,
			&validFrom,
			&validTo,
			&enabledInt,
		)
		if err != nil {
			return nil, err
		}

		if bookingCapacity.Valid {
			v := int(bookingCapacity.Int64)
			item.BookingCapacity = &v
		}
		if firstBookingTime.Valid {
			v := firstBookingTime.String
			item.FirstBookingTime = &v
		}
		if lastBookingTime.Valid {
			v := lastBookingTime.String
			item.LastBookingTime = &v
		}
		if validFrom.Valid {
			v := validFrom.String
			item.ValidFrom = &v
		}
		if validTo.Valid {
			v := validTo.String
			item.ValidTo = &v
		}
		item.Enabled = enabledInt == 1

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *POSRepository) UpsertHoursOfOperations(ctx context.Context, merchantID string, items []models.POSHoursOfOperationPatch) error {
	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)

		if _, err := db.ExecContext(txCtx, `
			UPDATE hours_of_operation
			SET enabled = FALSE
			WHERE merchant_id = ?
		`, merchantID); err != nil {
			return err
		}

		for _, item := range items {
			if item.DayOfWeekFrom < 1 || item.DayOfWeekFrom > 7 || item.DayOfWeekTo < 1 || item.DayOfWeekTo > 7 {
				return fmt.Errorf("invalid day_of_week range: from=%d to=%d", item.DayOfWeekFrom, item.DayOfWeekTo)
			}
			if strings.TrimSpace(item.HourFrom) == "" || strings.TrimSpace(item.HourTo) == "" {
				return fmt.Errorf("hour_from and hour_to are required")
			}

			id := ""
			if item.ID != nil {
				id = strings.TrimSpace(*item.ID)
			}

			enabled := true
			if item.Enabled != nil {
				enabled = *item.Enabled
			}

			var firstBooking interface{}
			if item.FirstBookingTime != nil && strings.TrimSpace(*item.FirstBookingTime) != "" {
				firstBooking = strings.TrimSpace(*item.FirstBookingTime)
			}

			var lastBooking interface{}
			if item.LastBookingTime != nil && strings.TrimSpace(*item.LastBookingTime) != "" {
				lastBooking = strings.TrimSpace(*item.LastBookingTime)
			}

			var validFrom interface{}
			if item.ValidFrom != nil && strings.TrimSpace(*item.ValidFrom) != "" {
				validFrom = strings.TrimSpace(*item.ValidFrom)
			}

			var validTo interface{}
			if item.ValidTo != nil && strings.TrimSpace(*item.ValidTo) != "" {
				validTo = strings.TrimSpace(*item.ValidTo)
			}

			upsertQuery := `
				INSERT INTO hours_of_operation (
					id,
					merchant_id,
					day_of_week_from,
					day_of_week_to,
					hour_from,
					hour_to,
					booking_capacity,
					first_booking_time,
					last_booking_time,
					valid_from,
					valid_to,
					enabled
				)
				VALUES (
					COALESCE(NULLIF(?, ''), UUID()),
					?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
				)
				ON DUPLICATE KEY UPDATE
					merchant_id = VALUES(merchant_id),
					day_of_week_from = VALUES(day_of_week_from),
					day_of_week_to = VALUES(day_of_week_to),
					hour_from = VALUES(hour_from),
					hour_to = VALUES(hour_to),
					booking_capacity = VALUES(booking_capacity),
					first_booking_time = VALUES(first_booking_time),
					last_booking_time = VALUES(last_booking_time),
					valid_from = VALUES(valid_from),
					valid_to = VALUES(valid_to),
					enabled = VALUES(enabled)
			`
			if dbx.ActiveDialect() == dbx.Postgres {
				// UUID() -> gen_random_uuid() (cast texte : la colonne id est
				// varchar, le cast uuid->varchar n'est pas implicite en PG)
				upsertQuery = `
				INSERT INTO hours_of_operation (
					id,
					merchant_id,
					day_of_week_from,
					day_of_week_to,
					hour_from,
					hour_to,
					booking_capacity,
					first_booking_time,
					last_booking_time,
					valid_from,
					valid_to,
					enabled
				)
				VALUES (
					COALESCE(NULLIF(?, ''), CAST(gen_random_uuid() AS TEXT)),
					?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
				)
				ON CONFLICT (id) DO UPDATE SET
					merchant_id = EXCLUDED.merchant_id,
					day_of_week_from = EXCLUDED.day_of_week_from,
					day_of_week_to = EXCLUDED.day_of_week_to,
					hour_from = EXCLUDED.hour_from,
					hour_to = EXCLUDED.hour_to,
					booking_capacity = EXCLUDED.booking_capacity,
					first_booking_time = EXCLUDED.first_booking_time,
					last_booking_time = EXCLUDED.last_booking_time,
					valid_from = EXCLUDED.valid_from,
					valid_to = EXCLUDED.valid_to,
					enabled = EXCLUDED.enabled
			`
			}

			if _, err := db.ExecContext(txCtx, upsertQuery,
				id,
				merchantID,
				item.DayOfWeekFrom,
				item.DayOfWeekTo,
				strings.TrimSpace(item.HourFrom),
				strings.TrimSpace(item.HourTo),
				item.BookingCapacity,
				firstBooking,
				lastBooking,
				validFrom,
				validTo,
				enabled,
			); err != nil {
				return err
			}
		}

		return nil
	})
}

func (r *POSRepository) CreateHourOfOperation(ctx context.Context, merchantID string, req *models.POSHoursOfOperationPatch) (*models.POSHoursOfOperation, error) {
	if req == nil {
		return nil, models.ErrInvalidInput
	}
	if err := validateHourOfOperationPayload(req); err != nil {
		return nil, err
	}

	db := dbx.GetDB(ctx, r.database)

	hourID := helpers.GeneratePrefixedID("hoo")
	if req.ID != nil && strings.TrimSpace(*req.ID) != "" {
		hourID = strings.TrimSpace(*req.ID)
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	var firstBooking interface{}
	if req.FirstBookingTime != nil && strings.TrimSpace(*req.FirstBookingTime) != "" {
		firstBooking = strings.TrimSpace(*req.FirstBookingTime)
	}

	var lastBooking interface{}
	if req.LastBookingTime != nil && strings.TrimSpace(*req.LastBookingTime) != "" {
		lastBooking = strings.TrimSpace(*req.LastBookingTime)
	}

	var validFrom interface{}
	if req.ValidFrom != nil && strings.TrimSpace(*req.ValidFrom) != "" {
		validFrom = strings.TrimSpace(*req.ValidFrom)
	}

	var validTo interface{}
	if req.ValidTo != nil && strings.TrimSpace(*req.ValidTo) != "" {
		validTo = strings.TrimSpace(*req.ValidTo)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO hours_of_operation (
			id,
			merchant_id,
			day_of_week_from,
			day_of_week_to,
			hour_from,
			hour_to,
			booking_capacity,
			first_booking_time,
			last_booking_time,
			valid_from,
			valid_to,
			enabled
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		hourID,
		merchantID,
		req.DayOfWeekFrom,
		req.DayOfWeekTo,
		strings.TrimSpace(req.HourFrom),
		strings.TrimSpace(req.HourTo),
		req.BookingCapacity,
		firstBooking,
		lastBooking,
		validFrom,
		validTo,
		enabled,
	); err != nil {
		return nil, err
	}

	return r.GetHourOfOperationByID(ctx, merchantID, hourID)
}

func (r *POSRepository) UpdateHourOfOperation(ctx context.Context, merchantID string, hourID string, req *models.POSHoursOfOperationPatch) (*models.POSHoursOfOperation, error) {
	if req == nil {
		return nil, models.ErrInvalidInput
	}
	if err := validateHourOfOperationPayload(req); err != nil {
		return nil, err
	}

	db := dbx.GetDB(ctx, r.database)

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	var firstBooking interface{}
	if req.FirstBookingTime != nil && strings.TrimSpace(*req.FirstBookingTime) != "" {
		firstBooking = strings.TrimSpace(*req.FirstBookingTime)
	}

	var lastBooking interface{}
	if req.LastBookingTime != nil && strings.TrimSpace(*req.LastBookingTime) != "" {
		lastBooking = strings.TrimSpace(*req.LastBookingTime)
	}

	var validFrom interface{}
	if req.ValidFrom != nil && strings.TrimSpace(*req.ValidFrom) != "" {
		validFrom = strings.TrimSpace(*req.ValidFrom)
	}

	var validTo interface{}
	if req.ValidTo != nil && strings.TrimSpace(*req.ValidTo) != "" {
		validTo = strings.TrimSpace(*req.ValidTo)
	}

	res, err := db.ExecContext(ctx, `
		UPDATE hours_of_operation
		SET day_of_week_from = ?,
			day_of_week_to = ?,
			hour_from = ?,
			hour_to = ?,
			booking_capacity = ?,
			first_booking_time = ?,
			last_booking_time = ?,
			valid_from = ?,
			valid_to = ?,
			enabled = ?
		WHERE id = ? AND merchant_id = ?
	`,
		req.DayOfWeekFrom,
		req.DayOfWeekTo,
		strings.TrimSpace(req.HourFrom),
		strings.TrimSpace(req.HourTo),
		req.BookingCapacity,
		firstBooking,
		lastBooking,
		validFrom,
		validTo,
		enabled,
		hourID,
		merchantID,
	)
	if err != nil {
		return nil, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, models.ErrNotFound
	}

	return r.GetHourOfOperationByID(ctx, merchantID, hourID)
}

func (r *POSRepository) DeleteHourOfOperation(ctx context.Context, merchantID string, hourID string) error {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx, `
		UPDATE hours_of_operation
		SET enabled = FALSE
		WHERE id = ? AND merchant_id = ?
	`, hourID, merchantID)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return models.ErrNotFound
	}

	return nil
}

func (r *POSRepository) GetHourOfOperationByID(ctx context.Context, merchantID string, hourID string) (*models.POSHoursOfOperation, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
		SELECT
			hoo.id,
			hoo.day_of_week_from,
			hoo.day_of_week_to,
			` + posTimeFmt("hoo.hour_from") + ` AS hour_from,
			` + posTimeFmt("hoo.hour_to") + ` AS hour_to,
			hoo.booking_capacity,
			CASE WHEN hoo.first_booking_time IS NULL THEN NULL ELSE ` + posTimeFmt("hoo.first_booking_time") + ` END AS first_booking_time,
			CASE WHEN hoo.last_booking_time IS NULL THEN NULL ELSE ` + posTimeFmt("hoo.last_booking_time") + ` END AS last_booking_time,
			CASE WHEN hoo.valid_from IS NULL THEN NULL ELSE ` + posDateTimeFmt("hoo.valid_from") + ` END AS valid_from,
			CASE WHEN hoo.valid_to IS NULL THEN NULL ELSE ` + posDateTimeFmt("hoo.valid_to") + ` END AS valid_to,
			CASE WHEN hoo.enabled THEN 1 ELSE 0 END
		FROM hours_of_operation hoo
		WHERE hoo.id = ? AND hoo.merchant_id = ?
	`

	var item models.POSHoursOfOperation
	var bookingCapacity sql.NullInt64
	var firstBookingTime sql.NullString
	var lastBookingTime sql.NullString
	var validFrom sql.NullString
	var validTo sql.NullString
	var enabledInt int

	err := db.QueryRowContext(ctx, query, hourID, merchantID).Scan(
		&item.ID,
		&item.DayOfWeekFrom,
		&item.DayOfWeekTo,
		&item.HourFrom,
		&item.HourTo,
		&bookingCapacity,
		&firstBookingTime,
		&lastBookingTime,
		&validFrom,
		&validTo,
		&enabledInt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		}
		return nil, err
	}

	if bookingCapacity.Valid {
		v := int(bookingCapacity.Int64)
		item.BookingCapacity = &v
	}
	if firstBookingTime.Valid {
		v := firstBookingTime.String
		item.FirstBookingTime = &v
	}
	if lastBookingTime.Valid {
		v := lastBookingTime.String
		item.LastBookingTime = &v
	}
	if validFrom.Valid {
		v := validFrom.String
		item.ValidFrom = &v
	}
	if validTo.Valid {
		v := validTo.String
		item.ValidTo = &v
	}
	item.Enabled = enabledInt == 1

	return &item, nil
}

func validateHourOfOperationPayload(req *models.POSHoursOfOperationPatch) error {
	if req.DayOfWeekFrom < 1 || req.DayOfWeekFrom > 7 || req.DayOfWeekTo < 1 || req.DayOfWeekTo > 7 {
		return models.ErrInvalidInput
	}
	if strings.TrimSpace(req.HourFrom) == "" || strings.TrimSpace(req.HourTo) == "" {
		return models.ErrInvalidInput
	}
	return nil
}

func (r *POSRepository) UpdateMerchant(ctx context.Context, merchantID string, req *models.MerchantSettings) error {
	db := dbx.GetDB(ctx, r.database)

	updates := []string{}
	args := []interface{}{}

	if req.BusinessName != nil {
		updates = append(updates, "fullName = ?")
		args = append(args, *req.BusinessName)
	}
	if req.Address != nil {
		updates = append(updates, "address = ?")
		args = append(args, *req.Address)
	}
	if req.StreetNumber != nil {
		updates = append(updates, "street_number = ?")
		args = append(args, *req.StreetNumber)
	}
	if req.Street != nil {
		updates = append(updates, "street = ?")
		args = append(args, *req.Street)
	}
	if req.ZipCode != nil {
		updates = append(updates, "zip_code = ?")
		args = append(args, *req.ZipCode)
	}
	if req.City != nil {
		updates = append(updates, "city = ?")
		args = append(args, *req.City)
	}
	if req.Country != nil {
		updates = append(updates, "country = ?")
		args = append(args, *req.Country)
	}
	if req.Lat != nil {
		updates = append(updates, "lat = ?")
		args = append(args, *req.Lat)
	}
	if req.Lng != nil {
		updates = append(updates, "lng = ?")
		args = append(args, *req.Lng)
	}
	if req.Timezone != nil {
		updates = append(updates, "timezone = ?")
		args = append(args, *req.Timezone)
	}
	if req.Logo != nil {
		updates = append(updates, "logo = ?")
		args = append(args, *req.Logo)
	}
	if req.HandicapAccess != nil {
		updates = append(updates, "handicap_access = ?")
		args = append(args, *req.HandicapAccess)
	}
	if req.SIRET != nil {
		updates = append(updates, "SIRET = ?")
		args = append(args, *req.SIRET)
	}
	if req.WebSite != nil {
		updates = append(updates, "web_site = ?")
		args = append(args, *req.WebSite)
	}
	if req.MerchantTel != nil {
		updates = append(updates, "merchantTel = ?")
		args = append(args, *req.MerchantTel)
	}
	if req.IsActive != nil {
		updates = append(updates, "is_active = ?")
		args = append(args, *req.IsActive)
	}

	if len(updates) == 0 {
		return nil
	}

	args = append(args, merchantID)

	query := fmt.Sprintf(`
		UPDATE merchant
		SET %s
		WHERE id = ?
	`, strings.Join(updates, ", "))

	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *POSRepository) UpdateScannorderSettings(ctx context.Context, merchantID string, req *models.ScannorderSettings) error {
	db := dbx.GetDB(ctx, r.database)

	updates := []string{}
	args := []interface{}{}

	if req.Activated != nil {
		updates = append(updates, "activated = ?")
		args = append(args, *req.Activated)
	}
	if req.ShowAddress != nil {
		updates = append(updates, "show_address = ?")
		args = append(args, *req.ShowAddress)
	}
	if req.HeaderBackground != nil {
		updates = append(updates, "header_background = ?")
		args = append(args, *req.HeaderBackground)
	}
	if req.HeaderBackgroundURL != nil {
		updates = append(updates, "header_background_url = ?")
		args = append(args, *req.HeaderBackgroundURL)
	}
	if req.HomePage != nil {
		updates = append(updates, "home_page = ?")
		args = append(args, *req.HomePage)
	}
	if req.HomePageTitle != nil {
		updates = append(updates, "home_page_title = ?")
		args = append(args, *req.HomePageTitle)
	}
	if req.HomePageDesc != nil {
		updates = append(updates, "home_page_desc = ?")
		args = append(args, *req.HomePageDesc)
	}
	if req.InfoPopupEnabled != nil {
		updates = append(updates, "info_popup_enabled = ?")
		args = append(args, *req.InfoPopupEnabled)
	}
	if req.ProductBgColor != nil {
		updates = append(updates, "product_bg_color = ?")
		args = append(args, *req.ProductBgColor)
	}
	if req.BtnColor != nil {
		updates = append(updates, "btn_color = ?")
		args = append(args, *req.BtnColor)
	}
	if req.BtnTextColor != nil {
		updates = append(updates, "btn_text_color = ?")
		args = append(args, *req.BtnTextColor)
	}
	if req.DeliveryType != nil {
		updates = append(updates, "delivery_type = ?")
		args = append(args, *req.DeliveryType)
	}
	if req.EnablePayments != nil {
		updates = append(updates, "enable_payments = ?")
		args = append(args, *req.EnablePayments)
	}

	if len(updates) == 0 {
		return nil
	}

	args = append(args, merchantID)

	query := fmt.Sprintf(`
		UPDATE scannorder_settings
		SET %s
		WHERE merchant_id = ?
	`, strings.Join(updates, ", "))

	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *POSRepository) UpdateMerchantMarketing(ctx context.Context, merchantID string, req *models.MerchantMarketingSettings) error {
	db := dbx.GetDB(ctx, r.database)

	updates := []string{}
	args := []interface{}{}

	if req.SMSEnabled != nil {
		updates = append(updates, "sms_enabled = ?")
		args = append(args, *req.SMSEnabled)
	}
	if req.EmailEnabled != nil {
		updates = append(updates, "email_enabled = ?")
		args = append(args, *req.EmailEnabled)
	}
	if req.SMSSenderName != nil {
		updates = append(updates, "sms_sender_name = ?")
		args = append(args, *req.SMSSenderName)
	}
	if req.EmailSenderName != nil {
		updates = append(updates, "email_sender_name = ?")
		args = append(args, *req.EmailSenderName)
	}
	if req.SMSTemplate != nil {
		updates = append(updates, "sms_template = ?")
		args = append(args, *req.SMSTemplate)
	}
	if req.EmailTemplate != nil {
		updates = append(updates, "email_template = ?")
		args = append(args, *req.EmailTemplate)
	}
	if req.TrackingTemplate != nil {
		updates = append(updates, "tracking_template = ?")
		args = append(args, *req.TrackingTemplate)
	}

	if len(updates) == 0 {
		return nil
	}

	args = append(args, merchantID)

	query := fmt.Sprintf(`
		UPDATE merchant_marketing_settings
		SET %s
		WHERE merchant_id = ?
	`, strings.Join(updates, ", "))

	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *POSRepository) UpdateMerchantParameters(ctx context.Context, merchantID string, req *models.MerchantParametersSettings) error {
	db := dbx.GetDB(ctx, r.database)

	updates := []string{}
	args := []interface{}{}

	if req.ManageOnSite != nil {
		updates = append(updates, "manage_on_site = ?")
		args = append(args, *req.ManageOnSite)
	}
	if req.ManageTakeAway != nil {
		updates = append(updates, "manage_take_away = ?")
		args = append(args, *req.ManageTakeAway)
	}
	if req.ManageDelivery != nil {
		updates = append(updates, "manage_delivery = ?")
		args = append(args, *req.ManageDelivery)
	}
	if req.ConcurrentPreparationCapacity != nil {
		updates = append(updates, "concurrent_preparation_capacity = ?")
		args = append(args, *req.ConcurrentPreparationCapacity)
	}
	if req.DeliveryFees != nil {
		updates = append(updates, "delivery_fees = ?")
		args = append(args, *req.DeliveryFees)
	}
	if req.DeliveryFeesLimit != nil {
		updates = append(updates, "delivery_fees_limit = ?")
		args = append(args, *req.DeliveryFeesLimit)
	}
	if req.DeliveryDistanceLimit != nil {
		updates = append(updates, "delivery_distance_limit = ?")
		args = append(args, *req.DeliveryDistanceLimit)
	}
	if req.MinimumCartForDeliveryOrder != nil {
		updates = append(updates, "minimum_cart_for_delivery_order = ?")
		args = append(args, *req.MinimumCartForDeliveryOrder)
	}
	if req.KitchenShowOnlyPaid != nil {
		updates = append(updates, "kitchen_show_only_paid = ?")
		args = append(args, *req.KitchenShowOnlyPaid)
	}
	if req.KitchenShowPendingApproval != nil {
		updates = append(updates, "kitchen_show_pending_approval = ?")
		args = append(args, *req.KitchenShowPendingApproval)
	}
	if req.KitchenDistributionMode != nil {
		updates = append(updates, "kitchen_distribution_mode = ?")
		args = append(args, *req.KitchenDistributionMode)
	}
	if req.ProductionDisplayMode != nil {
		updates = append(updates, "production_display_mode = ?")
		args = append(args, *req.ProductionDisplayMode)
	}
	if req.MinimumPreparationTime != nil {
		updates = append(updates, "minimum_preparation_time = ?")
		args = append(args, *req.MinimumPreparationTime)
	}
	if req.MaximumPreparationTime != nil {
		updates = append(updates, "maximum_preparation_time = ?")
		args = append(args, *req.MaximumPreparationTime)
	}
	if req.DisableComponentsUnderSafetyStock != nil {
		updates = append(updates, "disable_components_under_safety_stock = ?")
		args = append(args, *req.DisableComponentsUnderSafetyStock)
	}
	if req.ServiceRequiredForOrdering != nil {
		updates = append(updates, "service_required_for_ordering = ?")
		args = append(args, *req.ServiceRequiredForOrdering)
	}
	if req.CashRegisterRequiredForOrdering != nil {
		updates = append(updates, "cash_register_required_for_ordering = ?")
		args = append(args, *req.CashRegisterRequiredForOrdering)
	}
	if req.WaiterAppCanCashIn != nil {
		updates = append(updates, "waiter_app_can_cash_in = ?")
		args = append(args, *req.WaiterAppCanCashIn)
	}
	if req.WaiterAppCanClockIn != nil {
		updates = append(updates, "waiter_app_can_clock_in = ?")
		args = append(args, *req.WaiterAppCanClockIn)
	}
	if req.AutoCompleteOrders != nil {
		updates = append(updates, "auto_complete_orders = ?")
		args = append(args, *req.AutoCompleteOrders)
	}
	if req.AutoCompleteOrdersDelay != nil {
		updates = append(updates, "auto_complete_orders_delay = ?")
		args = append(args, *req.AutoCompleteOrdersDelay)
	}
	if req.AutoAcceptSnoDeliveryOrders != nil {
		updates = append(updates, "auto_accept_sno_delivery_orders = ?")
		args = append(args, *req.AutoAcceptSnoDeliveryOrders)
	}
	if req.AutoAcceptSnoTakeAwayOrders != nil {
		updates = append(updates, "auto_accept_sno_take_away_orders = ?")
		args = append(args, *req.AutoAcceptSnoTakeAwayOrders)
	}
	if req.AutomaticallyAddCustomerRewards != nil {
		updates = append(updates, "automatically_add_customer_rewards = ?")
		args = append(args, *req.AutomaticallyAddCustomerRewards)
	}
	if req.WarningNewOrderNotPaid != nil {
		updates = append(updates, "warning_new_order_not_paid = ?")
		args = append(args, *req.WarningNewOrderNotPaid)
	}
	if req.EnableAdvanceOrders != nil {
		updates = append(updates, "enable_advance_orders = ?")
		args = append(args, *req.EnableAdvanceOrders)
	}
	if req.AdvanceOrderDays != nil {
		updates = append(updates, "advance_order_days = ?")
		args = append(args, *req.AdvanceOrderDays)
	}
	if req.PagerNumberRequired != nil {
		updates = append(updates, "pager_number_required = ?")
		args = append(args, *req.PagerNumberRequired)
	}
	if req.POSAutoLockEnabled != nil {
		updates = append(updates, "pos_auto_lock_enabled = ?")
		args = append(args, *req.POSAutoLockEnabled)
	}
	if req.POSAutoLockDelayMinutes != nil {
		updates = append(updates, "pos_auto_lock_delay_minutes = ?")
		args = append(args, *req.POSAutoLockDelayMinutes)
	}
	if req.POSUpsellEnabled != nil {
		updates = append(updates, "pos_upsell_enabled = ?")
		args = append(args, *req.POSUpsellEnabled)
	}
	if req.CustomerFormRequirements != nil {
		updates = append(updates, "customer_form_requirements = ?")
		args = append(args, []byte(*req.CustomerFormRequirements))
	}
	if req.Currency != nil {
		updates = append(updates, "currency = ?")
		args = append(args, *req.Currency)
	}
	if req.IsOpen != nil {
		updates = append(updates, "is_open = ?")
		args = append(args, *req.IsOpen)
	}

	if len(updates) == 0 {
		return nil
	}

	args = append(args, merchantID)

	query := fmt.Sprintf(`
		UPDATE merchant_parameters
		SET %s
		WHERE merchant_id = ?
	`, strings.Join(updates, ", "))

	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *POSRepository) GetMerchantSettings(ctx context.Context, merchantID string) (
	*models.MerchantSettings,
	*models.MerchantParametersSettings,
	*models.MerchantMarketingSettings,
	*models.ScannorderSettings,
	error,
) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// ───────────────────────────────
	// 1) Merchant
	// ───────────────────────────────
	queryMerchant := `
		SELECT id, fullName, address, street_number, street, zip_code, city,
		       country, lat, lng, timezone, /*logo, */logo_url, handicap_access,
		       SIRET, web_site, email, merchantTel, creation_date,
		       is_active
		FROM merchant
		WHERE id = ?
	`

	var m models.MerchantSettings
	row := db.QueryRowContext(ctx, queryMerchant, merchantID)
	err := row.Scan(
		&m.MerchantID,
		&m.BusinessName,
		&m.Address,
		&m.StreetNumber,
		&m.Street,
		&m.ZipCode,
		&m.City,
		&m.Country,
		&m.Lat,
		&m.Lng,
		&m.Timezone,
		//&m.Logo,
		&m.LogoURL,
		&m.HandicapAccess,
		&m.SIRET,
		&m.WebSite,
		&m.Email,
		&m.MerchantTel,
		&m.CreationDate,
		&m.IsActive,
	)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching merchant settings for ID %s: %v", merchantID, err))
		return nil, nil, nil, nil, err
	}

	// ───────────────────────────────
	// 2) Merchant Parameters
	// ───────────────────────────────
	queryParams := `
		SELECT merchant_id, manage_on_site, manage_take_away, manage_delivery,
		       last_menu_update, concurrent_preparation_capacity, delivery_fees,
		       delivery_fees_limit, delivery_distance_limit, minimum_cart_for_delivery_order,
		       kitchen_show_only_paid, kitchen_show_pending_approval, kitchen_distribution_mode,
		       production_display_mode, minimum_preparation_time, maximum_preparation_time,
		       disable_components_under_safety_stock, service_required_for_ordering,
		       cash_register_required_for_ordering, waiter_app_can_cash_in,
		       waiter_app_can_clock_in, auto_complete_orders, auto_complete_orders_delay,
		       auto_accept_sno_delivery_orders, auto_accept_sno_take_away_orders,
		       automatically_add_customer_rewards, warning_new_order_not_paid,
		       enable_advance_orders, advance_order_days, pager_number_required,
		       pos_auto_lock_enabled, pos_auto_lock_delay_minutes, pos_upsell_enabled,
		       customer_form_requirements,
		       enabled_rating, currency, is_open, primary_color, text_color_on_primary_color,
		       zoning_type, radial_cone_count, radial_zone_ranges, grid_cell_size_km,
		       grid_origin_lat, grid_origin_lng, cardinal_cone_count, cardinal_zone_ranges
		FROM merchant_parameters
		WHERE merchant_id = ?
	`

	var params models.MerchantParametersSettings
	var customerFormRequirementsRaw []byte
	row = db.QueryRowContext(ctx, queryParams, merchantID)
	err = row.Scan(
		&params.MerchantID,
		&params.ManageOnSite,
		&params.ManageTakeAway,
		&params.ManageDelivery,
		&params.LastMenuUpdate,
		&params.ConcurrentPreparationCapacity,
		&params.DeliveryFees,
		&params.DeliveryFeesLimit,
		&params.DeliveryDistanceLimit,
		&params.MinimumCartForDeliveryOrder,
		&params.KitchenShowOnlyPaid,
		&params.KitchenShowPendingApproval,
		&params.KitchenDistributionMode,
		&params.ProductionDisplayMode,
		&params.MinimumPreparationTime,
		&params.MaximumPreparationTime,
		&params.DisableComponentsUnderSafetyStock,
		&params.ServiceRequiredForOrdering,
		&params.CashRegisterRequiredForOrdering,
		&params.WaiterAppCanCashIn,
		&params.WaiterAppCanClockIn,
		&params.AutoCompleteOrders,
		&params.AutoCompleteOrdersDelay,
		&params.AutoAcceptSnoDeliveryOrders,
		&params.AutoAcceptSnoTakeAwayOrders,
		&params.AutomaticallyAddCustomerRewards,
		&params.WarningNewOrderNotPaid,
		&params.EnableAdvanceOrders,
		&params.AdvanceOrderDays,
		&params.PagerNumberRequired,
		&params.POSAutoLockEnabled,
		&params.POSAutoLockDelayMinutes,
		&params.POSUpsellEnabled,
		&customerFormRequirementsRaw,
		&params.EnabledRating,
		&params.Currency,
		&params.IsOpen,
		&params.PrimaryColor,
		&params.TextColorOnPrimaryColor,
		&params.ZoningType,
		&params.RadialConeCount,
		&params.RadialZoneRanges,
		&params.GridCellSizeKm,
		&params.GridOriginLat,
		&params.GridOriginLng,
		&params.CardinalConeCount,
		&params.CardinalZoneRanges,
	)
	if err != nil {
		return &m, nil, nil, nil, err
	}
	if len(customerFormRequirementsRaw) > 0 {
		raw := json.RawMessage(customerFormRequirementsRaw)
		params.CustomerFormRequirements = &raw
	}

	// ───────────────────────────────
	// 3) Merchant Marketing
	// ───────────────────────────────
	queryMarketing := `
		SELECT id, merchant_id, sms_enabled, sms_unit_price, email_enabled,
		       sms_sender_name, email_sender_name, sms_template, email_template,
		       tracking_template, messaggio_login, messaggio_from, created_at,
		       updated_at
		FROM merchant_marketing_settings
		WHERE merchant_id = ?
	`

	var marketing models.MerchantMarketingSettings
	row = db.QueryRowContext(ctx, queryMarketing, merchantID)
	err = row.Scan(
		&marketing.ID,
		&marketing.MerchantID,
		&marketing.SMSEnabled,
		&marketing.SMSUnitPrice,
		&marketing.EmailEnabled,
		&marketing.SMSSenderName,
		&marketing.EmailSenderName,
		&marketing.SMSTemplate,
		&marketing.EmailTemplate,
		&marketing.TrackingTemplate,
		&marketing.MessaggioLogin,
		&marketing.MessaggioFrom,
		&marketing.CreatedAt,
		&marketing.UpdatedAt,
	)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching merchant marketing settings for ID %s: %v", merchantID, err))
		return &m, &params, nil, nil, err
	}

	// ───────────────────────────────
	// 4) Scannorder Settings
	// ───────────────────────────────
	queryScann := `
		SELECT merchant_id, activated, show_address, header_background,
		       header_background_url, home_page, home_page_title,
		       home_page_desc, info_popup_enabled, info_popup_title,
		       info_popup_content, info_popup_button_content,
		       product_bg_color, nav_bg_color, bg_color, btn_color,
		       btn_text_color, product_categ_bg_color,
		       product_categ_text_color, popup_bg_color, popup_text_color,
		       ad_text_color, home_text_color, product_text_color,
		       discount_color, discount_text_color, border_radius,
		       shadow_style, delivery_type, enable_payments,
		       variable_fees, fixed_fees, users_default_name,
		       seo_title, seo_description, seo_keywords, seo_cuisine_type
		FROM scannorder_settings
		WHERE merchant_id = ?
	`

	var scann models.ScannorderSettings
	row = db.QueryRowContext(ctx, queryScann, merchantID)
	err = row.Scan(
		&scann.MerchantID,
		&scann.Activated,
		&scann.ShowAddress,
		&scann.HeaderBackground,
		&scann.HeaderBackgroundURL,
		&scann.HomePage,
		&scann.HomePageTitle,
		&scann.HomePageDesc,
		&scann.InfoPopupEnabled,
		&scann.InfoPopupTitle,
		&scann.InfoPopupContent,
		&scann.InfoPopupButtonContent,
		&scann.ProductBgColor,
		&scann.NavBGColor,
		&scann.BGColor,
		&scann.BtnColor,
		&scann.BtnTextColor,
		&scann.ProductCategBGColor,
		&scann.ProductCategTextColor,
		&scann.PopupBGColor,
		&scann.PopupTextColor,
		&scann.ADTextColor,
		&scann.HomeTextColor,
		&scann.ProductTextColor,
		&scann.DiscountColor,
		&scann.DiscountTextColor,
		&scann.BorderRadius,
		&scann.ShadowStyle,
		&scann.DeliveryType,
		&scann.EnablePayments,
		&scann.VariableFees,
		&scann.FixedFees,
		&scann.UsersDefaultName,
		&scann.SEOTitle,
		&scann.SEODescription,
		&scann.SEOKeywords,
		&scann.SEOCuisineType,
	)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching scannorder settings for ID %s: %v", merchantID, err))
		return &m, &params, &marketing, nil, err
	}

	return &m, &params, &marketing, &scann, nil
}
