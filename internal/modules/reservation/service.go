package reservation

import (
	"context"
	"fmt"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/modules/bookings"
)

// ReservationService définit le contrat pour la logique métier
type ReservationService interface {
	GetOpenHours(ctx context.Context, qr string) OpenHoursResponse
	GetBookingAvailability(ctx context.Context, qr string, requestedDate string, partySize int) AvailabilityResponse
	CreateReservation(ctx context.Context, qr string, req BookingRequest) CreateBookingResponse
	GetReservation(ctx context.Context, qr string, bookingNumber string) CreateBookingResponse
	UpdateReservation(ctx context.Context, qr string, req BookingRequest) CreateBookingResponse
	CancelReservation(ctx context.Context, qr string, bookingNumber string) GenericResponse
}

type reservationService struct {
	repo       ReservationRepository
	bookingSvc *bookings.BookingsService
}

// NewReservationService instancie le service avec son repository
func NewReservationService(repo ReservationRepository, bookingSvc *bookings.BookingsService) ReservationService {
	return &reservationService{repo: repo, bookingSvc: bookingSvc}
}

func (s *reservationService) GetOpenHours(ctx context.Context, qr string) OpenHoursResponse {
	// 1. Récupération du marchand via le repository
	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil {
		return OpenHoursResponse{Status: "2", Error: fmt.Sprintf("Database error: %v", err)}
	}
	if merchant == nil {
		return OpenHoursResponse{Status: "-1", Error: "QR Code expired"}
	}

	// 2. Récupération des horaires bruts via le repository
	operationHours, err := s.repo.GetOperationHoursByQR(ctx, qr)
	if err != nil {
		return OpenHoursResponse{Status: "2", Error: fmt.Sprintf("Query error: %v", err)}
	}

	// 3. Logique de formatage (anciennement dans la boucle PHP)
	jsOpenDaysMap := make(map[int]bool)
	openHoursByDay := make(map[string]string)
	dayMap := map[int]string{
		1: "Lundi", 2: "Mardi", 3: "Mercredi", 4: "Jeudi",
		5: "Vendredi", 6: "Samedi", 7: "Dimanche",
	}

	for _, row := range operationHours {
		jsDay := row.DayOfWeek % 7
		jsOpenDaysMap[jsDay] = true

		dayName := dayMap[row.DayOfWeek]
		timeRange := formatTime(row.HourFrom) + " - " + formatTime(row.HourTo)

		if existing, ok := openHoursByDay[dayName]; ok {
			openHoursByDay[dayName] = existing + " / " + timeRange
		} else {
			openHoursByDay[dayName] = timeRange
		}
	}

	// 4. Finalisation de la map de la semaine
	fullWeekHours := make(map[string]string)
	for i := 1; i <= 7; i++ {
		dayName := dayMap[i]
		if hours, open := openHoursByDay[dayName]; open {
			fullWeekHours[dayName] = hours
		} else {
			fullWeekHours[dayName] = "Fermé"
		}
	}

	merchant.OpenHours = fullWeekHours

	var jsOpenDays []int
	for k := range jsOpenDaysMap {
		jsOpenDays = append(jsOpenDays, k)
	}

	return OpenHoursResponse{
		OpenDays:         jsOpenDays,
		MaximumPartySize: merchant.ReserveMaximumPartySize,
		Merchant:         merchant,
	}
}

// Utilitaire privé au service
func formatTime(dbTime string) string {
	if len(dbTime) >= 5 {
		return dbTime[:5]
	}
	return dbTime
}

