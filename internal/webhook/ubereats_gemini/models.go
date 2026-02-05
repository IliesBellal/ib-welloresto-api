package ubereats_gemini

import "time"

// EventType définit le type d'événement reçu (ex: orders.notification)
type EventType string

const (
	EventOrderNotification EventType = "orders.notification"
	EventStoreStatus       EventType = "store.status"
	EventOrderCanceled     EventType = "orders.canceled"
	// Ajouter les autres types...
)

// WebhookPayload est la structure enveloppe générique reçue d'Uber
type WebhookPayload struct {
	EventID   string      `json:"event_id"`
	EventType EventType   `json:"event_type"`
	Time      time.Time   `json:"time"`
	Resource  interface{} `json:"resource"` // À mapper selon le type (Order, Store, etc.)
	// Meta données supplémentaires...
}

// OrderResource modèle temporaire pour une commande
type OrderResource struct {
	ID string `json:"id"`
	// Champs réels à compléter...
}

// StoreResource modèle temporaire pour un magasin
type StoreResource struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
