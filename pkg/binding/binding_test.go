package binding

// binding_test.go — 绑定入口的三条判据：失败必写 400、缺 form tag 会静默绑不上、
// BindForm 在反射绑定之后被调用一次。
//
// 第二条不是理论问题：gin 的表单绑定按 tag 取键，字段没 tag 时它去查键 ""，
// 于是整段绑定返回零值而不报错。DTO 少写一个 tag 的表现是「前端传了、后端没收到」，
// 这里用一条会变红的断言把它钉住。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

type sampleReq struct {
	Name  string `form:"name"`
	Page  int    `form:"page"`
	Extra []string
}

func (r *sampleReq) BindForm(vals url.Values) error {
	r.Extra = vals["extra"]
	return nil
}

func TestJSONRejectsBadBody(t *testing.T) {
	c, rec := newContext(http.MethodPost, "/x", "{not json")
	if _, ok := JSON[sampleReq](c); ok {
		t.Fatal("非法 JSON 应返回 ok=false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("失败必须写 400，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "code") {
		t.Fatalf("失败响应必须是统一结构，实际 %s", rec.Body.String())
	}
}

func TestQueryBindsOnlyTaggedFields(t *testing.T) {
	c, _ := newContext(http.MethodGet, "/x?name=abc&page=3&untagged=zzz", "")
	req, ok := Query[sampleReq](c)
	if !ok {
		t.Fatal("合法查询串应绑定成功")
	}
	if req.Name != "abc" || req.Page != 3 {
		t.Fatalf("带 tag 的字段应绑上，实际 %+v", req)
	}
}

func TestFormRunsBindFormAfterReflection(t *testing.T) {
	c, _ := newContext(http.MethodPost, "/x", "name=abc&page=2&extra=a&extra=b")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, ok := Form[sampleReq](c)
	if !ok {
		t.Fatal("合法表单应绑定成功")
	}
	if req.Name != "abc" || req.Page != 2 {
		t.Fatalf("平铺字段应绑上，实际 %+v", req)
	}
	if len(req.Extra) != 2 || req.Extra[0] != "a" || req.Extra[1] != "b" {
		t.Fatalf("BindForm 应在反射绑定之后收到全部值，实际 %+v", req.Extra)
	}
}

func newContext(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request = req
	return c, rec
}
