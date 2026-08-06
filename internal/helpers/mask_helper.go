package helpers

import "strings"

// MaskEmail obfuscates the local part of an email address, keeping the
// first and last character visible plus the full domain (e.g. so the user
// can still recognize which account received the code): "jean.dupont@wello.fr" -> "j***t@wello.fr".
func MaskEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	if at <= 0 {
		return email
	}

	local := email[:at]
	domain := email[at:]

	if len(local) <= 2 {
		return local[:1] + "***" + domain
	}

	return local[:1] + "***" + local[len(local)-1:] + domain
}

// MaskPhone obfuscates a phone number, keeping a short prefix (country code
// when present) and the last 2 digits: "+33612345678" -> "+33*****78".
func MaskPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if len(phone) <= 4 {
		return strings.Repeat("*", len(phone))
	}

	prefixLen := 2
	if strings.HasPrefix(phone, "+") {
		prefixLen = 3
	}
	if prefixLen > len(phone)-2 {
		prefixLen = 0
	}

	prefix := phone[:prefixLen]
	suffix := phone[len(phone)-2:]
	return prefix + "*****" + suffix
}
