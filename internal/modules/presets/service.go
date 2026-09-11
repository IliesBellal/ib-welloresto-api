package presets

import (
	"context"
	"fmt"

	"welloresto-api/internal/modules/locations"
	"welloresto-api/internal/modules/menu"
)

type Service struct {
	presetsRepo   *Repository
	menuRepo      *menu.MenuRepository
	locationsRepo *locations.LocationsRepository
}

func NewService(presetsRepo *Repository, menuRepo *menu.MenuRepository, locationsRepo *locations.LocationsRepository) *Service {
	return &Service{presetsRepo: presetsRepo, menuRepo: menuRepo, locationsRepo: locationsRepo}
}

// Default table footprint for a preset-generated floor plan. A preset can
// only ever guess at a plausible default layout — never a real room — so
// tables are laid out on a simple grid, four per row, rather than trying to
// invent meaningful pixel positions.
const (
	defaultTableWidth  = 80.0
	defaultTableHeight = 80.0
	tableGridGapX      = 40.0
	tableGridGapY      = 40.0
	tablesPerRow       = 4
)

// ApplyPreset applies presetCode's current active version to merchantID:
// merchant_parameters columns, productcateg rows, floors/locations rows (if
// the preset enables a floor plan), then freezes preset_code/preset_version
// on merchant. Must run inside the same transaction as the merchant's
// creation (ambient via ctx — see internal/utils/dbutils.RunInTx and
// internal/database/dbx.GetDB, which every repository call below goes
// through) — a failure partway through must roll back the whole merchant,
// never leave one half-configured.
//
// Config is validated (PresetConfig.Validate) before any write, so a
// malformed preset can never produce an inconsistent merchant.
func (s *Service) ApplyPreset(ctx context.Context, merchantID, presetCode string) error {
	preset, err := s.presetsRepo.GetActivePresetByCode(ctx, presetCode)
	if err != nil {
		return err
	}
	if err := preset.Config.Validate(); err != nil {
		return fmt.Errorf("ApplyPreset(%s): %w", presetCode, err)
	}

	if err := s.presetsRepo.UpdateMerchantParametersFromConfig(ctx, merchantID, preset.Config); err != nil {
		return fmt.Errorf("ApplyPreset(%s): merchant_parameters: %w", presetCode, err)
	}

	for _, name := range preset.Config.Categories {
		if _, err := s.menuRepo.CreateProductCategory(ctx, &menu.CreateProductCategoryPayload{
			Name:       name,
			MerchantID: merchantID,
		}); err != nil {
			return fmt.Errorf("ApplyPreset(%s): category %q: %w", presetCode, name, err)
		}
	}

	if preset.Config.FloorPlan.Enabled {
		for _, zone := range preset.Config.FloorPlan.Zones {
			floorID, err := s.locationsRepo.CreateFloor(ctx, merchantID, zone.Name)
			if err != nil {
				return fmt.Errorf("ApplyPreset(%s): zone %q: %w", presetCode, zone.Name, err)
			}
			for i := 0; i < zone.TableCount; i++ {
				row := i / tablesPerRow
				col := i % tablesPerRow
				_, err := s.locationsRepo.CreateTable(ctx, merchantID, floorID, locations.CreateTableRequest{
					LocationName: fmt.Sprintf("%s %d", zone.Name, i+1),
					Seats:        zone.Seats,
					Shape:        zone.Shape,
					X:            float64(col) * (defaultTableWidth + tableGridGapX),
					Y:            float64(row) * (defaultTableHeight + tableGridGapY),
					Width:        defaultTableWidth,
					Height:       defaultTableHeight,
				})
				if err != nil {
					return fmt.Errorf("ApplyPreset(%s): zone %q table %d: %w", presetCode, zone.Name, i+1, err)
				}
			}
		}
	}

	if err := s.presetsRepo.SetMerchantPreset(ctx, merchantID, preset.Code, preset.Version); err != nil {
		return fmt.Errorf("ApplyPreset(%s): freeze preset_code/preset_version: %w", presetCode, err)
	}

	return nil
}