func (s *reservationService) GetBookingAvailability(ctx context.Context, qr string, requestedDate string, partySize int) AvailabilityResponse {
	// 1. Params marchand
	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil || merchant == nil {
		return AvailabilityResponse{Status: "error", Error: "Restaurant not found"}
	}

	if merchant.ReserveMaximumPartySize < partySize {
		return AvailabilityResponse{Status: "maximum_party_size_reached", Error: "Maximum party size reached"}
	}

	// 2. Préparation de la date et jour de la semaine
	t, err := time.Parse("2006-01-02", requestedDate)
	if err != nil {
		return AvailabilityResponse{Status: "error", Error: "Invalid date format"}
	}
	dayOfWeek := int(t.Weekday())
	if dayOfWeek == 0 {
		dayOfWeek = 7
	} // Ajustement Dimanche pour ton format PHP (1-7)

	// 3. Récupération data
	ranges, err := s.repo.GetOperationRanges(ctx, merchant.MerchantID, dayOfWeek, requestedDate)
	if err != nil {
		return AvailabilityResponse{Status: "error_pdo", Error: err.Error()}
	}

	bookings, err := s.repo.GetBookedCapacity(ctx, merchant.MerchantID, requestedDate)
	if err != nil {
		return AvailabilityResponse{Status: "error_pdo", Error: err.Error()}
	}

	// 4. Calcul de l'heure actuelle chez le marchand
	loc, _ := time.LoadLocation(merchant.Timezone)
	nowMerchant := time.Now().In(loc)

	var allSlots []Slot

	for _, r := range ranges {
		// Parsing des bornes
		startService, _ := time.ParseInLocation("2006-01-02 15:04:05", requestedDate+" "+r.HourFrom, loc)
		endService, _ := time.ParseInLocation("2006-01-02 15:04:05", requestedDate+" "+r.HourTo, loc)

		firstBookable := startService
		if r.FirstBookingTime != nil && *r.FirstBookingTime != "" {
			firstBookable, _ = time.ParseInLocation("2006-01-02 15:04:05", requestedDate+" "+*r.FirstBookingTime, loc)
		}

		lastBookable := endService
		if r.LastBookingTime != nil && *r.LastBookingTime != "" {
			lastBookable, _ = time.ParseInLocation("2006-01-02 15:04:05", requestedDate+" "+*r.LastBookingTime, loc)
		}

		currentSlot := firstBookable
		interval := time.Duration(merchant.SlotIntervalMinutes) * time.Minute
		duration := time.Duration(merchant.DefaultBookingDuration) * time.Minute

		for !currentSlot.After(lastBookable) {
			isAvailable := true

			// Déjà passé ?
			if currentSlot.Before(nowMerchant) {
				isAvailable = false
			}

			if isAvailable {
				// Vérification capacité sur la fenêtre de durée
				maxBooked := 0
				endBookingWindow := currentSlot.Add(duration)

				tempSlot := currentSlot
				for tempSlot.Before(endBookingWindow) {
					slotKey := tempSlot.Format("15:04:05")
					if booked, ok := bookings[slotKey]; ok {
						if booked > maxBooked {
							maxBooked = booked
						}
					}
					tempSlot = tempSlot.Add(interval)
				}

				if (r.BookingCapacity - maxBooked) < partySize {
					isAvailable = false
				}
			}

			allSlots = append(allSlots, Slot{
				Time:      currentSlot.Format("H:i"),
				Available: isAvailable,
				HOOID:     r.ID,
			})

			currentSlot = currentSlot.Add(interval)
		}
	}

	return AvailabilityResponse{Slots: allSlots}
}

