package helpers

import (
	"fmt"
	"welloresto-api/internal/models"
)

func GetRedisOrderKey(merchantID string, orderID string) string {
	return fmt.Sprintf(models.OrdersCachePrefix+"%s:%s", merchantID, orderID)
}
