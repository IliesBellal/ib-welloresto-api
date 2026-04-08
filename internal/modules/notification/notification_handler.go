package notification

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type NotificationHandler struct {
	service *NotificationService
}

func NewNotificationHandler(service *NotificationService) *NotificationHandler {
	return &NotificationHandler{
		service: service,
	}
}

// SendTestNotificationRequest represents the payload for sending a test notification
type SendTestNotificationRequest struct {
	OrderID    string `json:"order_id"`
	MerchantID string `json:"merchant_id"`
}

// SendTestNotification handles POST /test/notification
// Sends an order update notification to all devices for a merchant
func (h *NotificationHandler) SendTestNotification(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "notification", "test_send", map[string]string{"error": "missing_token"})
		return
	}

	var payload SendTestNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "notification", "test_send", map[string]string{"error": "invalid_body"})
		return
	}

	// Validate required fields
	if strings.TrimSpace(payload.OrderID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "notification", "test_send", map[string]string{"error": "missing_order_id"})
		return
	}

	if strings.TrimSpace(payload.MerchantID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "notification", "test_send", map[string]string{"error": "missing_merchant_id"})
		return
	}

	// Send notification asynchronously
	err := h.service.SendNotificationAsync(payload.MerchantID, payload.OrderID, NotificationTypeOrderUpdate)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "notification", "test_send", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "notification", "test_send", map[string]interface{}{
		"status":      "1",
		"message":     "Notification sent successfully",
		"order_id":    payload.OrderID,
		"merchant_id": payload.MerchantID,
	})
}
