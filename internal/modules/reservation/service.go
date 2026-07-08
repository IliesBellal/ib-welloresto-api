package reservation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/modules/bookingcomm"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/modules/bookings"
	"welloresto-api/internal/modules/notification"
)

// ReservationService définit le contrat pour la logique métier
type ReservationService interface {
	GetOpenHours(ctx context.Context, qr string) OpenHoursResponse
	GetBookingAvailability(ctx context.Context, qr string, requestedDate string, partySize int) AvailabilityResponse
	CreateReservation(ctx context.Context, qr string, idempotencyKey string, req BookingRequest) PublicBookingResponse
	GetReservation(ctx context.Context, qr string, bookingNumber string) PublicBookingResponse
	UpdateReservation(ctx context.Context, qr string, req BookingRequest) PublicBookingResponse
	CancelReservation(ctx context.Context, qr string, bookingNumber string) GenericResponse
	JoinWaitlist(ctx context.Context, qr string, req WaitlistJoinRequest) WaitlistPublicResponse
	GetWaitlistStatus(ctx context.Context, qr string, token string) WaitlistPublicResponse
	LeaveWaitlist(ctx context.Context, qr string, token string) GenericResponse
}

type reservationService struct {
	repo       ReservationRepository
	bookingSvc *bookings.BookingsService
	redis      *redisclient.Client
	notifier   *notification.NotificationService
	comm       *bookingcomm.Service
}

// NewReservationService instancie le service avec son repository
func NewReservationService(repo ReservationRepository, bookingSvc *bookings.BookingsService, redis *redisclient.Client, notifier *notification.NotificationService, comm *bookingcomm.Service) ReservationService {
	return &reservationService{repo: repo, bookingSvc: bookingSvc, redis: redis, notifier: notifier, comm: comm}
}

// notifyPOS pousse un événement temps réel (WebSocket + FCM) vers le POS du
// marchand via le canal existant. No-op si le service n'est pas câblé.
func (s *reservationService) notifyPOS(merchantID, entityID, nType string) {
	if s.notifier == nil {
		return
	}
	_ = s.notifier.SendNotificationAsync(merchantID, entityID, nType)
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
		jsOpenDaysMap[row.DayOfWeek] = true

		dayName := dayMap[row.DayOfWeek]
		timeRange := formatTime(row.HourFrom) + " - " + formatTime(row.HourTo)

		if existing, ok := openHoursByDay[dayName]; ok {
			openHoursByDay[dayName] = existing + " / " + timeRange
		} else {
			openHoursByDay[dayName] = timeRange
		}
	}

	// Finalisation de la map de la semaine
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
	if merchant.ReserveMinimumPartySize > 0 && partySize < merchant.ReserveMinimumPartySize {
		return AvailabilityResponse{Status: "minimum_party_size_not_reached", Error: "Minimum party size not reached"}
	}

	loc, _ := time.LoadLocation(merchant.Timezone)
	requestedDateStr, err := normalizeRequestedDateInput(requestedDate, loc)
	if err != nil {
		return AvailabilityResponse{Status: "error", Error: "Invalid requested date"}
	}

	computed, err := s.buildComputedAvailability(ctx, merchant, requestedDateStr, partySize, "")
	if err != nil {
		return AvailabilityResponse{Status: "error_pdo", Error: err.Error()}
	}

	allSlots := make([]Slot, 0, len(computed))
	for _, slot := range computed {
		timeFrom, _ := time.ParseInLocation("2006-01-02 15:04:05", slot.DateFrom, loc)
		allSlots = append(allSlots, Slot{
			Time:            timeFrom.Format("15:04"),
			Available:       slot.Available,
			DurationMinutes: slot.DurationMinutes,
			HOOID:           fmt.Sprintf("%d", slot.HourOfOperationID),
		})
	}

	return AvailabilityResponse{Slots: allSlots}
}

