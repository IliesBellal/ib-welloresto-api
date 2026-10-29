package deliveroosig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerify(t *testing.T) {
	const secret, guid, body = "whsec-test", "a1b2-c3d4", `{"event":"order.new"}`
	cases := []struct {
		name, guid, sig, secret, want string
	}{
		{"séparateur espace", guid, sign(secret, guid+" "+body), secret, "space"},
		{"ancien séparateur", guid, sign(secret, guid+" \n "+body), secret, "legacy_newline"},
		{"casse ignorée", guid, strings.ToUpper(sign(secret, guid+" "+body)), secret, "space"},
		{"mauvais secret", guid, sign("autre", guid+" "+body), secret, ""},
		{"autre GUID", "zzzz", sign(secret, guid+" "+body), secret, ""},
		{"corps seul signé", guid, sign(secret, body), secret, ""},
		{"sans secret", guid, sign(secret, guid+" "+body), "", ""},
		{"sans signature", guid, "", secret, ""},
	}
	for _, c := range cases {
		if got := Verify([]byte(body), c.guid, c.sig, c.secret); got != c.want {
			t.Errorf("%s : Verify = %q, want %q", c.name, got, c.want)
		}
	}
}