/* Version avec transaction manuelle (plus de contrôle, mais plus de code)
func (s *reservationService) CreateReservationOld(ctx context.Context, qr string, req BookingRequest) CreateBookingResponse {
	// 1. Début de transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CreateBookingResponse{Status: "-2", Error: err.Error()}
	}
	// Rollback automatique si on ne Commit pas
	defer tx.Rollback()

	// 2. Vérification Marchand
	merchant, err := s.repo.GetMerchantByQR(ctx, qr) // On peut réutiliser la méthode existante
	if err != nil || merchant == nil {
		return CreateBookingResponse{Status: "-1", Error: "QR Code expired"}
	}

	if merchant.ReserveMaximumPartySize < req.Booking.PartySize {
		return CreateBookingResponse{Status: "maximum_party_size_reached", Error: "Maximum party size reached"}
	}

	// 3. Préparation des dates et infos
	startTime, err := time.Parse("2006-01-02 15:04:05", req.Booking.StartDate)
	if err != nil {
		return CreateBookingResponse{Status: "-4", Error: "Invalid start date format"}
	}

	req.MerchantID = merchant.MerchantID
	req.Booking.MerchantID = merchant.MerchantID
	req.Booking.Status = "PENDING_APPROVAL"
	req.CreatedBy = "WR_ONLINE_BOOKING"

	// Calcul de la date de fin (équivalent setEndDateFromStartDateAndDuration)
	duration := time.Duration(merchant.DefaultBookingDuration) * time.Minute
	req.Booking.EndDate = startTime.Add(duration).Format("2006-01-02 15:04:05")

	// 4. Gestion du Client (équivalent getCustomerByPhoneNumber)
	req.Customer.MerchantID = merchant.MerchantID
	// On nettoie le téléphone (implémente ta logique de normalisation ici)
	req.Customer.CustomerTel = s.normalizePhone(req.Customer.CustomerTel)

	customer, err := s.repo.GetCustomerByPhone(ctx, tx, req.Customer.CustomerTel, merchant.MerchantID)
	if err == nil && customer != nil {
		req.Customer.CustomerID = customer.CustomerID
	}

	// 5. Création de la réservation
	bookingID, err := s.repo.CreateBooking(ctx, tx, &req)
	if err != nil {
		return CreateBookingResponse{Status: "-2", Error: "Insert failed: " + err.Error()}
	}
	req.Booking.BookingID = bookingID

	// 6. Auto-acceptation si activée
	if merchant.AutoAcceptReserveBookings {
		_, err := s.bookingSvc.AcceptBooking(ctx, "nil", bookingID)
		if err != nil {
			return CreateBookingResponse{Status: "-2", Error: "Auto-accept failed"}
		}
		req.Booking.Status = "ACCEPTED"
	}

	// 7. Validation finale
	if err := tx.Commit(); err != nil {
		return CreateBookingResponse{Status: "-2", Error: "Commit failed"}
	}

	return CreateBookingResponse{Status: "1", Booking: req.Booking}
}
*/

func (s *reservationService) CreateReservation(ctx context.Context, qr string, req BookingRequest) CreateBookingResponse {
	// 1. Vérification Marchand (Utilise la connexion, puis la libère)
	log := logger.FromContext(ctx)

	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil || merchant == nil {
		return CreateBookingResponse{Status: "-1", Error: "QR Code expired"}
	}

	if merchant.ReserveMaximumPartySize < req.Booking.PartySize {
		return CreateBookingResponse{Status: "maximum_party_size_reached", Error: "Maximum party size reached"}
	}

	// 2. Préparation des dates et infos métier
	startTime, err := time.Parse("2006-01-02 15:04:05", req.Booking.StartDate)
	if err != nil {
		return CreateBookingResponse{Status: "-4", Error: "Invalid start date format"}
	}

	req.MerchantID = merchant.MerchantID
	req.Booking.MerchantID = merchant.MerchantID
	req.Booking.Status = "PENDING_APPROVAL"
	req.CreatedBy = "WR_ONLINE_BOOKING"

	// Calcul de la date de fin
	duration := time.Duration(merchant.DefaultBookingDuration) * time.Minute
	req.Booking.EndDate = startTime.Add(duration).Format("2006-01-02 15:04:05")

	// 3. Nettoyage des données
	req.Customer.MerchantID = merchant.MerchantID
	req.Customer.CustomerTel = s.normalizePhone(req.Customer.CustomerTel)

	// 4. Délégation de la persistance transactionnelle au Repository
	// La transaction sera gérée de A à Z par cette méthode.
	bookingID, err := s.repo.CreateBookingTransaction(ctx, &req)
	if err != nil {
		return CreateBookingResponse{Status: "-2", Error: "Insert failed: " + err.Error()}
	}
	req.Booking.BookingID = bookingID

	// 5. Auto-acceptation si activée
	// À ce stade, la transaction de création est terminée et commitée. La connexion est libre !
	if merchant.AutoAcceptReserveBookings {
		_, err := s.bookingSvc.AcceptBooking(ctx, "nil", bookingID)
		if err != nil {
			log.Error(err.Error())
			return CreateBookingResponse{Status: "-2", Error: "Auto-accept failed"}
		}
		req.Booking.Status = "ACCEPTED"
	}

	return CreateBookingResponse{Status: "1", Booking: req.Booking}
}

