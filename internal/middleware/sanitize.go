package middleware

import "strings"

var sensitiveKeys = []string{
	"password",
	"token",
	"authorization",
	"secret",
}

func sanitizePayload(payload string) string {
	lower := strings.ToLower(payload)

	for _, k := range sensitiveKeys {
		if strings.Contains(lower, k) {
			return "[REDACTED]"
		}
	}
	return payload
}
