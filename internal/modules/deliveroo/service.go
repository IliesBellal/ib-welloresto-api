package deliveroo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

type DeliverooService struct {
	repo   *DeliverooRepository
	db     *sql.DB
	log    *zap.Logger
	client *DeliverooClient
}

func NewDeliverooService(repo *DeliverooRepository, db *sql.DB, log *zap.Logger) *DeliverooService {
	dc := NewDeliverooClient()
	return &DeliverooService{repo: repo, db: db, log: log, client: dc}
}

func (s *DeliverooService) AcceptOrder(ctx context.Context, merchantID string, orderID string) error {

	// 1️⃣ Load brand_order_id in DB
	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("deliveroo: brand_order_id missing")
		}
		return err
	}

	// 2️⃣ Get bearer token
	token, err := s.repo.GetBearerToken(ctx)
	if err != nil {
		return fmt.Errorf("deliveroo token error: %w", err)
	}

	// 3️⃣ Call Deliveroo API
	statusCode, payload, err := s.client.AcceptOrder(ctx, token, brandOrderID)
	if err != nil {
		return err
	}

	// 4️⃣ If OK → update DB
	if statusCode >= 200 && statusCode < 300 {
		if err := s.repo.UpdateAcceptedStatus(ctx, brandOrderID); err != nil {
			return err
		}
		return nil
	}

	// 5️⃣ Non-2xx → return error with payload
	return fmt.Errorf("deliveroo returned %d: %v", statusCode, payload)
}

func (s *DeliverooService) StartDeliverooDelivery(ctx context.Context, brandOrderID string) error {

	_, err := s.repo.MarkDeliverooDeliveryStarted(ctx, brandOrderID)
	if err != nil {
		return err
	}

	payload := map[string]string{
		"stage":       "collected",
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	}

	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST",
		"https://api.developers.deliveroo.com/order/v1/orders/"+brandOrderID+"/prep_stage",
		strings.NewReader(string(jsonBody)),
	)
	if err != nil {
		return err
	}

	token, err := s.repo.GetBearerToken(ctx)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	_, err = http.DefaultClient.Do(req)
	return err
}

func (s *DeliverooService) StartDelivery(ctx context.Context, brandOrderID string) (map[string]any, error) {

	// Retrieve internal IDs
	_, err := s.repo.MarkDeliverooDeliveryStarted(ctx, brandOrderID)
	if err != nil {
		return nil, err
	}

	// Build occurred_at timestamp
	occurredAt := time.Now().UTC().Format(time.RFC3339)

	bearer, err := s.repo.GetBearerToken(ctx)
	if err != nil {
		return nil, err
	}

	// Create "collected" stage
	payload := map[string]any{
		"stage":       "collected",
		"occurred_at": occurredAt,
	}

	resp, err := s.client.CreateStageForOrder(ctx, brandOrderID, bearer, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// If deliveroo says "not found"
	if resp.StatusCode == 404 {
		s.FinishOrderIfDoesNotExist(ctx, brandOrderID)
		return map[string]any{
			"status": "1",
			"error":  "Order does not exist anymore at Deliveroo",
		}, nil
	}

	var decoded map[string]any
	json.Unmarshal(body, &decoded)

	// Success
	return map[string]any{
		"status":  "1",
		"payload": decoded,
	}, nil
}

func (s *DeliverooService) DenyOrder(ctx context.Context, orderID, reasonID, reasonType, comment string) error {
	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	token, err := s.repo.GetBearerToken(ctx)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"status":        "rejected",
		"reject_reason": reasonType,
		"notes":         comment,
	}

	resp, err := s.client.PatchOrderStatus(ctx, brandOrderID, payload, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Update local status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
		// return s.repo.UpdateBrandStatusRejected(ctx, brandOrderID)
	}

	return fmt.Errorf("deliveroo rejected with %d", resp.StatusCode)
}

func (s *DeliverooService) UpdateOrderStatus(ctx context.Context, brandOrderID string, payload map[string]any) (map[string]any, error) {

	bearer, err := s.repo.GetBearerToken(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.UpdateOrderStatus(ctx, brandOrderID, bearer, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 404 {
		// Recursion retransmission as in PHP
		return s.UpdateOrderStatus(ctx, brandOrderID, map[string]any{
			"status":        "rejected",
			"reject_reason": "other",
			"notes":         "timed out",
		})
	}

	var decoded map[string]any
	json.Unmarshal(body, &decoded)

	return map[string]any{
		"status":  "1",
		"payload": decoded,
	}, nil
}

func (s *DeliverooService) FinishOrderIfDoesNotExist(ctx context.Context, brandOrderID string) {
	// Will be implemented later
}

func (s *DeliverooService) ReadyForCollection(ctx context.Context, orderID string) error {
	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	token, err := s.repo.GetBearerToken(ctx)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"stage":       "ready_for_collection",
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	}

	resp, err := s.client.CreateStage(ctx, brandOrderID, payload, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return s.repo.UpdateReadyForHandoffLocal(ctx, orderID) // ici
	}

	return fmt.Errorf("Deliveroo stage returned %d", resp.StatusCode)
}

func (s *DeliverooService) CancelOrder(ctx context.Context, userID, orderID, reasonID, comment string) error {

	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	token, err := s.repo.GetBearerToken(ctx)
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

	resp, err := s.client.CancelOrder(ctx, brandOrderID, token, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return s.repo.MarkOrderCanceledLocal(ctx, orderID) // ici
	}

	return fmt.Errorf("deliveroo cancel returned %d", resp.StatusCode)
}