func (s *reservationService) normalizePhone(phone string) string {
	// Implémente ici ta logique PHP normalizePhoneNumber
	return phone
}

const MaximumSequenceNumber = 3

func (s *reservationService) GetReservation(ctx context.Context, qr string, bookingNumber string) CreateBookingResponse {
	merchant, _ := s.repo.GetMerchantByQR(ctx, qr)
	if merchant == nil {
		return CreateBookingResponse{Status: "-1", Error: "QR Code expired"}
	}

	// 2. Récupération résa
	booking, err := s.repo.GetBookingByNumber(ctx, bookingNumber, merchant.MerchantID)
	if err != nil || booking == nil {
		return CreateBookingResponse{Status: "0", Error: "Booking not found"}
	}

	// 3. Logique de calcul du "Cancelable"
	loc, _ := time.LoadLocation(merchant.Timezone)
	now := time.Now().In(loc)

	// On parse la date de début de résa (MySQL format)
	layout := "2006-01-02 15:04:05"
	bookingDateFrom, _ := time.ParseInLocation(layout, booking.StartDate, loc)

	// Date limite = Date résa - X heures
	cancelLimit := bookingDateFrom.Add(time.Duration(-merchant.CancelBookingLimitOffsetHours) * time.Hour)

	// Vérification des 3 conditions PHP
	booking.Cancelable = merchant.CancelableByCustomer &&
		(now.Before(cancelLimit) || now.Equal(cancelLimit)) &&
		(booking.SequenceNumber < MaximumSequenceNumber)

	return CreateBookingResponse{Status: "1", Booking: booking}
}

func (s *reservationService) UpdateReservation(ctx context.Context, qr string, req BookingRequest) CreateBookingResponse {
	// 1. Vérifier si c'est encore modifiable (Pas de transaction globale = pas de deadlock)
	current := s.GetReservation(ctx, qr, req.Booking.BookingNumber)
	if current.Booking == nil || !current.Booking.Cancelable {
		return CreateBookingResponse{Status: "too_late_to_edit"}
	}

	// 2. Récupérer les paramètres du marchand
	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil || merchant == nil {
		return CreateBookingResponse{Status: "-1", Error: "QR Code expired or merchant not found"}
	}

	// 3. Préparation des dates
	startTime, err := time.Parse("2006-01-02 15:04:05", req.Booking.StartDate)
	if err != nil {
		return CreateBookingResponse{Status: "-4", Error: "Invalid start date format"}
	}

	duration := time.Duration(merchant.DefaultBookingDuration) * time.Minute

	req.Booking.BookingID = current.Booking.BookingID // On récupère l'ID interne
	req.Booking.EndDate = startTime.Add(duration).Format("2006-01-02 15:04:05")
	req.Booking.Status = "PENDING_APPROVAL"

	// 4. Auto-acceptation si activée
	if merchant.AutoAcceptReserveBookings {
		req.Booking.Status = "ACCEPTED"
	}

	// 5. Exécution de la mise à jour via le Repository
	err = s.repo.UpdateBooking(ctx, req.Booking)
	if err != nil {
		return CreateBookingResponse{Status: "-2", Error: err.Error()}
	}

	// 6. On retourne la réservation à jour
	return s.GetReservation(ctx, qr, req.Booking.BookingNumber)
}

func (s *reservationService) CancelReservation(ctx context.Context, qr string, bookingNumber string) GenericResponse {
	// 1. Vérifier si annulable
	current := s.GetReservation(ctx, qr, bookingNumber)
	if current.Booking == nil || !current.Booking.Cancelable {
		return GenericResponse{Status: "too_late_to_edit"}
	}

	// 2. Annulation via le service Booking existant
	// C'est ce service qui gérera ses propres accès DB ou transactions s'il en a besoin !
	_, err := s.bookingSvc.DenyBooking(ctx, "", bookingNumber)
	if err != nil {
		return GenericResponse{Status: "2", Error: err.Error()}
	}

	return GenericResponse{Status: "1"}
}
