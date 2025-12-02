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

	var stmt *sql.Stmt

	// UPDATE
	if c.CustomerID != nil && *c.CustomerID != "" {

		setParts := make([]string, len(fields))
		for i, field := range fields {
			setParts[i] = fmt.Sprintf("%s = COALESCE(:%s, %s)", field, field, field)
		}

		sqlQuery := `
			UPDATE customer SET ` + strings.Join(setParts, ", ") + `
			WHERE customer_id = :customer_id
			  AND merchant_id = :merchant_id
		`

		stmt, err = tx.Prepare(sqlQuery)
		if err != nil {
			return "", err
		}

	} else {
		// INSERT
		cols := strings.Join(fields, ", ")
		placeholders := ":" + strings.Join(fields, ", :")

		sqlQuery := `
			INSERT INTO customer (` + cols + `, merchant_id)
			VALUES (` + placeholders + `, :merchant_id)
		`

		stmt, err = tx.Prepare(sqlQuery)
		if err != nil {
			return "", err
		}
	}

	defer stmt.Close()

	params := map[string]interface{}{
		"merchant_id": c.MerchantID,
	}

	// Bind dynamic parameters
	for _, f := range fields {

		switch f {

		case "customer_name":
			if c.CustomerName != nil && *c.CustomerName != "" {
				val := ucfirst(*c.CustomerName)
				params[f] = val
			} else {
				params[f] = nil
			}

		case "customer_tel":
			if c.CustomerTel != nil && *c.CustomerTel != "" {
				val := normalizePhoneNumber(*c.CustomerTel)
				params[f] = val
			} else {
				params[f] = nil
			}

		// FLOATS
		case "customer_lat":
			if c.CustomerLat != nil {
				params[f] = *c.CustomerLat
			} else {
				params[f] = nil
			}

		case "customer_lng":
			if c.CustomerLng != nil {
				params[f] = *c.CustomerLng
			} else {
				params[f] = nil
			}

		case "customer_temporary_lat":
			if c.CustomerTemporaryLat != nil {
				params[f] = *c.CustomerTemporaryLat
			} else {
				params[f] = nil
			}

		case "customer_temporary_lng":
			if c.CustomerTemporaryLng != nil {
				params[f] = *c.CustomerTemporaryLng
			} else {
				params[f] = nil
			}

		// STRINGS
		default:
			// generic string pointer
			valPtr := getStringField(c, f)
			if valPtr != nil && *valPtr != "" {
				params[f] = *valPtr
			} else {
				params[f] = nil
			}
		}
	}

	if c.CustomerID != nil {
		params["customer_id"] = *c.CustomerID
	}

	_, err = stmt.Exec(params)
	if err != nil {
		return "", err
	}

	var newID string

	if c.CustomerID != nil && *c.CustomerID != "" {
		newID = *c.CustomerID
	} else {
		err = tx.QueryRow("SELECT LAST_INSERT_ID()").Scan(&newID)
		if err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return newID, nil
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
