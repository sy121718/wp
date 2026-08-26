package response

import (
	"net/http"
	"net/http/httptest"
)

// newRequestWithHeader 构造带指定请求头的测试请求。
func newRequestWithHeader(headerKey, headerValue string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if headerValue != "" {
		req.Header.Set(headerKey, headerValue)
	}
	return req
}