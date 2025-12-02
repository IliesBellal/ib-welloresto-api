package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type CustomerRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewCustomerRepository(db *sql.DB) *CustomerRepository {
	return &CustomerRepository{db: db}
}

// ------------------------------------------------------
// normalizePhoneNumber (version simple)
// ------------------------------------------------------
func normalizePhoneNumber(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, ".", "")
	phone = strings.ReplaceAll(phone, "-", "")
	return phone
}

// ------------------------------------------------------
// ucfirst
// ------------------------------------------------------
func ucfirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (r *CustomerRepository) UpdateOrCreateCustomer(ctx context.Context, c *models.Customer) (string, error) {

	if c.MerchantID == "" {
		return "", errors.New("merchant_id is required")
	}

	fields := []string{
		"customer_name", "customer_tel", "customer_address", "customer_email",
		"customer_lat", "customer_lng", "customer_door_number", "customer_floor_number",
		"customer_additional_address", "customer_business_name", "customer_birthdate",
		"customer_additional_info", "customer_temporary_address", "customer_temporary_lat",
		"customer_temporary_lng", "customer_temporary_door_number",
		"customer_temporary_floor_number", "customer_temporary_additional_address",
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	values := make([]interface{}, 0)

	//------------------------------------------
	// UPDATE
	//------------------------------------------
	if c.CustomerID != nil && *c.CustomerID != "" {

		setParts := []string{}
		for _, f := range fields {
			setParts = append(setParts, fmt.Sprintf("%s = COALESCE(?, %s)", f, f))
		}

		sqlQuery := `
			UPDATE customer
			SET ` + strings.Join(setParts, ", ") + `
			WHERE customer_id = ? AND merchant_id = ?
		`

		// params
		for _, f := range fields {
			values = append(values, extractFieldValue(c, f))
		}
		values = append(values, *c.CustomerID)
		values = append(values, c.MerchantID)

		_, err := tx.ExecContext(ctx, sqlQuery, values...)
		if err != nil {
			return "", err
		}

		if err := tx.Commit(); err != nil {
			return "", err
		}

		return *c.CustomerID, nil
	}

	//------------------------------------------
	// INSERT
	//------------------------------------------
	cols := strings.Join(fields, ", ")
	placeholders := strings.TrimRight(strings.Repeat("?, ", len(fields)), ", ")

	sqlQuery := `
		INSERT INTO customer (` + cols + `, merchant_id)
		VALUES (` + placeholders + `, ?)
	`

	for _, f := range fields {
		values = append(values, extractFieldValue(c, f))
	}
	values = append(values, c.MerchantID)

	res, err := tx.ExecContext(ctx, sqlQuery, values...)
	if err != nil {
		return "", err
	}

	newID, err := res.LastInsertId()
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return fmt.Sprintf("%d", newID), nil
}

func extractFieldValue(c *models.Customer, field string) interface{} {

	switch field {

	case "customer_name":
		if c.CustomerName != nil && *c.CustomerName != "" {
			return ucfirst(*c.CustomerName)
		}
		return nil

	case "customer_tel":
		if c.CustomerTel != nil && *c.CustomerTel != "" {
			return normalizePhoneNumber(*c.CustomerTel)
		}
		return nil

	// FLOATS
	case "customer_lat":
		if c.CustomerLat != nil {
			return *c.CustomerLat
		}
		return nil

	case "customer_lng":
		if c.CustomerLng != nil {
			return *c.CustomerLng
		}
		return nil

	case "customer_temporary_lat":
		if c.CustomerTemporaryLat != nil {
			return *c.CustomerTemporaryLat
		}
		return nil

	case "customer_temporary_lng":
		if c.CustomerTemporaryLng != nil {
			return *c.CustomerTemporaryLng
		}
		return nil

	// STRINGS génériques
	default:
		ptr := getStringField(c, field)
		if ptr != nil && *ptr != "" {
			return *ptr
		}
		return nil
	}
}

// --------------------------------------------------
// helper pour récupérer un *string selon un field
// --------------------------------------------------
func getStringField(c *models.Customer, name string) *string {

	switch name {
	case "customer_address":
		return c.CustomerAddress
	case "customer_email":
		return c.CustomerEmail
	case "customer_door_number":
		return c.CustomerDoorNumber
	case "customer_floor_number":
		return c.CustomerFloorNumber
	case "customer_additional_address":
		return c.CustomerAdditionalAddress
	case "customer_business_name":
		return c.CustomerBusinessName
	case "customer_birthdate":
		return c.CustomerBirthdate
	case "customer_additional_info":
		return c.CustomerAdditionalInfo
	case "customer_temporary_address":
		return c.CustomerTemporaryAddress
	case "customer_temporary_door_number":
		return c.CustomerTemporaryDoorNumber
	case "customer_temporary_floor_number":
		return c.CustomerTemporaryFloorNumber
	case "customer_temporary_additional_address":
		return c.CustomerTemporaryAdditionalAddress
	}

	return nil
}
