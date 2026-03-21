package helpers

import (
	"fmt"
	"welloresto-api/internal/models"
)

func GetRedisOrderKey(merchantID string, orderID string) string {
	return fmt.Sprintf(models.OrdersCachePrefix+"%s:%s", merchantID, orderID)
}

func GetWebhookUberEventKey(eventType, resourceID, status string) string {
	return fmt.Sprintf(models.WebhookUberEatsEventPrefix+"%s:%s:%s", eventType, resourceID, status)
}
