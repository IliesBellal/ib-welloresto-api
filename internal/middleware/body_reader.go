package middleware

import (
	"bytes"
	"io"
	"net/http"
)

func readRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	r.Body = io.NopCloser(bytes.NewBuffer(body))
	return body, nil
}
