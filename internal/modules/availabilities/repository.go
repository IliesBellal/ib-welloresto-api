package availabilities

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/utils/dbutils"
)

type AvailabilitiesRepository struct {
	database *sql.DB
}

func NewAvailabilitiesRepository(db *sql.DB) *AvailabilitiesRepository {
	return &AvailabilitiesRepository{database: db}
}

// GetAvailabilitiesByMerchant récupère toutes les disponibilités pour un commerçant
// avec les produits et créneaux associés
func (r *AvailabilitiesRepository) GetAvailabilitiesByMerchant(ctx context.Context, merchantID string) ([]Availability, error) {
	db := dbutils.GetDB(ctx, r.database)

	// Récupérer les availabilities principales
	query := `
		SELECT 
			availability_id,
			merchant_id,
			availability_name,
			unavailable_message,
			enabled,
			creation_date,
			update_date
		FROM availabilities
		WHERE merchant_id = ? AND enabled = 1
		ORDER BY creation_date DESC
	`

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query availabilities: %w", err)
	}
	defer rows.Close()

	var availabilities []Availability
	var availabilityIDs []string

	for rows.Next() {
		var a Availability
		err := rows.Scan(
			&a.AvailabilityID,
			&a.MerchantID,
			&a.Name,
			&a.UnavailableMessage,
			&a.Enabled,
			&a.CreatedAt,
			&a.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan availability: %w", err)
		}
		availabilities = append(availabilities, a)
		availabilityIDs = append(availabilityIDs, a.AvailabilityID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Si pas de disponibilités, retourner la liste vide
	if len(availabilities) == 0 {
		return availabilities, nil
	}

	// Récupérer tous les produits en une seule requête
	productsByAvailability, err := r.getProductIDsByAvailabilityIDs(db, ctx, availabilityIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get product IDs: %w", err)
	}

	// Récupérer tous les créneaux en une seule requête
	schedulesByAvailability, err := r.getSchedulesByAvailabilityIDs(db, ctx, availabilityIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedules: %w", err)
	}

	// Mapper les résultats
	for i := range availabilities {
		availabilities[i].ProductIDs = productsByAvailability[availabilities[i].AvailabilityID]
		availabilities[i].Schedules = schedulesByAvailability[availabilities[i].AvailabilityID]
	}

	return availabilities, nil
}

// GetAvailabilityByID récupère une disponibilité spécifique avec tous ses détails
func (r *AvailabilitiesRepository) GetAvailabilityByID(ctx context.Context, merchantID, availabilityID string) (*Availability, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		SELECT 
			availability_id,
			merchant_id,
			availability_name,
			unavailable_message,
			enabled,
			creation_date,
			update_date
		FROM availabilities
		WHERE availability_id = ? AND merchant_id = ? AND enabled = 1
	`

	row := db.QueryRowContext(ctx, query, availabilityID, merchantID)

	var a Availability
	err := row.Scan(
		&a.AvailabilityID,
		&a.MerchantID,
		&a.Name,
		&a.UnavailableMessage,
		&a.Enabled,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan availability: %w", err)
	}

	// Récupérer les produits (une seule requête)
	productIDs, err := r.getProductIDsByAvailabilityIDs(db, ctx, []string{a.AvailabilityID})
	if err != nil {
		return nil, fmt.Errorf("failed to get product IDs: %w", err)
	}
	a.ProductIDs = productIDs[a.AvailabilityID]

	// Récupérer les créneaux (une seule requête)
	schedules, err := r.getSchedulesByAvailabilityIDs(db, ctx, []string{a.AvailabilityID})
	if err != nil {
		return nil, fmt.Errorf("failed to get schedules: %w", err)
	}
	a.Schedules = schedules[a.AvailabilityID]

	return &a, nil
}

// Create crée une nouvelle disponibilité avec ses produits et créneaux (atomique)
func (r *AvailabilitiesRepository) Create(ctx context.Context, merchantID string, req CreateAvailabilityRequest) (*Availability, error) {
	// Démarrer une transaction
	db := dbutils.GetDB(ctx, r.database)

	// Générer l'ID
	availabilityID := helpers.GeneratePrefixedID(helpers.AvailabilityIDPrefix)
	now := time.Now().UTC()

	// Insérer la disponibilité
	insertQuery := `
		INSERT INTO availabilities (
			availability_id,
			merchant_id,
			availability_name,
			unavailable_message,
			enabled,
			creation_date,
			update_date
		) VALUES (?, ?, ?, ?, 1, ?, ?)
	`

	_, err := db.ExecContext(ctx, insertQuery,
		availabilityID,
		merchantID,
		req.Name,
		req.UnavailableMessage,
		now,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert availability: %w", err)
	}

	// Insérer les produits
	if len(req.ProductIDs) > 0 {
		productQuery := `
			INSERT INTO availabilities_products (
				availability_product_id,
				availability_id,
				product_id,
				creation_date
			) VALUES (?, ?, ?, ?)
		`

		for _, productID := range req.ProductIDs {
			_, err := db.ExecContext(ctx, productQuery,
				helpers.GeneratePrefixedID(helpers.AvailabilityProductPrefix),
				availabilityID,
				productID,
				now,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to insert product: %w", err)
			}
		}
	}

	// Insérer les créneaux
	if len(req.Schedules) > 0 {
		scheduleQuery := `
			INSERT INTO availabilities_schedules (
				schedule_id,
				availability_id,
				day_of_week,
				available_from,
				available_to,
				creation_date,
				update_date
			) VALUES (?, ?, ?, ?, ?, ?, ?)
		`

		for _, schedule := range req.Schedules {
			schedule.ScheduleID = helpers.GeneratePrefixedID(helpers.AvailabilitySchedulePrefix)
			_, err := db.ExecContext(ctx, scheduleQuery,
				schedule.ScheduleID,
				availabilityID,
				schedule.DayOfWeek,
				schedule.StartTime,
				schedule.EndTime,
				now,
				now,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to insert schedule: %w", err)
			}
		}
	}

	// Commit la transaction
	// if err = tx.Commit(); err != nil {
	// 	return nil, fmt.Errorf("failed to commit transaction: %w", err)
	// }

	// Retourner l'objet créé
	return &Availability{
		AvailabilityID:     availabilityID,
		MerchantID:         merchantID,
		Name:               req.Name,
		UnavailableMessage: req.UnavailableMessage,
		Enabled:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
		ProductIDs:         req.ProductIDs,
		Schedules:          convertSchedules(availabilityID, req.Schedules, now),
	}, nil
}

// Update met à jour une disponibilité existante (atomique)
func (r *AvailabilitiesRepository) Update(ctx context.Context, merchantID, availabilityID string, req UpdateAvailabilityRequest) (*Availability, error) {
	db := dbutils.GetDB(ctx, r.database)

	now := time.Now().UTC()

	// Mettre à jour l'availability
	updateQuery := `
		UPDATE availabilities
		SET availability_name = ?, unavailable_message = ?, update_date = ?
		WHERE availability_id = ? AND merchant_id = ? AND enabled = 1
	`

	result, err := db.ExecContext(ctx, updateQuery,
		req.Name,
		req.UnavailableMessage,
		now,
		availabilityID,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update availability: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("availability not found")
	}

	// Supprimer les produits existants
	deleteProductsQuery := `DELETE FROM availabilities_products WHERE availability_id = ?`
	_, err = db.ExecContext(ctx, deleteProductsQuery, availabilityID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete products: %w", err)
	}

	// Insérer les nouveaux produits
	if len(req.ProductIDs) > 0 {
		productQuery := `
			INSERT INTO availabilities_products (
				availability_product_id,
				availability_id,
				product_id,
				creation_date
			) VALUES (?, ?, ?, ?)
		`

		for _, productID := range req.ProductIDs {
			_, err := db.ExecContext(ctx, productQuery,
				helpers.GeneratePrefixedID(helpers.AvailabilityProductPrefix),
				availabilityID,
				productID,
				now,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to insert product: %w", err)
			}
		}
	}

	// Supprimer les créneaux existants
	deleteSchedulesQuery := `DELETE FROM availabilities_schedules WHERE availability_id = ?`
	_, err = db.ExecContext(ctx, deleteSchedulesQuery, availabilityID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete schedules: %w", err)
	}

	// Insérer les nouveaux créneaux
	if len(req.Schedules) > 0 {
		scheduleQuery := `
			INSERT INTO availabilities_schedules (
				schedule_id,
				availability_id,
				day_of_week,
				available_from,
				available_to,
				creation_date,
				update_date
			) VALUES (?, ?, ?, ?, ?, ?, ?)
		`

		for _, schedule := range req.Schedules {
			schedule.ScheduleID = helpers.GeneratePrefixedID(helpers.AvailabilitySchedulePrefix)
			_, err := db.ExecContext(ctx, scheduleQuery,
				schedule.ScheduleID,
				availabilityID,
				schedule.DayOfWeek,
				schedule.StartTime,
				schedule.EndTime,
				now,
				now,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to insert schedule: %w", err)
			}
		}
	}

	// Commit la transaction
	// if err = tx.Commit(); err != nil {
	// 	return nil, fmt.Errorf("failed to commit transaction: %w", err)
	// }

	// Retourner l'objet mis à jour
	return &Availability{
		AvailabilityID:     availabilityID,
		MerchantID:         merchantID,
		Name:               req.Name,
		UnavailableMessage: req.UnavailableMessage,
		Enabled:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
		ProductIDs:         req.ProductIDs,
		Schedules:          convertSchedules(availabilityID, req.Schedules, now),
	}, nil
}

// Delete effectue une suppression logique (enabled = 0)
func (r *AvailabilitiesRepository) Delete(ctx context.Context, merchantID, availabilityID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		UPDATE availabilities
		SET enabled = 0, update_date = ?
		WHERE availability_id = ? AND merchant_id = ? AND enabled = 1
	`

	result, err := db.ExecContext(ctx, query, time.Now().UTC(), availabilityID, merchantID)
	if err != nil {
		return fmt.Errorf("failed to delete availability: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("availability not found")
	}

	return nil
}

// GetAvailabilitiesForProduct récupère toutes les disponibilités actives pour un produit
func (r *AvailabilitiesRepository) GetAvailabilitiesForProduct(ctx context.Context, merchantID, productID string) ([]Availability, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		SELECT DISTINCT
			a.availability_id,
			a.merchant_id,
			a.availability_name,
			a.unavailable_message,
			a.enabled,
			a.creation_date,
			a.update_date
		FROM availabilities a
		INNER JOIN availabilities_products ap ON a.availability_id = ap.availability_id
		WHERE a.merchant_id = ? AND ap.product_id = ? AND a.enabled = 1
		ORDER BY a.creation_date DESC
	`

	rows, err := db.QueryContext(ctx, query, merchantID, productID)
	if err != nil {
		return nil, fmt.Errorf("failed to query availabilities for product: %w", err)
	}
	defer rows.Close()

	var availabilities []Availability
	var availabilityIDs []string

	for rows.Next() {
		var a Availability
		err := rows.Scan(
			&a.AvailabilityID,
			&a.MerchantID,
			&a.Name,
			&a.UnavailableMessage,
			&a.Enabled,
			&a.CreatedAt,
			&a.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan availability: %w", err)
		}
		availabilities = append(availabilities, a)
		availabilityIDs = append(availabilityIDs, a.AvailabilityID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Si pas de disponibilités, retourner la liste vide
	if len(availabilities) == 0 {
		return availabilities, nil
	}

	// Récupérer tous les créneaux en une seule requête
	schedulesByAvailability, err := r.getSchedulesByAvailabilityIDs(db, ctx, availabilityIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedules: %w", err)
	}

	// Mapper les résultats
	for i := range availabilities {
		availabilities[i].Schedules = schedulesByAvailability[availabilities[i].AvailabilityID]
		availabilities[i].ProductIDs = []string{productID}
	}

	return availabilities, nil
}

// ============ Helper Methods ============

// getProductIDsByAvailabilityIDs récupère tous les produits pour plusieurs disponibilités en une seule requête
func (r *AvailabilitiesRepository) getProductIDsByAvailabilityIDs(db dbutils.DBTX, ctx context.Context, availabilityIDs []string) (map[string][]string, error) {
	if len(availabilityIDs) == 0 {
		return make(map[string][]string), nil
	}

	// Construire la clause IN
	placeholders := make([]string, len(availabilityIDs))
	args := make([]interface{}, len(availabilityIDs))
	for i, id := range availabilityIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT availability_id, product_id
		FROM availabilities_products
		WHERE availability_id IN (%s)
		ORDER BY creation_date ASC
	`, strings.Join(placeholders, ","))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	productsByAvailability := make(map[string][]string)
	for rows.Next() {
		var availabilityID, productID string
		if err := rows.Scan(&availabilityID, &productID); err != nil {
			return nil, err
		}
		productsByAvailability[availabilityID] = append(productsByAvailability[availabilityID], productID)
	}

	return productsByAvailability, rows.Err()
}

// getSchedulesByAvailabilityIDs récupère tous les créneaux pour plusieurs disponibilités en une seule requête
func (r *AvailabilitiesRepository) getSchedulesByAvailabilityIDs(db dbutils.DBTX, ctx context.Context, availabilityIDs []string) (map[string][]AvailabilitySchedule, error) {
	if len(availabilityIDs) == 0 {
		return make(map[string][]AvailabilitySchedule), nil
	}

	// Construire la clause IN
	placeholders := make([]string, len(availabilityIDs))
	args := make([]interface{}, len(availabilityIDs))
	for i, id := range availabilityIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT 
			schedule_id,
			availability_id,
			day_of_week,
			available_from,
			available_to,
			creation_date,
			update_date
		FROM availabilities_schedules
		WHERE availability_id IN (%s)
		ORDER BY availability_id ASC, day_of_week ASC, available_from ASC
	`, strings.Join(placeholders, ","))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedulesByAvailability := make(map[string][]AvailabilitySchedule)
	for rows.Next() {
		var s AvailabilitySchedule
		if err := rows.Scan(
			&s.ScheduleID,
			&s.AvailabilityID,
			&s.DayOfWeek,
			&s.StartTime,
			&s.EndTime,
			&s.CreatedAt,
			&s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		schedulesByAvailability[s.AvailabilityID] = append(schedulesByAvailability[s.AvailabilityID], s)
	}

	return schedulesByAvailability, rows.Err()
}

// Helper pour convertir les requêtes de créneaux en objets Availability
func convertSchedules(availabilityID string, reqs []CreateAvailabilityScheduleReq, now time.Time) []AvailabilitySchedule {
	var schedules []AvailabilitySchedule
	for _, req := range reqs {
		schedules = append(schedules, AvailabilitySchedule{
			ScheduleID:     req.ScheduleID,
			AvailabilityID: availabilityID,
			DayOfWeek:      req.DayOfWeek,
			StartTime:      normalizeTime(req.StartTime),
			EndTime:        normalizeTime(req.EndTime),
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}
	return schedules
}

// Helper pour normaliser le format de l'heure
func normalizeTime(timeStr string) string {
	// Si le format est HH:MM, ajouter :00
	if strings.Count(timeStr, ":") == 1 {
		return timeStr + ":00"
	}
	return timeStr
}
