package response

import (
	"net/http"
	"net/http/httptest"
)

func newRequestWithHeader(headerKey, headerValue string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if headerValue != "" {
		req.Header.Set(headerKey, headerValue)
	}
	return req
}
