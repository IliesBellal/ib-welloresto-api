package ubereats

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type UberEatsService struct {
	repo       *UberEatsRepository
	db         *sql.DB
	log        *zap.Logger
	httpClient *UberEatsClient
}

func NewUberEatsService(repo *UberEatsRepository, db *sql.DB, log *zap.Logger) *UberEatsService {
	uec := NewUberEatsClient()
	return &UberEatsService{
		repo: repo, db: db, log: log, httpClient: uec,
	}
}

func (s *UberEatsService) AcceptOrder(ctx context.Context, merchantID, orderID string) error {

	// 1) Store info
	store, err := s.repo.GetStore(ctx, merchantID)
	if err != nil {
		return fmt.Errorf("get store: %w", err)
	}

	// 2) order info
	uberOrderID, creationDate, err := s.repo.GetOrderInfo(ctx, orderID)
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	if !creationDate.Valid {
		return fmt.Errorf("missing creation date")
	}

	// 3) Preparation time
	estMinutes := 0
	if store.EstimatedPreparationTime == "" || store.EstimatedPreparationTime == "AUTO" {

		count, err := s.repo.CountOrderItems(ctx, orderID)
		if err != nil {
			return fmt.Errorf("count items: %w", err)
		}

		avg, err := s.repo.GetAverageDistributionTime(ctx, merchantID, count)
		if err == nil {
			estMinutes = int(float64(avg) / 60.0 * 0.7)
		}
	} else {
		var tmp int
		fmt.Sscan(store.EstimatedPreparationTime, &tmp)
		estMinutes = tmp
	}

	if estMinutes < 5 {
		estMinutes = 5
	}
	if estMinutes > 59 {
		estMinutes = 59
	}

	// 4) Pickup time
	loc, err := time.LoadLocation(store.Timezone)
	if err != nil {
		loc = time.UTC
	}
	pickupAt := time.Now().In(loc).Add(time.Duration(estMinutes) * time.Minute)
	readyForPickup := pickupAt.UTC().Format("2006-01-02T15:04:05Z")

	payload := map[string]interface{}{
		"ready_for_pickup_time": readyForPickup,
		"external_id":           orderID,
		"accepted_by":           merchantID,
	}

	// 5) Send to Uber
	resp, err := s.httpClient.AcceptOrder(ctx, store.BearerToken, uberOrderID, payload)
	if err != nil {
		_ = s.httpClient.FinishOrderIfDoesNotExist(ctx, store.BearerToken, uberOrderID)
		return fmt.Errorf("uber request failed: %w", err)
	}
	defer resp.Body.Close()

	// 6) Success?
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := s.repo.UpdateOrderAccepted(ctx, orderID, estMinutes); err != nil {
			return err
		}
		return nil
	}

	// fallback
	_ = s.httpClient.FinishOrderIfDoesNotExist(ctx, store.BearerToken, uberOrderID)
	return fmt.Errorf("uber returned status %d", resp.StatusCode)
}

func (s *UberEatsService) SetOrderStarted(ctx context.Context, merchantID string, brandOrderID string) error {

	store, err := s.repo.GetStore(ctx, merchantID)
	if err != nil {
		return err
	}

	body := map[string]string{
		"status": "started",
	}

	data, _ := json.Marshal(body)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.uber.com/v1/eats/orders/%s/restaurantdelivery/status", brandOrderID),
		bytes.NewBuffer(data),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+store.BearerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (s *UberEatsService) finishOrderIfDoesNotExist(ctx context.Context, bearerToken, uberOrderID string) error {
	// TODO: implement logic later; for now noop (per your request)
	return nil
}

func (s *UberEatsService) DenyOrder(ctx context.Context, merchantID, orderID, reasonID, reasonType, comment string) error {
	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	token, err := s.repo.GetUberBearerToken(ctx)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"deny_reason": map[string]interface{}{
			"info":        reasonType,
			"type":        reasonType,
			"client_code": reasonID,
		},
	}

	body, _ := json.Marshal(payload)

	resp, err := s.httpClient.DenyOrder(ctx, brandOrderID, token, string(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return s.repo.SetOrderBrandDenied(ctx, orderID) // ici
	}

	return fmt.Errorf("ubereats denied with %d", resp.StatusCode)
}

func (s *UberEatsService) ReadyForHandoff(ctx context.Context, orderID string) error {
	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	token, err := s.repo.GetUberBearerToken(ctx)
	if err != nil {
		return err
	}

	resp, err := s.httpClient.PostReady(ctx, brandOrderID, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return s.repo.UpdateReadyForHandoffLocal(ctx, orderID)
	}

	return fmt.Errorf("Uber Eats ready returned %d", resp.StatusCode)
}

func (s *UberEatsService) CancelOrder(
	ctx context.Context,
	merchantID string,
	orderID string,
	reasonID string,
	comment string,
) error {

	brandOrderID, err := s.repo.GetBrandOrderForCancel(ctx, orderID)
	if err != nil {
		return err
	}

	token, err := s.repo.GetUberBearerToken(ctx)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"deny_reason": map[string]interface{}{
			"info":        comment,
			"type":        reasonID,
			"client_code": reasonID,
		},
	}

	body, _ := json.Marshal(payload)

	resp, err := s.httpClient.CancelOrder(ctx, brandOrderID, token, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return s.repo.MarkOrderCanceledLocal(ctx, orderID) // ici
	}

	return fmt.Errorf("uber cancel returned %d", resp.StatusCode)
}
