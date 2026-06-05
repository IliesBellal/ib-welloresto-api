package upsell

import (
	"context"
	"database/sql"
	"encoding/json"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

// Repository handles all persistence operations for the upsell_suggestions table
// and the upsell settings stored in merchant_parameters.
type Repository struct {
	database *sql.DB
}

// NewRepository creates a new upsell Repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// CreateSuggestion inserts a new suggestion row and returns the generated ID.
// suggested_items is serialised to JSON before insertion.
func (r *Repository) CreateSuggestion(ctx context.Context, params CreateSuggestionParams) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	id := helpers.GeneratePrefixedID(helpers.UpsellSuggestionIDPrefix)

	itemsJSON, err := json.Marshal(params.SuggestedItems)
	if err != nil {
		log.Error("upsell: failed to marshal suggested_items: " + err.Error())
		return "", err
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO upsell_suggestions
			(id, merchant_id, cart_signature, suggested_items, source,
			 llm_provider, llm_model, tokens_in, tokens_out, latency_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		id,
		params.MerchantID,
		params.CartSignature,
		itemsJSON,
		params.Source,
		params.LLMProvider,
		params.LLMModel,
		params.TokensIn,
		params.TokensOut,
		params.LatencyMs,
	)
	if err != nil {
		log.Error("upsell: CreateSuggestion insert failed: " + err.Error())
		return "", err
	}

	return id, nil
}

// RecordAcceptance writes the acceptance data back onto an existing suggestion.
// The operation is idempotent: if accepted_items is already set the call is a
// no-op and returns nil.
// Returns ErrSuggestionNotFound when the row does not exist.
// Returns ErrSuggestionMerchantMismatch when the suggestion belongs to a
// different merchant; the caller is responsible for logging a security warning.
func (r *Repository) RecordAcceptance(ctx context.Context, suggestionID string, merchantID string, params RecordAcceptanceParams) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Read the current row to enforce idempotency and ownership.
	var storedMerchantID string
	var acceptedItemsRaw []byte
	err := db.QueryRowContext(ctx, `
		SELECT merchant_id, accepted_items
		FROM upsell_suggestions
		WHERE id = ?
		LIMIT 1
	`, suggestionID).Scan(&storedMerchantID, &acceptedItemsRaw)
	if err == sql.ErrNoRows {
		return ErrSuggestionNotFound
	}
	if err != nil {
		log.Error("upsell: RecordAcceptance select failed: " + err.Error())
		return err
	}

	// Idempotency: already tracked.
	if acceptedItemsRaw != nil {
		log.Info("upsell: acceptance already tracked for suggestion " + suggestionID)
		return nil
	}

	// Ownership check.
	if storedMerchantID != merchantID {
		return ErrSuggestionMerchantMismatch
	}

	acceptedJSON, err := json.Marshal(params.AcceptedItems)
	if err != nil {
		log.Error("upsell: failed to marshal accepted_items: " + err.Error())
		return err
	}

	_, err = db.ExecContext(ctx, `
		UPDATE upsell_suggestions
		SET order_id       = ?,
		    accepted_items = ?,
		    revenue_impact = ?
		WHERE id = ?
	`, params.OrderID, acceptedJSON, params.RevenueImpact, suggestionID)
	if err != nil {
		log.Error("upsell: RecordAcceptance update failed: " + err.Error())
		return err
	}

	return nil
}