func (s *reservationService) CreateReservation(ctx context.Context, qr string, idempotencyKey string, req BookingRequest) PublicBookingResponse {
	if req.Booking == nil || req.Customer == nil {
		return PublicBookingResponse{Status: "-4", Error: "Invalid booking payload"}
	}

	// 1. Vérification Marchand (Utilise la connexion, puis la libère)

	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil || merchant == nil {
		return PublicBookingResponse{Status: "-1", Error: "QR Code expired"}
	}

	if req.Booking.PartySize < merchant.ReserveMinimumPartySize {
		return PublicBookingResponse{Status: "minimum_party_size_not_reached", Error: "Minimum party size not reached"}
	}
	if merchant.ReserveMaximumPartySize < req.Booking.PartySize {
		return PublicBookingResponse{Status: "maximum_party_size_reached", Error: "Maximum party size reached"}
	}

	if replay, ok := s.tryIdempotentReplay(ctx, qr, idempotencyKey, merchant); ok {
		return replay
	}

	loc, _ := time.LoadLocation(merchant.Timezone)
	startTime, err := time.ParseInLocation("2006-01-02 15:04:05", req.Booking.StartDate, loc)
	if err != nil {
		return PublicBookingResponse{Status: "-4", Error: "Invalid start date format"}
	}
	requestedDateStr := bookingcore.NormalizeRequestedDate(startTime)

	computed, err := s.buildComputedAvailability(ctx, merchant, requestedDateStr, req.Booking.PartySize, "")
	if err != nil {
		return PublicBookingResponse{Status: "-2", Error: err.Error()}
	}
	available, durationMinutes := s.findMatchingSlot(computed, startTime)
	if !available {
		return PublicBookingResponse{Status: "slot_unavailable", Error: "Slot unavailable"}
	}

	req.MerchantID = merchant.MerchantID
	req.Booking.MerchantID = merchant.MerchantID
	req.Booking.Status = bookingcore.StatusPending
	if merchant.AutoAcceptReserveBookings {
		req.Booking.Status = bookingcore.StatusConfirmed
	}
	req.CreatedBy = "WR_ONLINE_BOOKING"

	req.Booking.DurationMinutes = durationMinutes
	req.Booking.EndDate = startTime.Add(time.Duration(durationMinutes) * time.Minute).UTC().Format("2006-01-02 15:04:05")
	req.Booking.StartDate = startTime.UTC().Format("2006-01-02 15:04:05")

	// 3. Nettoyage des données
	req.Customer.MerchantID = merchant.MerchantID
	req.Customer.CustomerID = "" // ignore toute valeur fournie par le client public
	req.Customer.CustomerTel = s.normalizePhone(req.Customer.CustomerTel)
	warning, err := s.repo.FindExistingActiveBookingWarning(ctx, merchant.MerchantID, req.Customer.CustomerTel, req.Booking.StartDate)
	if err != nil {
		return PublicBookingResponse{Status: "-2", Error: err.Error()}
	}

	// 4. Délégation de la persistance transactionnelle au Repository
	// La transaction sera gérée de A à Z par cette méthode.
	bookingID, err := s.repo.CreateBookingTransaction(ctx, &req)
	if err != nil {
		s.clearPendingIdempotency(ctx, qr, idempotencyKey)
		return PublicBookingResponse{Status: "-2", Error: "Insert failed: " + err.Error()}
	}
	req.Booking.BookingID = bookingID
	stored, err := s.repo.GetBookingByNumber(ctx, req.Booking.BookingNumber, merchant.MerchantID)
	if err != nil || stored == nil {
		s.clearPendingIdempotency(ctx, qr, idempotencyKey)
		return PublicBookingResponse{Status: "-2", Error: "Unable to load created booking"}
	}

	response := PublicBookingResponse{Status: "1", Booking: s.toBookingPublic(merchant, stored)}
	if warning {
		response.Warning = "possible_duplicate_same_phone_same_slot"
	}
	s.saveIdempotencyResult(ctx, qr, idempotencyKey, req.Booking.BookingNumber)

	// Notification temps réel POS : nouvelle demande de réservation publique.
	s.notifyPOS(merchant.MerchantID, stored.BookingID, notification.NotificationTypeNewBooking)

	// Confirmation immédiate au client si la réservation est auto-acceptée.
	if s.comm != nil && merchant.AutoAcceptReserveBookings {
		s.comm.SendConfirmation(ctx, bookingcomm.BookingMessage{
			MerchantSlug:  qr,
			MerchantName:  merchant.BusinessName,
			CustomerName:  req.Customer.CustomerName,
			CustomerEmail: req.Customer.CustomerEmail,
			CustomerPhone: req.Customer.CustomerTel,
			BookingNumber: req.Booking.BookingNumber,
			DateLabel:     bookingcore.FormatDateLabelFR(startTime),
			TimeLabel:     startTime.Format("15:04"),
			PartySize:     req.Booking.PartySize,
			SMSEnabled:    merchant.SMSEnabled,
		})
	}

	return response
}

func (s *reservationService) normalizePhone(phone string) string {
	// Implémente ici ta logique PHP normalizePhoneNumber
	return phone
}

