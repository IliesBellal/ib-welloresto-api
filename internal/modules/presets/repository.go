package presets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"welloresto-api/internal/database/dbx"
)

// ErrPresetNotFound is returned when no active merchant_presets row matches
// the requested code.
var ErrPresetNotFound = errors.New("merchant_preset_not_found")

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// GetActivePresetByCode returns the latest active version of a preset code.
// "Latest" — not a caller-supplied version — because the version a merchant
// ends up with is whatever is current the moment it applies the preset
// (ApplyPreset then freezes it on merchant.preset_version; it is never
// revisited). naf_codes is not selected — see models.go's MerchantPreset doc.
func (r *Repository) GetActivePresetByCode(ctx context.Context, code string) (*MerchantPreset, error) {
	db := dbx.GetDB(ctx, r.database)

	row := db.QueryRowContext(ctx, `
		SELECT id, code, version, label, description, config, is_active, created_at
		FROM merchant_presets
		WHERE code = ? AND is_active = TRUE
		ORDER BY version DESC
		LIMIT 1
	`, code)

	p := &MerchantPreset{}
	var configRaw []byte
	if err := row.Scan(&p.ID, &p.Code, &p.Version, &p.Label, &p.Description, &configRaw, &p.IsActive, &p.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrPresetNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(configRaw, &p.Config); err != nil {
		return nil, fmt.Errorf("merchant_presets %s v%d: unmarshal config: %w", p.Code, p.Version, err)
	}
	return p, nil
}

// SetMerchantPreset writes the frozen preset_code/preset_version onto
// merchant — the last step of ApplyPreset, once every other write has
// succeeded. merchant.id is an integer PK while merchantID circulates as a
// string everywhere in application code — same CAST convention as
// pos.POSRepository.SetDefaultRoleID.
func (r *Repository) SetMerchantPreset(ctx context.Context, merchantID, code string, version int) error {
	db := dbx.GetDB(ctx, r.database)

	castExpr := "CAST(id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		castExpr = "CAST(id AS TEXT)"
	}

	_, err := db.ExecContext(ctx,
		"UPDATE merchant SET preset_code = ?, preset_version = ? WHERE "+castExpr+" = ?",
		code, version, merchantID,
	)
	return err
}

// UpdateMerchantParametersFromConfig applies the merchant_parameters-facing
// half of a preset config (channels, kitchen, payments, prep_times,
// covers_required) in a single UPDATE. Categories and floor_plan are applied
// separately (menu.MenuRepository, locations.LocationsRepository — see
// service.go) since they are entities, not merchant_parameters columns.
func (r *Repository) UpdateMerchantParametersFromConfig(ctx context.Context, merchantID string, cfg PresetConfig) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE merchant_parameters
		SET manage_on_site = ?,
		    manage_take_away = ?,
		    manage_delivery = ?,
		    production_display_mode = ?,
		    pager_number_required = ?,
		    cash_register_required_for_ordering = ?,
		    waiter_app_can_cash_in = ?,
		    preparation_time_mode = ?,
		    preparation_time = ?,
		    minimum_preparation_time = ?,
		    maximum_preparation_time = ?,
		    pos_covers_count_required = ?
		WHERE merchant_id = ?
	`,
		cfg.Channels.OnSite, cfg.Channels.TakeAway, cfg.Channels.Delivery,
		cfg.Kitchen.Display, cfg.Kitchen.CallNumbers,
		cfg.CashHandling.CashRegisterRequiredForOrdering, cfg.CashHandling.WaiterAppCanCashIn,
		cfg.PrepTimes.Mode, cfg.PrepTimes.PreparationTime, cfg.PrepTimes.MinimumPreparationTime, cfg.PrepTimes.MaximumPreparationTime,
		cfg.CoversRequired,
		merchantID,
	)
	return err
}