// GetSuggestion fetches a single suggestion by ID and deserialises the JSON columns.
// Returns ErrSuggestionNotFound when the row does not exist.
func (r *Repository) GetSuggestion(ctx context.Context, suggestionID string) (*Suggestion, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var s Suggestion
	var suggestedItemsRaw []byte
	var acceptedItemsRaw []byte
	var revenueImpact sql.NullFloat64

	err := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, order_id, cart_signature,
		       suggested_items, source,
		       accepted_items, revenue_impact,
		       llm_provider, llm_model, tokens_in, tokens_out, latency_ms,
		       created_at
		FROM upsell_suggestions
		WHERE id = ?
		LIMIT 1
	`, suggestionID).Scan(
		&s.ID,
		&s.MerchantID,
		&s.OrderID,
		&s.CartSignature,
		&suggestedItemsRaw,
		&s.Source,
		&acceptedItemsRaw,
		&revenueImpact,
		&s.LLMProvider,
		&s.LLMModel,
		&s.TokensIn,
		&s.TokensOut,
		&s.LatencyMs,
		&s.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrSuggestionNotFound
	}
	if err != nil {
		log.Error("upsell: GetSuggestion query failed: " + err.Error())
		return nil, err
	}

	if err := json.Unmarshal(suggestedItemsRaw, &s.SuggestedItems); err != nil {
		log.Error("upsell: failed to unmarshal suggested_items: " + err.Error())
		return nil, err
	}

	if acceptedItemsRaw != nil {
		if err := json.Unmarshal(acceptedItemsRaw, &s.AcceptedItems); err != nil {
			log.Error("upsell: failed to unmarshal accepted_items: " + err.Error())
			return nil, err
		}
	}

	if revenueImpact.Valid {
		s.RevenueImpact = &revenueImpact.Float64
	}

	return &s, nil
}

// DeleteOldSuggestions removes suggestions older than olderThanMonths months.
// Returns the number of deleted rows, intended for cron logging.
func (r *Repository) DeleteOldSuggestions(ctx context.Context, olderThanMonths int) (int64, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	res, err := db.ExecContext(ctx, `
		DELETE FROM upsell_suggestions
		WHERE created_at < DATE_SUB(NOW(), INTERVAL ? MONTH)
	`, olderThanMonths)
	if err != nil {
		log.Error("upsell: DeleteOldSuggestions failed: " + err.Error())
		return 0, err
	}

	deleted, err := res.RowsAffected()
	if err != nil {
		log.Error("upsell: DeleteOldSuggestions RowsAffected failed: " + err.Error())
		return 0, err
	}

	return deleted, nil
}

// ListFeaturedProducts returns up to limit products marked as popular for a merchant.
// Each result is shaped into a SuggestedItem with a default title template and score 0.5.
func (r *Repository) ListFeaturedProducts(ctx context.Context, merchantID string, limit int) ([]SuggestedItem, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx, `
		SELECT product_id, name, price, image_url
		FROM products
		WHERE merchant_id = ?
		  AND is_popular  = 1
		  AND available   = 1
		  AND enabled     = 1
		  AND status      IN ('available', '1')
		LIMIT ?
	`, merchantID, limit)
	if err != nil {
		log.Error("upsell: ListFeaturedProducts query failed: " + err.Error())
		return nil, err
	}
	defer rows.Close()

	result := make([]SuggestedItem, 0)
	for rows.Next() {
		var productID, name string
		var price int64
		var imageURL sql.NullString
		if err := rows.Scan(&productID, &name, &price, &imageURL); err != nil {
			log.Error("upsell: ListFeaturedProducts scan failed: " + err.Error())
			return nil, err
		}
		item := SuggestedItem{
			ProductID: productID,
			Title:     "Notre sélection : " + name,
			Score:     0.5,
			Name:      name,
			Price:     price,
		}
		if imageURL.Valid {
			item.ImageURL = &imageURL.String
		}
		result = append(result, item)
	}

	return result, rows.Err()
}

// GetMerchantUpsellSettings reads enable_upsell and upsell_max_items from
// merchant_parameters.
// Returns safe defaults (false, 3, nil) when no row exists for the merchant.
// maxItems is capped between 1 and 10 regardless of the stored value.
func (r *Repository) GetMerchantUpsellSettings(ctx context.Context, merchantID string) (enabled bool, maxItems int, err error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var rawEnabled sql.NullBool
	var rawMaxItems sql.NullInt64

	queryErr := db.QueryRowContext(ctx, `
		SELECT enable_upsell, upsell_max_items
		FROM merchant_parameters
		WHERE merchant_id = ?
		LIMIT 1
	`, merchantID).Scan(&rawEnabled, &rawMaxItems)

	if queryErr == sql.ErrNoRows {
		return false, 3, nil
	}
	if queryErr != nil {
		log.Error("upsell: GetMerchantUpsellSettings failed: " + queryErr.Error())
		return false, 3, queryErr
	}

	if rawEnabled.Valid {
		enabled = rawEnabled.Bool
	}

	maxItems = 3 // safe default
	if rawMaxItems.Valid {
		maxItems = int(rawMaxItems.Int64)
	}

	// Cap between 1 and 10.
	if maxItems < 1 {
		maxItems = 1
	}
	if maxItems > 10 {
		maxItems = 10
	}

	return enabled, maxItems, nil
}
