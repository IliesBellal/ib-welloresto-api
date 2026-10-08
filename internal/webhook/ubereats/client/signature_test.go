package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"event_type":"orders.notification","meta":{"resource_id":"abc"}}`)
	mac := hmac.New(sha256.New, []byte("client-secret"))
	mac.Write(body)
	good := hex.EncodeToString(mac.Sum(nil))

	cases := []struct {
		name, sig, secret string
		body              []byte
		want              bool
	}{
		{"valid", good, "client-secret", body, true},
		{"valid, uppercase hex", strings.ToUpper(good), "client-secret", body, true},
		{"wrong secret", good, "other-secret", body, false},
		{"body altered", good, "client-secret", append([]byte(nil), append(body, ' ')...), false},
		{"missing header", "", "client-secret", body, false},
		{"no secret configured", good, "", body, false},
	}
	for _, c := range cases {
		if got := VerifySignature(c.body, c.sig, c.secret); got != c.want {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
