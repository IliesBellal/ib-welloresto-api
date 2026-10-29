package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaxBodyForPrefix(t *testing.T) {
	var read int
	var readErr error
	h := MaxBodyForPrefix("/scannorder/", 10)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		read, readErr = len(b), err
	}))

	cases := []struct {
		path      string
		body      string
		wantRead  int
		wantError bool
	}{
		{"/scannorder/resto/orders", "0123456789", 10, false},
		{"/scannorder/resto/orders", strings.Repeat("x", 50), 10, true},
		{"/uploads/photo", strings.Repeat("x", 50), 50, false},
	}
	for _, c := range cases {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.body)))
		if read != c.wantRead || (readErr != nil) != c.wantError {
			t.Errorf("%s (%d octets) : lu %d, err %v ; want %d, erreur %v", c.path, len(c.body), read, readErr, c.wantRead, c.wantError)
		}
	}
}
