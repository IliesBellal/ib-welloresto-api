package models

import "encoding/json"

type WebhookEvent struct {
	EventID      string          `json:"event_id"`
	EventType    string          `json:"event_type"`
	EventTime    int64           `json:"event_time"`
	Resource     json.RawMessage `json:"resource,omitempty"`
	ResourceHref string          `json:"resource_href"`
	Meta         WebhookMeta     `json:"meta"`
}

type WebhookMeta struct {
	// orders.notification
	UserID     string `json:"user_id,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`

	// delivery.state_changed
	OrderID string `json:"order_id,omitempty"`
	Status  string `json:"status,omitempty"`

	// parfois présent
	CourierTripID string `json:"courier_trip_id,omitempty"`
}
