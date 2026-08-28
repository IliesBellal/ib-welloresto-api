package productionprofiles

import (
	"time"

	"welloresto-api/internal/models"
)

// ProductionProfileEntry is the response DTO for list/create/update
// operations — it does not carry the product association matrix (see
// ProductionProfileDetail for that).
//
// SplitBySource and DisplayOnlyPaidOrders are production-screen display
// settings that used to live in the Flutter app's local SharedPreferences
// (ProductionSettingsNotifier, per-device) — they are profile-level fields
// now, exactly like Name, so they travel with the profile's definition
// instead of being reconfigured on every device.
type ProductionProfileEntry struct {
	ID                    string    `json:"id"`
	MerchantID            string    `json:"merchant_id"`
	Name                  string    `json:"name"`
	SplitBySource         bool      `json:"split_by_source"`
	DisplayOnlyPaidOrders bool      `json:"display_only_paid_orders"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// ProductProductionProfile is one row of a profile's product association
// matrix. Only products carrying at least one true flag are ever returned —
// a sparse list, mirroring the allergens/tags association pattern.
type ProductProductionProfile struct {
	ProductID     string `json:"product_id"`
	ShouldProduce bool   `json:"should_produce"`
	ShouldMonitor bool   `json:"should_monitor"`
}

// ProductionProfileDetail is the response DTO for GET /production-profiles/{id}.
type ProductionProfileDetail struct {
	ID                    string                     `json:"id"`
	MerchantID            string                     `json:"merchant_id"`
	Name                  string                     `json:"name"`
	SplitBySource         bool                       `json:"split_by_source"`
	DisplayOnlyPaidOrders bool                       `json:"display_only_paid_orders"`
	CreatedAt             time.Time                  `json:"created_at"`
	UpdatedAt             time.Time                  `json:"updated_at"`
	Products              []ProductProductionProfile `json:"products"`
}

// CreateProductionProfileRequest is the DTO for creating a profile.
// SplitBySource defaults to true and DisplayOnlyPaidOrders to false when
// omitted, matching ProductionSettingsNotifier's pre-migration defaults.
type CreateProductionProfileRequest struct {
	Name                  string `json:"name"`
	SplitBySource         *bool  `json:"split_by_source,omitempty"`
	DisplayOnlyPaidOrders *bool  `json:"display_only_paid_orders,omitempty"`
}

func (r *CreateProductionProfileRequest) Validate() error {
	if len(r.Name) == 0 {
		return models.ErrInvalidInput
	}
	return nil
}

// UpdateProductionProfileRequest is the DTO for partial updates on a
// profile. All fields are optional; only non-nil fields are written.
type UpdateProductionProfileRequest struct {
	Name                  *string `json:"name,omitempty"`
	SplitBySource         *bool   `json:"split_by_source,omitempty"`
	DisplayOnlyPaidOrders *bool   `json:"display_only_paid_orders,omitempty"`
}

func (r *UpdateProductionProfileRequest) Validate() error {
	if r.Name != nil && len(*r.Name) == 0 {
		return models.ErrInvalidInput
	}
	return nil
}

// ProductProductionProfileInput is one row of the desired state sent to
// PUT /production-profiles/{id}/products.
type ProductProductionProfileInput struct {
	ProductID     string `json:"product_id"`
	ShouldProduce bool   `json:"should_produce"`
	ShouldMonitor bool   `json:"should_monitor"`
}

// ReplaceProductsRequest is the body of PUT /production-profiles/{id}/products:
// the full desired state of the association matrix (full-replace, same
// semantics as MenuRepository.SyncProductAllergens). A product absent from
// the list is implicitly should_produce=false, should_monitor=false.
type ReplaceProductsRequest []ProductProductionProfileInput

func (req ReplaceProductsRequest) Validate() error {
	seen := make(map[string]bool, len(req))
	for _, item := range req {
		if len(item.ProductID) == 0 {
			return models.ErrInvalidInput
		}
		if seen[item.ProductID] {
			return models.ErrInvalidInput
		}
		seen[item.ProductID] = true
	}
	return nil
}
