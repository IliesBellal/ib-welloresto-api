package helpers

import (
	"fmt"
	"strings"
	"welloresto-api/internal/models"
)

func GetRedisOrderKey(merchantID string, orderID string) string {
	return fmt.Sprintf(models.OrdersCachePrefix+"%s:%s", merchantID, orderID)
}

func GetWebhookUberEventKey(eventID string) string {
	return fmt.Sprintf(models.WebhookUberEatsEventPrefix+"%s", eventID)
}

func GetWebhookDeliverooEventKey(eventID, orderID, status string) string {
	return fmt.Sprintf(models.WebhookDeliverooEventPrefix+"%s:%s:%s", eventID, orderID, status)
}

func GetWebhookDeliverooLocationKey(locationID string) string {
	return fmt.Sprintf(models.WebhookDeliverooLocationPrefix+"%s", locationID)
}

func GetMFACacheKey(token string) string {
	return fmt.Sprintf(models.MFACachePrefix+"%s", token)
}

func GetVerificationCacheKey(mode string, token string) string {
	return fmt.Sprintf(models.VerificationCachePrefix+"%s:%s", mode, token)
}

// normalizeMenuName met le nom à plat (trim + minuscule) pour que la clé de
// confirmation soit stable quelle que soit la casse/les espaces saisis.
func normalizeMenuName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func GetMenuProductNameConfirmKey(merchantID, name string) string {
	return fmt.Sprintf(models.MenuProductNameConfirmPrefix+"%s:%s", merchantID, normalizeMenuName(name))
}

func GetMenuComponentNameConfirmKey(merchantID, name string) string {
	return fmt.Sprintf(models.MenuComponentNameConfirmPrefix+"%s:%s", merchantID, normalizeMenuName(name))
}

func GetMenuAttributeNameConfirmKey(merchantID, name string) string {
	return fmt.Sprintf(models.MenuAttributeNameConfirmPrefix+"%s:%s", merchantID, normalizeMenuName(name))
}
