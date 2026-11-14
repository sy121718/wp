package binding

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

func TestParseFormKeepsPartialValuesOnError(t *testing.T) {
	c, rec := newContext(http.MethodPost, "/x", "name=abc")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := ParseForm[requiredNameReq](c)
	if err == nil {
		t.Fatal("缺必填项应报错（由调用方决定怎么渲染，包内不写响应）")
	}
	if req == nil || req.Name != "abc" {
		t.Fatalf("失败时也要回已绑上的字段，实际 %+v", req)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("ParseForm 不该写响应，实际 %s", rec.Body.String())
	}
}

func TestFieldReadsSharedHidden(t *testing.T) {
	c, _ := newContext(http.MethodPost, "/x", "next=+%2Fuser%2Faccount+&other=1")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if got := Field(c, "next"); got != "/user/account" {
		t.Fatalf("应去空白后返回，实际 %q", got)
	}
	if got := Field(c, "missing"); got != "" {
		t.Fatalf("缺省键应返回空串，实际 %q", got)
	}
}

type requiredNameReq struct {
	Name  string `form:"name"`
	Title string `form:"title" binding:"required"`
}

func newContext(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request = req
	return c, rec
}

// 兼容入口同时接受 JSON 与表单：访客下单、退货申请等公开端点两种调用方都在用。
// 正因如此它不声明唯一协议（契约生成器标 unknown），需要协议时用显式入口。
func TestCompatJSONAcceptsBothShapes(t *testing.T) {
	c, _ := newContext(http.MethodPost, "/x", "name=abc&page=3")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if req, ok := JSON[sampleReq](c); !ok || req.Name != "abc" || req.Page != 3 {
		t.Fatalf("兼容入口应接受表单体：%+v ok=%v", req, ok)
	}

	c, _ = newContext(http.MethodPost, "/x", `{"name":"abc","page":3}`)
	c.Request.Header.Set("Content-Type", "application/json")
	if req, ok := JSON[sampleReq](c); !ok || req.Name != "abc" || req.Page != 3 {
		t.Fatalf("兼容入口应接受 JSON：%+v ok=%v", req, ok)
	}
}

// 多值表单由表单入口绑定（读写方法均确定的 Form/ParseForm）。
func TestFormReadsMultiValueBody(t *testing.T) {
	type multiReq struct {
		TagIDs []string `form:"tagIds"`
	}
	c, _ := newContext(http.MethodPost, "/x", "tagIds=t1&tagIds=t2&tagIds=t3")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, ok := Form[multiReq](c)
	if !ok {
		t.Fatalf("多值表单应能绑定")
	}
	if len(req.TagIDs) != 3 || req.TagIDs[0] != "t1" || req.TagIDs[2] != "t3" {
		t.Fatalf("同名多值没收成切片：%v", req.TagIDs)
	}
}

// 显式 JSON 入口不接受表单体：协议由端点声明，不按 Content-Type 猜。
func TestParseJSONRejectsFormBody(t *testing.T) {
	c, _ := newContext(http.MethodPost, "/x", "name=abc&page=3")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := ParseJSON[sampleReq](c); err == nil {
		t.Fatal("ParseJSON 不该接受表单体")
	}
}

// 显式表单入口只读 body，不合并查询串。
func TestParseFormIgnoresQuery(t *testing.T) {
	c, _ := newContext(http.MethodPost, "/x?name=from-query", "page=3")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, err := ParseForm[sampleReq](c)
	if err != nil || req.Name != "" || req.Page != 3 {
		t.Fatalf("表单入口不该合并查询串：%+v err=%v", req, err)
	}
}

// 查询入口只读查询串。
func TestQueryReadsOnlyQueryString(t *testing.T) {
	c, _ := newContext(http.MethodGet, "/x?name=abc&page=3", "")
	req, ok := Query[sampleReq](c)
	if !ok || req.Name != "abc" || req.Page != 3 {
		t.Fatalf("查询入口应从查询串绑定：%+v ok=%v", req, ok)
	}
}

func TestJSONStillRequiresValidJSONWithoutContentType(t *testing.T) {
	c, rec := newContext(http.MethodPost, "/x", "{not json")
	if _, ok := JSON[sampleReq](c); ok {
		t.Fatalf("无 Content-Type 的畸形 body 不该被当成表单绑定成功")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("应写 400，实际 %d", rec.Code)
	}
}
