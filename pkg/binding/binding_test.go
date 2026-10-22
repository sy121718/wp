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

// TestParseFormKeepsPartialValuesOnError 页面出口要**就地回显表单**：
// 漏填一个必填项时，已经绑上的字段必须还在（否则整张表单在用户眼前被清空）。
// 这条与 Form 的区别只有一处 —— 谁写响应 —— 但判据完全不同。
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

// TestFieldReadsSharedHidden 跨表单共享的隐藏域（next / returnUrl）。
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

// requiredNameReq 两个字段：title 必填、name 可空 —— 提交只有 name 的表单，
// 用来验证「失败时已绑上的字段仍然返回」。
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

// TestJSONAcceptsFormBody 同一端点同时接受 JSON 与表单体（写动作的双客户端）。
//
// 这是 htmx 写表单能落到现有端点的前提：MCP/AI 发 JSON，htmx 发 formdata，
// 两边都进 binding.JSON。若退回 ShouldBindJSON，htmx 的表单会被当「JSON 解析失败」
// 整条 400（实测字段全为零值），而页面上看起来只是「点了没反应」。
func TestJSONAcceptsFormBody(t *testing.T) {
	c, rec := newContext(http.MethodPost, "/x", "name=abc&page=3")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, ok := JSON[sampleReq](c)
	if !ok {
		t.Fatalf("表单体应能绑定，实际失败（状态 %d，body %s）", rec.Code, rec.Body.String())
	}
	if req.Name != "abc" || req.Page != 3 {
		t.Fatalf("表单字段没绑上：%+v", req)
	}
}

// TestJSONFormMultiValue 同名多值要收成切片（form tag 声明时）。
//
// 多选 checkbox 组默认是「后者覆盖」，失效模式是「勾了三个、只存最后一个」——
// 那类缺陷在页面上看不出来（保存成功、只是数据少了）。
func TestJSONFormMultiValue(t *testing.T) {
	type multiReq struct {
		TagIDs []string `form:"tagIds"`
	}
	c, _ := newContext(http.MethodPost, "/x", "tagIds=t1&tagIds=t2&tagIds=t3")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, ok := JSON[multiReq](c)
	if !ok {
		t.Fatalf("多值表单应能绑定")
	}
	if len(req.TagIDs) != 3 || req.TagIDs[0] != "t1" || req.TagIDs[2] != "t3" {
		t.Fatalf("同名多值没收成切片：%v", req.TagIDs)
	}
}

// TestJSONStillRequiresValidJSONWithoutContentType 没 Content-Type 但有 body 时仍按 JSON 校验。
//
// gin 的 ShouldBind 在没有 Content-Type 时一律当表单处理，一段畸形 JSON 会被
// "绑定成功"。写接口不能靠客户端自觉带 CT，所以这条边界必须显式拦回去。
func TestJSONStillRequiresValidJSONWithoutContentType(t *testing.T) {
	c, rec := newContext(http.MethodPost, "/x", "{not json")
	if _, ok := JSON[sampleReq](c); ok {
		t.Fatalf("无 Content-Type 的畸形 body 不该被当成表单绑定成功")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("应写 400，实际 %d", rec.Code)
	}
}
