package binding

import (
	"errors"
	"net/http"
	"testing"
)

type failingLimitedBody struct{}

func (failingLimitedBody) Read([]byte) (int, error) { return 0, errors.New("private read failure") }
func (failingLimitedBody) Close() error             { return nil }

func TestJSONLimitedReadFailure(t *testing.T) {
	c, rec := newContext(http.MethodPost, "/x", "")
	c.Request.Body = failingLimitedBody{}
	if req, ok := JSONLimited[sampleReq](c, 10); ok || req != nil || rec.Code != http.StatusBadRequest {
		t.Fatalf("read failure: req=%v ok=%v status=%d", req, ok, rec.Code)
	}
}

// JSONLimited 在限长之后复用兼容入口，因此仍接受表单体（它服务的端点两种形状都有）。
func TestJSONLimitedAcceptsFormBody(t *testing.T) {
	c, _ := newContext(http.MethodPost, "/x", "name=abc&extra=a&extra=b")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, ok := JSONLimited[sampleReq](c, 100)
	if !ok || len(req.Extra) != 2 || req.Extra[1] != "b" {
		t.Fatalf("FormBinder: req=%+v ok=%v", req, ok)
	}
}

func TestJSONLimited(t *testing.T) {
	type request struct {
		Name string `json:"name" form:"name" binding:"required"`
	}
	for _, tc := range []struct {
		name, body, contentType string
		limit                   int64
		valid                   bool
	}{
		{"exact", `{"name":"abc"}`, "application/json", 14, true},
		{"over", `{"name":"abc"} `, "application/json", 14, false},
		{"invalid", `{broken`, "application/json", 20, false},
		{"required", `{}`, "application/json", 20, false},
		{"empty", ``, "application/json", 20, false},
		{"zero", `{"name":"abc"}`, "application/json", 0, false},
		{"negative", `{"name":"abc"}`, "application/json", -1, false},
		{"no content type", `{"name":"abc"}`, "", 14, true},
		{"form", `name=abc`, "application/x-www-form-urlencoded", 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newContext(http.MethodPost, "/x", tc.body)
			c.Request.Header.Set("Content-Type", tc.contentType)
			req, ok := JSONLimited[request](c, tc.limit)
			if ok != tc.valid {
				t.Fatalf("ok=%v, want %v: %s", ok, tc.valid, rec.Body.String())
			}
			if ok && req.Name != "abc" {
				t.Fatalf("name=%q", req.Name)
			}
			if !ok && (req != nil || rec.Code != http.StatusBadRequest) {
				t.Fatalf("req=%v status=%d", req, rec.Code)
			}
		})
	}
}
