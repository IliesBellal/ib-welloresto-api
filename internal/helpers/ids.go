package helpers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

const (
	AuditLogIDPrefix                     = "audit-log"
	AvailabilityIDPrefix                 = "avail"
	AvailabilitySchedulePrefix           = "avail-sc"
	AvailabilityProductPrefix            = "avail-prod"
	DiscountIDPrefix                     = "discount"
	TagIDPrefix                          = "tag"
	AttributeIDPrefix                    = "attribute"
	AttributeOptionIDPrefix              = "attribute-option"
	ReceiptIDPrefix                      = "receipt"
	UserIDPrefix                         = "user"
	HACCPTemperatureZoneIDPrefix         = "haccp-tz"
	HACCPTemperatureReadingIDPrefix      = "haccp-tr"
	HACCPCorrectiveActionIDPrefix        = "haccp-ca"
	HACCPReadingCorrectiveActionIDPrefix = "haccp-rca"
	HACCPTemperatureSessionIDPrefix      = "haccp-ts"
	HACCPSettingsIDPrefix                = "haccp-settings"
	HACCPUploadRecordIDPrefix            = "haccp-file"
	HACCPCleaningZoneIDPrefix            = "haccp-cz"
	HACCPCleaningSurfaceIDPrefix         = "haccp-cs"
	HACCPCleaningSessionIDPrefix         = "haccp-csess"
	HACCPCleaningTaskIDPrefix            = "haccp-ct"
	HACCPCleaningExecutionIDPrefix       = "haccp-ce"
	HACCPGoodsReceiptIDPrefix            = "haccp-gr"
	PlanningSettingsIDPrefix             = "plan-set"
	PlanningEmployeeIDPrefix             = "plan-emp"
	PlanningEmployeeDocumentIDPrefix     = "plan-doc"
	PlanningWeekIDPrefix                 = "plan-week"
	PlanningShiftIDPrefix                = "plan-shift"
	PlanningTimeEntryIDPrefix            = "plan-time"
	PlanningLeaveRequestIDPrefix         = "plan-leave"
	PlanningShiftSwapRequestIDPrefix     = "plan-swap"
	PlanningEmployeeAmendmentIDPrefix    = "plan-ame"
	PlanningSystemRuleIDPrefix           = "plan-rule"
	PlanningHolidayIDPrefix              = "plan-hol"
)

// GeneratePrefixedID generates a unique ID with the given prefix (e.g., "order-xxxx-xxxx").
func GeneratePrefixedID(prefix string) string {
	return prefix + "-" + uuid.New().String()
}

// GenerateToken returns a cryptographically secure random hex token of the
// given byte length (e.g., 16 → 32-char token).
func GenerateToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generateToken: %w", err)
	}
	return hex.EncodeToString(b), nil
}
