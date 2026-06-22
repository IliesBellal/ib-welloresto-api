package printers

import (
	"time"

	"welloresto-api/internal/models"
)

const (
	RoleCaisse                 = "caisse"
	RoleProduction             = "production"
	RoleCaisseEtProduction     = "caisse_et_production"
	RoleLabelHaccp             = "label_haccp"
	RoleLabelProduction        = "label_production"
	RoleLabelHaccpEtProduction = "label_haccp_et_production"
)

var validRoles = map[string]bool{
	RoleCaisse:                 true,
	RoleProduction:             true,
	RoleCaisseEtProduction:     true,
	RoleLabelHaccp:             true,
	RoleLabelProduction:        true,
	RoleLabelHaccpEtProduction: true,
}

// languageForRole returns "zpl" for label roles, "esc_pos" for caisse/production roles.
// Mirrors the same rule applied client-side in Flutter.
func languageForRole(role string) string {
	switch role {
	case RoleLabelHaccp, RoleLabelProduction, RoleLabelHaccpEtProduction:
		return "zpl"
	default:
		return "esc_pos"
	}
}

// PrinterEntry is the response DTO returned for every printer operation.
type PrinterEntry struct {
	ID                    string    `json:"id"`
	MerchantID            string    `json:"merchant_id"`
	Name                  string    `json:"name"`
	ConnectionType        string    `json:"connection_type"`
	IPAddress             *string   `json:"ip_address,omitempty"`
	Port                  int       `json:"port"`
	BluetoothAddress      *string   `json:"bluetooth_address,omitempty"`
	Role                  string    `json:"role"`
	Language              string    `json:"language"`
	Enabled               bool      `json:"enabled"`
	ProductionProductIDs  []string  `json:"production_product_ids"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// CreatePrinterRequest is the DTO for creating a printer.
type CreatePrinterRequest struct {
	Name                 string    `json:"name"`
	ConnectionType       string    `json:"connection_type"`
	IPAddress            *string   `json:"ip_address,omitempty"`
	Port                 *int      `json:"port,omitempty"`
	BluetoothAddress     *string   `json:"bluetooth_address,omitempty"`
	Role                 string    `json:"role"`
	ProductionProductIDs *[]string `json:"production_product_ids,omitempty"`
}

func (r *CreatePrinterRequest) Validate() error {
	if len(r.Name) == 0 {
		return models.ErrInvalidInput
	}
	if r.ConnectionType != "wifi" && r.ConnectionType != "bluetooth" {
		return models.ErrInvalidInput
	}
	if r.ConnectionType == "wifi" && (r.IPAddress == nil || len(*r.IPAddress) == 0) {
		return models.ErrInvalidInput
	}
	if r.ConnectionType == "bluetooth" && (r.BluetoothAddress == nil || len(*r.BluetoothAddress) == 0) {
		return models.ErrInvalidInput
	}
	if !validRoles[r.Role] {
		return models.ErrInvalidInput
	}
	return nil
}

// UpdatePrinterRequest is the DTO for partial updates on a printer.
// All fields are optional; only non-nil fields are written to the DB.
type UpdatePrinterRequest struct {
	Name                 *string   `json:"name,omitempty"`
	ConnectionType       *string   `json:"connection_type,omitempty"`
	IPAddress            *string   `json:"ip_address,omitempty"`
	Port                 *int      `json:"port,omitempty"`
	BluetoothAddress     *string   `json:"bluetooth_address,omitempty"`
	Role                 *string   `json:"role,omitempty"`
	ProductionProductIDs *[]string `json:"production_product_ids,omitempty"`
}

func (r *UpdatePrinterRequest) Validate() error {
	if r.Name != nil && len(*r.Name) == 0 {
		return models.ErrInvalidInput
	}
	if r.ConnectionType != nil && *r.ConnectionType != "wifi" && *r.ConnectionType != "bluetooth" {
		return models.ErrInvalidInput
	}
	if r.Role != nil && !validRoles[*r.Role] {
		return models.ErrInvalidInput
	}
	return nil
}
