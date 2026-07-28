package brevo_events

import (
	"encoding/json"
	"strconv"
	"strings"
)

// BrevoEventPayload models the generic transactional event payload returned by Brevo.
type BrevoEventPayload struct {
	Event           string          `json:"event"`
	MessageID       string          `json:"message-id"`
	MessageIDAlt    string          `json:"messageId"`
	TransactionalID string          `json:"transactionalId"`
	ID              json.RawMessage `json:"id"`
}

func (p BrevoEventPayload) ProviderMessageID() string {
	for _, candidate := range []string{p.MessageID, p.MessageIDAlt, p.TransactionalID} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	if len(p.ID) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(p.ID, &asString); err == nil && strings.TrimSpace(asString) != "" {
		return strings.TrimSpace(asString)
	}
	var asInt int64
	if err := json.Unmarshal(p.ID, &asInt); err == nil {
		return strconv.FormatInt(asInt, 10)
	}
	var asFloat float64
	if err := json.Unmarshal(p.ID, &asFloat); err == nil {
		return strconv.FormatInt(int64(asFloat), 10)
	}
	return ""
}
