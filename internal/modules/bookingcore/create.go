package bookingcore

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/utils"
	"welloresto-api/internal/utils/dbutils"
)

// CustomerUpsert regroupe les champs client nécessaires à la création d'une
// réservation, communs aux flux public (/rsv) et staff (/bookings).
type CustomerUpsert struct {
	CustomerID    *string
	CustomerName  *string
	CustomerTel   *string
	CustomerEmail *string
	// Brand n'est renseigné que par les flux qui veulent taguer les nouveaux
	// clients (le flux public ne le fait pas).
	Brand *string
}

// CreateBookingParams réunit les champs nécessaires à l'insertion d'une
// réservation, communs aux flux public et staff. Les particularités de
// chaque flux (source, créateur, statut déjà résolu par l'appelant) restent
// à sa charge.
type CreateBookingParams struct {
	MerchantID string
	Source     string
	CreatedBy  string
	Status     string // déjà normalisé par l'appelant
	PartySize  int
	Comment    *string
	StartLocal time.Time // heure locale marchand
	EndLocal   time.Time // heure locale marchand
	Customer   CustomerUpsert
}

// CreateBooking effectue l'upsert client, génère un booking_number unique
// (scope marchand) et insère la réservation, le tout dans une transaction
// SQL réelle : si l'appelant a déjà ouvert une transaction via
// dbutils.RunInTx, celle-ci est réutilisée ; sinon une nouvelle est ouverte
// pour la durée de l'appel.
func CreateBooking(ctx context.Context, db *sql.DB, customerRepo *customers.CustomersRepository, p CreateBookingParams) (bookingID string, bookingNumber string, err error) {
	err = dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
		if p.EndLocal.Before(p.StartLocal) {
			return fmt.Errorf("start date is after end date")
		}

		customer := &models.Customer{
			MerchantID:    p.MerchantID,
			CustomerID:    p.Customer.CustomerID,
			CustomerName:  p.Customer.CustomerName,
			CustomerTel:   p.Customer.CustomerTel,
			CustomerEmail: p.Customer.CustomerEmail,
			CustomerBrand: p.Customer.Brand,
		}

		customerID, err := customerRepo.UpdateOrCreateCustomer(txCtx, customer)
		if err != nil {
			return fmt.Errorf("upsert customer: %w", err)
		}
		if customerID == nil || *customerID == "" {
			return fmt.Errorf("customer_upsert_failed")
		}

		number, err := generateUniqueBookingNumber(txCtx, db, p.MerchantID)
		if err != nil {
			return fmt.Errorf("generate booking number: %w", err)
		}

		duration := int(p.EndLocal.Sub(p.StartLocal).Minutes())
		startUTC := p.StartLocal.UTC().Format("2006-01-02 15:04:05")
		endUTC := p.EndLocal.UTC().Format("2006-01-02 15:04:05")

		conn := dbutils.GetDB(txCtx, db)
		res, err := conn.ExecContext(txCtx, `
			INSERT INTO bookings (
				booking_number, status, source, merchant_id, party_size,
				customer_id, comment, creation_date, booking_date_from,
				booking_date_to, booking_duration, created_by
			) VALUES (?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, ?, ?, ?, ?)`,
			number,
			p.Status,
			p.Source,
			p.MerchantID,
			p.PartySize,
			*customerID,
			p.Comment,
			startUTC,
			endUTC,
			duration,
			p.CreatedBy,
		)
		if err != nil {
			return err
		}

		id, err := res.LastInsertId()
		if err != nil {
			return err
		}

		bookingID = strconv.FormatInt(id, 10)
		bookingNumber = number
		return nil
	})

	return bookingID, bookingNumber, err
}

// generateUniqueBookingNumber tire un code aléatoire (majuscule) et vérifie
// son unicité au sein du marchand — GetBookingByNumber est toujours appelé
// scoped merchant_id, l'unicité n'a donc besoin d'être garantie qu'à ce
// niveau, pas globalement tous marchands confondus.
func generateUniqueBookingNumber(ctx context.Context, db *sql.DB, merchantID string) (string, error) {
	conn := dbutils.GetDB(ctx, db)

	for {
		number := strings.ToUpper(utils.GenerateRandomString(6))

		var exists string
		err := conn.QueryRowContext(ctx,
			`SELECT booking_id FROM bookings WHERE merchant_id = ? AND booking_number = ?`,
			merchantID, number,
		).Scan(&exists)

		if err == sql.ErrNoRows {
			return number, nil
		}
		if err != nil {
			return "", err
		}
	}
}
