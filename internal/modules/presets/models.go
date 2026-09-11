package presets

import (
	"fmt"
	"strings"
	"time"
)

// MerchantPreset mirrors one row of merchant_presets (one code/version pair).
// naf_codes is deliberately not modeled here — nothing in this chantier reads
// it back (see docs/decisions.md, Chantier 5b) — only the seed migration
// writes it, via a plain Postgres array literal.
type MerchantPreset struct {
	ID          string
	Code        string
	Version     int
	Label       string
	Description string
	Config      PresetConfig
	IsActive    bool
	CreatedAt   time.Time
}

// PresetConfig is the shape of merchant_presets.config (JSONB). Every field
// here corresponds to a real merchant_parameters column or a creatable
// entity (productcateg, floors, locations) — see docs/decisions.md, Chantier
// 5b, for the field-by-field correspondence this was confirmed against.
// suggested_modules is the one exception: advisory-only, never applied by
// ApplyPreset.
type PresetConfig struct {
	Channels         ChannelsConfig     `json:"channels"`
	FloorPlan        FloorPlanConfig    `json:"floor_plan"`
	Kitchen          KitchenConfig      `json:"kitchen"`
	Categories       []string           `json:"categories"`
	CashHandling     CashHandlingConfig `json:"cash_handling"`
	PrepTimes        PrepTimesConfig    `json:"prep_times"`
	CoversRequired   bool               `json:"covers_required"`
	SuggestedModules []string           `json:"suggested_modules"`
}

// ChannelsConfig -> merchant_parameters.manage_on_site/manage_take_away/manage_delivery.
type ChannelsConfig struct {
	OnSite   bool `json:"on_site"`
	TakeAway bool `json:"take_away"`
	Delivery bool `json:"delivery"`
}

// FloorPlanZone -> one floors row, plus TableCount locations rows (uniform
// seats/shape per zone — a preset cannot know a real room layout, only a
// reasonable default headcount).
type FloorPlanZone struct {
	Name       string `json:"name"`
	TableCount int    `json:"table_count"`
	Seats      int    `json:"seats"`
	Shape      string `json:"shape"`
}

type FloorPlanConfig struct {
	Enabled bool            `json:"enabled"`
	Zones   []FloorPlanZone `json:"zones"`
}

// KitchenConfig -> merchant_parameters.production_display_mode/pager_number_required.
type KitchenConfig struct {
	Display     string `json:"display"`
	CallNumbers bool   `json:"call_numbers"`
}

// CashHandlingConfig -> merchant_parameters.cash_register_required_for_ordering/waiter_app_can_cash_in.
// Named for what these two columns actually control (whether cash can be
// taken at the till / by a waiter's device), not "payments" — real
// configurable payment methods are a lot C concern, on a field this would
// otherwise collide with.
type CashHandlingConfig struct {
	CashRegisterRequiredForOrdering bool `json:"cash_register_required_for_ordering"`
	WaiterAppCanCashIn              bool `json:"waiter_app_can_cash_in"`
}

// PrepTimesConfig -> merchant_parameters.preparation_time_mode/preparation_time/
// minimum_preparation_time/maximum_preparation_time.
type PrepTimesConfig struct {
	Mode                   string `json:"mode"`
	PreparationTime        int    `json:"preparation_time"`
	MinimumPreparationTime int    `json:"minimum_preparation_time"`
	MaximumPreparationTime int    `json:"maximum_preparation_time"`
}

var validTableShapes = map[string]bool{"circle": true, "square": true, "rectangle": true, "oval": true}
var validKitchenDisplays = map[string]bool{"CLASSIC": true, "PRODUCT_FOCUS": true}
var validPrepModes = map[string]bool{"AUTO": true, "MANUAL": true}

// Validate rejects a config before ApplyPreset writes anything, so a
// malformed preset can never leave a merchant half-configured.
func (c PresetConfig) Validate() error {
	if !validKitchenDisplays[c.Kitchen.Display] {
		return fmt.Errorf("preset config: kitchen.display %q is not one of CLASSIC, PRODUCT_FOCUS", c.Kitchen.Display)
	}
	if !validPrepModes[c.PrepTimes.Mode] {
		return fmt.Errorf("preset config: prep_times.mode %q is not one of AUTO, MANUAL", c.PrepTimes.Mode)
	}
	if c.PrepTimes.PreparationTime <= 0 {
		return fmt.Errorf("preset config: prep_times.preparation_time must be > 0")
	}
	if c.PrepTimes.MinimumPreparationTime <= 0 || c.PrepTimes.MaximumPreparationTime <= 0 {
		return fmt.Errorf("preset config: prep_times.minimum_preparation_time/maximum_preparation_time must be > 0")
	}
	if c.PrepTimes.MinimumPreparationTime > c.PrepTimes.MaximumPreparationTime {
		return fmt.Errorf("preset config: prep_times.minimum_preparation_time (%d) exceeds maximum_preparation_time (%d)", c.PrepTimes.MinimumPreparationTime, c.PrepTimes.MaximumPreparationTime)
	}
	for i, name := range c.Categories {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("preset config: categories[%d] is blank", i)
		}
	}
	if c.FloorPlan.Enabled {
		if len(c.FloorPlan.Zones) == 0 {
			return fmt.Errorf("preset config: floor_plan.enabled is true but zones is empty")
		}
		for i, z := range c.FloorPlan.Zones {
			if strings.TrimSpace(z.Name) == "" {
				return fmt.Errorf("preset config: floor_plan.zones[%d].name is blank", i)
			}
			if z.TableCount <= 0 {
				return fmt.Errorf("preset config: floor_plan.zones[%d].table_count must be > 0", i)
			}
			if z.Seats <= 0 {
				return fmt.Errorf("preset config: floor_plan.zones[%d].seats must be > 0", i)
			}
			if !validTableShapes[z.Shape] {
				return fmt.Errorf("preset config: floor_plan.zones[%d].shape %q is not one of circle, square, rectangle, oval", i, z.Shape)
			}
		}
	}
	return nil
}