func mustAtoi(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

const MaximumSequenceNumber = 3

func (s *reservationService) GetReservation(ctx context.Context, qr string, bookingNumber string) PublicBookingResponse {
	merchant, _ := s.repo.GetMerchantByQR(ctx, qr)
	if merchant == nil {
		return PublicBookingResponse{Status: "-1", Error: "QR Code expired"}
	}

	// 2. Récupération résa
	booking, err := s.repo.GetBookingByNumber(ctx, bookingNumber, merchant.MerchantID)
	if err != nil || booking == nil {
		return PublicBookingResponse{Status: "0", Error: "Booking not found"}
	}

	// 3. Logique de calcul du "Cancelable"
	loc, _ := time.LoadLocation(merchant.Timezone)
	now := time.Now().In(loc)

	// On parse la date de début de résa (MySQL format)
	layout := "2006-01-02 15:04:05"
	bookingDateFromUTC, _ := time.Parse(layout, booking.StartDate)
	bookingDateFrom := bookingDateFromUTC.In(loc)

	// Date limite = Date résa - X heures
	cancelLimit := bookingDateFrom.Add(time.Duration(-merchant.CancelBookingLimitOffsetHours) * time.Hour)

	// Vérification des 3 conditions PHP
	booking.Cancelable = merchant.CancelableByCustomer &&
		(now.Before(cancelLimit) || now.Equal(cancelLimit)) &&
		(booking.SequenceNumber < MaximumSequenceNumber)

	return PublicBookingResponse{Status: "1", Booking: s.toBookingPublic(merchant, booking)}
}

func (s *reservationService) UpdateReservation(ctx context.Context, qr string, req BookingRequest) PublicBookingResponse {
	if req.Booking == nil {
		return PublicBookingResponse{Status: "-4", Error: "Invalid booking payload"}
	}

	// 1. Vérifier si c'est encore modifiable (Pas de transaction globale = pas de deadlock)
	current := s.GetReservation(ctx, qr, req.Booking.BookingNumber)
	if current.Booking == nil || !current.Booking.Cancelable {
		return PublicBookingResponse{Status: "too_late_to_edit"}
	}

	// 2. Récupérer les paramètres du marchand
	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil || merchant == nil {
		return PublicBookingResponse{Status: "-1", Error: "QR Code expired or merchant not found"}
	}
	stored, err := s.repo.GetBookingByNumber(ctx, req.Booking.BookingNumber, merchant.MerchantID)
	if err != nil || stored == nil {
		return PublicBookingResponse{Status: "0", Error: "Booking not found"}
	}

	// 3. Préparation des dates
	loc, _ := time.LoadLocation(merchant.Timezone)
	startTime, err := time.ParseInLocation("2006-01-02 15:04:05", req.Booking.StartDate, loc)
	if err != nil {
		return PublicBookingResponse{Status: "-4", Error: "Invalid start date format"}
	}
	requestedDateStr := bookingcore.NormalizeRequestedDate(startTime)
	computed, err := s.buildComputedAvailability(ctx, merchant, requestedDateStr, req.Booking.PartySize, req.Booking.BookingNumber)
	if err != nil {
		return PublicBookingResponse{Status: "-2", Error: err.Error()}
	}
	available, durationMinutes := s.findMatchingSlot(computed, startTime)
	if !available {
		return PublicBookingResponse{Status: "slot_unavailable", Error: "Slot unavailable"}
	}

	req.Booking.BookingID = stored.BookingID
	req.Booking.DurationMinutes = durationMinutes
	req.Booking.StartDate = startTime.UTC().Format("2006-01-02 15:04:05")
	req.Booking.EndDate = startTime.Add(time.Duration(durationMinutes) * time.Minute).UTC().Format("2006-01-02 15:04:05")
	req.Booking.Status = bookingcore.StatusPending

	// 4. Auto-acceptation si activée
	if merchant.AutoAcceptReserveBookings {
		req.Booking.Status = bookingcore.StatusConfirmed
	}

	// 5. Exécution de la mise à jour via le Repository
	err = s.repo.UpdateBooking(ctx, req.Booking)
	if err != nil {
		return PublicBookingResponse{Status: "-2", Error: err.Error()}
	}

	// Notification temps réel POS : modification d'une réservation par le client.
	s.notifyPOS(merchant.MerchantID, stored.BookingID, notification.NotificationTypeUpdateBooking)

	// Notification modification au client (coordonnées relues en base — le
	// corps de la requête publique ne contient pas forcément le customer).
	if s.comm != nil {
		name, email, phone, cerr := s.repo.GetBookingCustomerContact(ctx, req.Booking.BookingNumber, merchant.MerchantID)
		if cerr == nil {
			s.comm.SendModification(ctx, bookingcomm.BookingMessage{
				MerchantSlug:  qr,
				MerchantName:  merchant.BusinessName,
				CustomerName:  name,
				CustomerEmail: email,
				CustomerPhone: phone,
				BookingNumber: req.Booking.BookingNumber,
				DateLabel:     bookingcore.FormatDateLabelFR(startTime),
				TimeLabel:     startTime.Format("15:04"),
				PartySize:     req.Booking.PartySize,
				SMSEnabled:    merchant.SMSEnabled,
			})
		}
	}

	// 6. On retourne la réservation à jour
	return s.GetReservation(ctx, qr, req.Booking.BookingNumber)
}

func (s *reservationService) CancelReservation(ctx context.Context, qr string, bookingNumber string) GenericResponse {
	// 1. Vérifier si annulable
	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil || merchant == nil {
		return GenericResponse{Status: "-1", Error: "QR Code expired"}
	}

	current := s.GetReservation(ctx, qr, bookingNumber)
	if current.Booking == nil || !current.Booking.Cancelable {
		return GenericResponse{Status: "too_late_to_edit"}
	}

	err = s.repo.CancelBookingPublic(ctx, merchant.MerchantID, bookingNumber)
	if err != nil {
		return GenericResponse{Status: "2", Error: err.Error()}
	}

	// Notification annulation au client.
	if s.comm != nil {
		name, email, phone, cerr := s.repo.GetBookingCustomerContact(ctx, bookingNumber, merchant.MerchantID)
		if cerr == nil {
			loc, _ := time.LoadLocation(merchant.Timezone)
			if loc == nil {
				loc = time.UTC
			}
			dateLabel, timeLabel := "", ""
			if startTime, perr := time.Parse(time.RFC3339, current.Booking.DateFrom); perr == nil {
				startTime = startTime.In(loc)
				dateLabel = bookingcore.FormatDateLabelFR(startTime)
				timeLabel = startTime.Format("15:04")
			}
			s.comm.SendCancellation(ctx, bookingcomm.BookingMessage{
				MerchantSlug:  qr,
				MerchantName:  merchant.BusinessName,
				CustomerName:  name,
				CustomerEmail: email,
				CustomerPhone: phone,
				BookingNumber: bookingNumber,
				DateLabel:     dateLabel,
				TimeLabel:     timeLabel,
				PartySize:     current.Booking.PartySize,
				SMSEnabled:    merchant.SMSEnabled,
			})
		}
	}

	return GenericResponse{Status: "1"}
}

func normalizeRequestedDateInput(raw string, loc *time.Location) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty requested date")
	}

	if unix, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return bookingcore.NormalizeRequestedDate(time.Unix(unix, 0).In(loc)), nil
	}

	t, err := time.ParseInLocation("2006-01-02", trimmed, loc)
	if err != nil {
		return "", err
	}

	return bookingcore.NormalizeRequestedDate(t), nil
}

