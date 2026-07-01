package printers

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/go-sql-driver/mysql"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// printerScanner abstracts *sql.Row and *sql.Rows so a single scan helper covers both.
type printerScanner interface {
	Scan(dest ...interface{}) error
}

func scanPrinter(s printerScanner) (PrinterEntry, error) {
	var p PrinterEntry
	var ipAddress, bluetoothAddress, productionProductIDs sql.NullString
	err := s.Scan(
		&p.ID, &p.MerchantID, &p.Name, &p.ConnectionType,
		&ipAddress, &p.Port, &bluetoothAddress, &p.Role, &p.Language,
		&p.Enabled, &productionProductIDs, &p.PaperWidthMm, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return PrinterEntry{}, err
	}
	if ipAddress.Valid {
		p.IPAddress = &ipAddress.String
	}
	if bluetoothAddress.Valid {
		p.BluetoothAddress = &bluetoothAddress.String
	}
	p.ProductionProductIDs = make([]string, 0)
	if productionProductIDs.Valid {
		_ = json.Unmarshal([]byte(productionProductIDs.String), &p.ProductionProductIDs)
	}
	return p, nil
}

const selectCols = `
	SELECT printer_id, merchant_id, name, connection_type,
	       ip_address, port, bluetooth_address, role, language,
	       enabled, production_product_ids, paper_width_mm, created_at, updated_at
	FROM printers`

// ListPrinters returns all enabled printers for a merchant.
func (r *Repository) ListPrinters(ctx context.Context, merchantID string) ([]PrinterEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx,
		selectCols+` WHERE merchant_id = ? AND enabled = 1 ORDER BY name ASC`,
		merchantID,
	)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	result := make([]PrinterEntry, 0)
	for rows.Next() {
		p, err := scanPrinter(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// GetPrinter returns a single enabled printer, verifying merchant ownership.
func (r *Repository) GetPrinter(ctx context.Context, merchantID, printerID string) (*PrinterEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	row := db.QueryRowContext(ctx,
		selectCols+` WHERE printer_id = ? AND merchant_id = ? AND enabled = 1`,
		printerID, merchantID,
	)
	p, err := scanPrinter(row)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	return &p, nil
}

// CreatePrinter inserts a new printer and returns the created entry.
func (r *Repository) CreatePrinter(ctx context.Context, merchantID string, req *CreatePrinterRequest, printerID, language string) (*PrinterEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	port := 9100
	if req.Port != nil {
		port = *req.Port
	}

	paperWidthMm := 57
	if req.PaperWidthMm != nil {
		paperWidthMm = *req.PaperWidthMm
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO printers
		 (printer_id, merchant_id, name, connection_type, ip_address, port, bluetooth_address, role, language, production_product_ids, paper_width_mm)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		printerID, merchantID, req.Name, req.ConnectionType,
		toNullString(req.IPAddress), port,
		toNullString(req.BluetoothAddress), req.Role, language,
		productIDsToNullString(req.ProductionProductIDs), paperWidthMm,
	)
	if err != nil {
		log.Error(err.Error())
		if isUniqueConstraintError(err) {
			return nil, models.ErrInvalidInput
		}
		return nil, err
	}

	return r.GetPrinter(ctx, merchantID, printerID)
}

// UpdatePrinter applies a partial update to a printer owned by the merchant.
// When role changes, language is automatically recalculated.
func (r *Repository) UpdatePrinter(ctx context.Context, merchantID, printerID string, req *UpdatePrinterRequest) (*PrinterEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Verify ownership before mutating.
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM printers WHERE printer_id = ? AND merchant_id = ? AND enabled = 1`,
		printerID, merchantID,
	).Scan(&count); err != nil {
		log.Error(err.Error())
		return nil, err
	}
	if count == 0 {
		return nil, models.ErrForbidden
	}

	var updates []string
	var args []interface{}

	if req.Name != nil {
		updates = append(updates, "name = ?")
		args = append(args, *req.Name)
	}
	if req.ConnectionType != nil {
		updates = append(updates, "connection_type = ?")
		args = append(args, *req.ConnectionType)
	}
	if req.IPAddress != nil {
		updates = append(updates, "ip_address = ?")
		args = append(args, *req.IPAddress)
	}
	if req.Port != nil {
		updates = append(updates, "port = ?")
		args = append(args, *req.Port)
	}
	if req.BluetoothAddress != nil {
		updates = append(updates, "bluetooth_address = ?")
		args = append(args, *req.BluetoothAddress)
	}
	if req.Role != nil {
		updates = append(updates, "role = ?")
		args = append(args, *req.Role)
		// Recalculate language whenever role changes.
		updates = append(updates, "language = ?")
		args = append(args, languageForRole(*req.Role))
	}
	if req.ProductionProductIDs != nil {
		updates = append(updates, "production_product_ids = ?")
		args = append(args, productIDsToNullString(req.ProductionProductIDs))
	}
	if req.PaperWidthMm != nil {
		updates = append(updates, "paper_width_mm = ?")
		args = append(args, *req.PaperWidthMm)
	}

	if len(updates) > 0 {
		args = append(args, printerID, merchantID)
		query := `UPDATE printers SET ` + strings.Join(updates, ", ") + ` WHERE printer_id = ? AND merchant_id = ?`
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			log.Error(err.Error())
			if isUniqueConstraintError(err) {
				return nil, models.ErrInvalidInput
			}
			return nil, err
		}
	}

	return r.GetPrinter(ctx, merchantID, printerID)
}

// DeletePrinter soft-deletes a printer (enabled = 0) owned by the merchant.
func (r *Repository) DeletePrinter(ctx context.Context, merchantID, printerID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Verify ownership before mutating.
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM printers WHERE printer_id = ? AND merchant_id = ? AND enabled = 1`,
		printerID, merchantID,
	).Scan(&count); err != nil {
		log.Error(err.Error())
		return err
	}
	if count == 0 {
		return models.ErrForbidden
	}

	result, err := db.ExecContext(ctx,
		`UPDATE printers SET enabled = 0 WHERE printer_id = ? AND merchant_id = ?`,
		printerID, merchantID,
	)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Error(err.Error())
		return err
	}
	if rowsAffected == 0 {
		return models.ErrNotFound
	}

	return nil
}

func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// productIDsToNullString serializes a *[]string to JSON for storage.
// A nil pointer or an empty slice both result in a SQL NULL (no filter).
func productIDsToNullString(ids *[]string) sql.NullString {
	if ids == nil || len(*ids) == 0 {
		return sql.NullString{}
	}
	raw, err := json.Marshal(*ids)
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(raw), Valid: true}
}

func isUniqueConstraintError(err error) bool {
	if mysqlErr, ok := err.(*mysql.MySQLError); ok {
		return mysqlErr.Number == 1062
	}
	return false
}
