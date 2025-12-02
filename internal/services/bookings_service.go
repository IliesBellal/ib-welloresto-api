package services

import (
	"context"
	"errors"
	"fmt"
	"welloresto-api/internal/models"
	"welloresto-api/internal/repositories"
)

type BookingsService struct {
	repo     *repositories.BookingsRepository
	userRepo *repositories.UserRepository
}

func NewBookingsService(repo *repositories.BookingsRepository, _userRepo *repositories.UserRepository) *BookingsService {
	return &BookingsService{repo: repo, userRepo: _userRepo}
}

func (s *BookingsService) GetBookings(ctx context.Context, token string, req *models.BookingObjectRequest) ([]models.Booking, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	req.MerchantID = user.MerchantID

	return s.repo.GetBookings(ctx, req)
}

func (s *BookingsService) GetBookingByID(ctx context.Context, token, bookingID string) (*models.Booking, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	return s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
}

func (s *BookingsService) CreateBooking(ctx context.Context, token string, req *models.BookingObjectRequest) (*models.Booking, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	req.MerchantID = user.MerchantID

	// 1️⃣ Update or create customer
	params := map[string]interface{}{
		"merchant_id":    req.MerchantID,
		"customer_id":    req.Customer.CustomerID,
		"customer_name":  req.Customer.CustomerName,
		"customer_tel":   req.Customer.CustomerTel,
		"customer_email": req.Customer.CustomerEmail,
	}

	cs := NewCustomersService()

	customerRes, err := cs.UpdateOrCreateCustomer(ctx, params)
	if err != nil {
		return nil, err
	}

	if customerRes["status"] != "1" {
		return nil, fmt.Errorf("customer creation error")
	}

	customerID := fmt.Sprintf("%v", customerRes["customer_id"])

	// 2️⃣ Create booking
	bookingID, err := s.repo.CreateBooking(ctx, req, customerID)
	if err != nil {
		return nil, err
	}

	// 3️⃣ Reload booking using Fetcher (like PHP getBookings)
	result, err := s.repo.GetBookingByID(ctx, req.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	// 4️⃣ Email sending will be added later
	return result, nil
}

func (s *BookingsService) AcceptBooking(ctx context.Context, token, bookingID string) (map[string]interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	// 1️⃣ Update booking state
	err = s.repo.SetBookingState(ctx, bookingID, "ACCEPTED")
	if err != nil {
		return nil, err
	}

	// 2️⃣ Reload booking (fetchAndBuildBookings)
	booking, err := s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	// 3️⃣ Email pending — ignored for now

	return map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, nil
}

func (s *BookingsService) DenyBooking(ctx context.Context, token, bookingID string) (map[string]interface{}, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	err = s.repo.SetBookingState(ctx, bookingID, "DENIED")
	if err != nil {
		return nil, err
	}

	booking, err := s.repo.GetBookingByID(ctx, user.MerchantID, bookingID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, nil
}
