package deliveroo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"welloresto-api/internal/config"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type DeliverooService struct {
	repo   *DeliverooRepository
	db     *sql.DB
	client *DeliverooClient
	cfg    config.DeliverooConfig
}

func NewDeliverooService(db *sql.DB, cfg config.DeliverooConfig) *DeliverooService {
	dc := NewDeliverooClient(nil, cfg)
	repo := NewDeliverooRepo(db)
	return &DeliverooService{repo: repo, db: db, client: dc, cfg: cfg}
}

func (s *DeliverooService) SetCollected(ctx context.Context, brandOrderID string) error {
	err := s.client.SetCollected(ctx, brandOrderID)
	if err != nil {
		return err
	}

	return nil
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

	// 3️⃣ Call Deliveroo API
	err = s.client.AcceptOrder(ctx, brandOrderID)
	if err != nil {
		return err
	}

	// 5️⃣ Non-2xx → return error with payload
	return nil
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

	err = s.client.SetCollected(ctx, brandOrderID)
	if err != nil {
		return nil, err
	}

	// Success
	return map[string]any{
		"status": "1",
	}, nil
}

func (s *DeliverooService) DenyOrder(ctx context.Context, orderID string, in models.DenyOrderRequest) error {
	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	err = s.client.DenyOrder(ctx, brandOrderID, in)
	if err != nil {
		return err
	}

	return nil
}

func (s *DeliverooService) FinishOrderIfDoesNotExist(ctx context.Context, brandOrderID string) {
	// Will be implemented later
}

func (s *DeliverooService) ReadyForCollection(ctx context.Context, orderID string) error {
	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	err = s.client.SetReadyForCollection(ctx, brandOrderID)
	if err != nil {
		return err
	}

	return nil
}

func (s *DeliverooService) CancelOrder(ctx context.Context, userID, orderID string, in models.DenyOrderRequest) error {

	brandOrderID, err := s.repo.GetBrandOrderID(ctx, orderID)
	if err != nil {
		return err
	}

	err = s.client.DenyOrder(ctx, brandOrderID, in)
	if err != nil {
		return err
	}

	return nil
}

// Dans deliveroo_service.go

// SetSyncStatus informe Deliveroo de l'issue du traitement du webhook
func (s *DeliverooService) SetSyncStatus(ctx context.Context, brandOrderID string, status string, reason string) error {
	notes := ""
	if status == "failed" {
		notes = "Failed to process webhook internally"
	}

	err := s.client.SendSyncStatus(ctx, brandOrderID, status, reason, notes)
	if err != nil {
		// On logue l'erreur métier ici
		logger.FromContext(ctx).Error("Failed to send sync_status to Deliveroo",
			zap.String("brand_order_id", brandOrderID),
			zap.Error(err))
		return err
	}

	return nil
}
