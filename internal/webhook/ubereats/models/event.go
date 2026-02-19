package models

// =======================
// ROOT EVENT
// =======================

type UberWebhookEvent struct {
	EventID      string             `json:"event_id"`
	EventType    string             `json:"event_type"`
	EventTime    int64              `json:"event_time"` // epoch ms
	ResourceHref string             `json:"resource_href"`
	Meta         UberWebhookMeta    `json:"meta"`
	WebhookMeta  UberWebhookMetaSys `json:"webhook_meta"`
}

// =======================
// META (business)
// =======================

type UberWebhookMeta struct {
	UserID        string `json:"user_id,omitempty"`
	ResourceID    string `json:"resource_id,omitempty"`
	OrderID       string `json:"order_id,omitempty"`
	Status        string `json:"status,omitempty"`
	CourierTripID string `json:"courier_trip_id,omitempty"`
}

// =======================
// WEBHOOK SYSTEM META
// =======================

type UberWebhookMetaSys struct {
	ClientID            string `json:"client_id"`
	WebhookConfigID     string `json:"webhook_config_id"`
	WebhookMsgTimestamp int64  `json:"webhook_msg_timestamp"` // epoch seconds
	WebhookMsgUUID      string `json:"webhook_msg_uuid"`
}
