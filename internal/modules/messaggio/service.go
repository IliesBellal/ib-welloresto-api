package messaggio

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type SMSService interface {
	SendOrderTrackingSMS(ctx context.Context, merchantID int64, orderID string, customerPhone string) error
}

type smsService struct {
	repo   MarketingRepository
	client MessaggioClient
}

func NewSMSService(
	repo MarketingRepository,
	client MessaggioClient,
) SMSService {
	return &smsService{
		repo:   repo,
		client: client,
	}
}

func normalizePhone(phone string) (string, error) {

	re := regexp.MustCompile(`[^0-9+]`)
	normalized := re.ReplaceAllString(phone, "")

	if strings.HasPrefix(normalized, "+") {
		normalized = normalized[1:]
	}

	valid := regexp.MustCompile(`^[0-9]{8,15}$`)
	if !valid.MatchString(normalized) {
		return "", fmt.Errorf("invalid phone")
	}

	return normalized, nil
}

func (s *smsService) SendOrderTrackingSMS(
	ctx context.Context,
	merchantID int64,
	orderID string,
	customerPhone string,
) error {

	settings, err := s.repo.GetMarketingSettings(ctx, merchantID)
	if err != nil || !settings.SMSEnabled {
		return nil
	}

	phone, err := normalizePhone(customerPhone)
	if err != nil {
		return err
	}

	trackingURL := fmt.Sprintf(
		"https://scannorder.welloresto.fr/restaurant/%s/%s",
		settings.QRCode,
		orderID,
	)

	message := strings.ReplaceAll(
		settings.TrackingTemplate,
		"{tracking_url}",
		trackingURL,
	)

	message = strings.ReplaceAll(message, "{order_id}", orderID)

	err = s.client.SendSMS(
		ctx,
		settings.MessaggioLogin,
		settings.MessaggioFrom,
		phone,
		message,
	)
	if err != nil {
		return err
	}

	return s.repo.RecordSMSCost(ctx, merchantID, 1, settings.SMSUnitPrice)
}