func (s *reservationService) buildComputedAvailability(ctx context.Context, merchant *Merchant, requestedDate string, partySize int, excludeBookingNumber string) ([]bookingcore.ComputedSlot, error) {
	loc, _ := time.LoadLocation(merchant.Timezone)
	requestedDateTime, err := time.ParseInLocation("2006-01-02", requestedDate, loc)
	if err != nil {
		return nil, err
	}
	dayOfWeek := int(requestedDateTime.Weekday())
	if dayOfWeek == 0 {
		dayOfWeek = 7
	}

	ranges, err := s.repo.GetOperationRanges(ctx, merchant.MerchantID, dayOfWeek, requestedDate)
	if err != nil {
		return nil, err
	}

	var bookings []bookingcore.IntervalBooking
	if excludeBookingNumber == "" {
		bookings, err = s.repo.GetBookedCapacity(ctx, merchant.MerchantID, requestedDate)
	} else {
		bookings, err = s.repo.GetBookedCapacityExcludingBooking(ctx, merchant.MerchantID, requestedDate, excludeBookingNumber)
	}
	if err != nil {
		return nil, err
	}

	durationRules, err := s.repo.GetBookingDurationRules(ctx, merchant.MerchantID)
	if err != nil {
		return nil, err
	}

	occupation := bookingcore.BuildOccupationByInterval(bookings, merchant.SlotIntervalMinutes, NewBookingSettingsFromMerchant(merchant), durationRules)
	rawRanges := make([]bookingcore.SlotRange, 0, len(ranges))
	for _, r := range ranges {
		rawRanges = append(rawRanges, bookingcore.SlotRange{
			ID:               mustAtoi(r.ID),
			HourFrom:         r.HourFrom,
			HourTo:           r.HourTo,
			BookingCapacity:  r.BookingCapacity,
			FirstBookingTime: r.FirstBookingTime,
			LastBookingTime:  r.LastBookingTime,
		})
	}

	return bookingcore.ComputeSlots(
		bookingcore.SlotParams{
			RequestedDate:   requestedDate,
			PartySize:       partySize,
			BookingSettings: NewBookingSettingsFromMerchant(merchant),
			DurationRules:   durationRules,
		},
		rawRanges,
		occupation,
		time.Now().In(loc),
	), nil
}

