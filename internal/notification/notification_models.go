// notification/notification_models.go

package notification

type NotificationType string

type NotificationPayload map[string]interface{}

type NotificationMessage struct {
	MerchantID int
	OrderID    int
	Type       string
	EntityID   int
	Payload    NotificationPayload
}