func (s *reservationService) findMatchingSlot(slots []bookingcore.ComputedSlot, startTime time.Time) (bool, int) {
	target := startTime.Format("2006-01-02 15:04:05")
	for _, slot := range slots {
		if slot.DateFrom == target {
			return slot.Available, slot.DurationMinutes
		}
	}
	return false, 0
}

func (s *reservationService) toBookingPublic(merchant *Merchant, booking *BookingData) *BookingPublic {
	loc, _ := time.LoadLocation(merchant.Timezone)
	startDate := booking.StartDate
	if parsed, err := time.Parse("2006-01-02 15:04:05", booking.StartDate); err == nil {
		startDate = parsed.In(loc).Format(time.RFC3339)
	}

	duration := booking.DurationMinutes
	if duration == 0 {
		start, startErr := time.Parse("2006-01-02 15:04:05", booking.StartDate)
		end, endErr := time.Parse("2006-01-02 15:04:05", booking.EndDate)
		if startErr == nil && endErr == nil {
			duration = int(end.Sub(start).Minutes())
		}
	}

	remainingUpdates := MaximumSequenceNumber - booking.SequenceNumber
	if remainingUpdates < 0 {
		remainingUpdates = 0
	}

	return &BookingPublic{
		BookingNumber:    booking.BookingNumber,
		Status:           booking.Status,
		PartySize:        booking.PartySize,
		DateFrom:         startDate,
		DurationMinutes:  duration,
		Comment:          booking.Comment,
		Cancelable:       booking.Cancelable,
		Modifiable:       booking.Cancelable,
		RemainingUpdates: remainingUpdates,
		Merchant: MerchantPublic{
			BusinessName: merchant.BusinessName,
			Phone:        merchant.Phone,
			Address:      merchant.Address,
			LogoURL:      merchant.LogoURL,
			Design:       merchant.Design,
			Timezone:     merchant.Timezone,
		},
	}
}

func (s *reservationService) tryIdempotentReplay(ctx context.Context, slug, idempotencyKey string, merchant *Merchant) (PublicBookingResponse, bool) {
	if s.redis == nil || strings.TrimSpace(idempotencyKey) == "" {
		return PublicBookingResponse{}, false
	}

	key := "idem:" + slug + ":" + strings.TrimSpace(idempotencyKey)
	if bookingNumber, found := s.redis.Get(ctx, key); found && bookingNumber != "" && bookingNumber != "__pending__" {
		stored, err := s.repo.GetBookingByNumber(ctx, bookingNumber, merchant.MerchantID)
		if err == nil && stored != nil {
			return PublicBookingResponse{Status: "1", Booking: s.toBookingPublic(merchant, stored)}, true
		}
	}

	if !s.redis.SetNX(ctx, key, "__pending__", 15*time.Minute) {
		if bookingNumber, found := s.redis.Get(ctx, key); found && bookingNumber != "" && bookingNumber != "__pending__" {
			stored, err := s.repo.GetBookingByNumber(ctx, bookingNumber, merchant.MerchantID)
			if err == nil && stored != nil {
				return PublicBookingResponse{Status: "1", Booking: s.toBookingPublic(merchant, stored)}, true
			}
		}
	}

	return PublicBookingResponse{}, false
}

func (s *reservationService) saveIdempotencyResult(ctx context.Context, slug, idempotencyKey, bookingNumber string) {
	if s.redis == nil || strings.TrimSpace(idempotencyKey) == "" || bookingNumber == "" {
		return
	}

	s.redis.Set(ctx, "idem:"+slug+":"+strings.TrimSpace(idempotencyKey), bookingNumber, 15*time.Minute)
}

func (s *reservationService) clearPendingIdempotency(ctx context.Context, slug, idempotencyKey string) {
	if s.redis == nil || strings.TrimSpace(idempotencyKey) == "" {
		return
	}

	s.redis.Delete(ctx, "idem:"+slug+":"+strings.TrimSpace(idempotencyKey))
}
