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

type listReq struct {
	Name      string `form:"name"`
	ListQuery string `listquery:"name,page"`
}

// Parse* 三个入口都把错误交回调用方（包内不写响应），这里同时固化这一点。
func TestParseJSONRejectsBadBody(t *testing.T) {
	c, rec := newContext(http.MethodPost, "/x", "{not json")
	if _, err := ParseJSON[sampleReq](c); err == nil {
		t.Fatal("非法 JSON 必须报错")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("ParseJSON 不该写响应，实际 %s", rec.Body.String())
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

// 查询入口只读查询串，并按 listquery tag 回填 ListQuery。
func TestParseQueryReadsOnlyQueryString(t *testing.T) {
	c, _ := newContext(http.MethodGet, "/x?name=abc&page=3&untagged=zzz", "")
	req, err := ParseQuery[listReq](c)
	if err != nil {
		t.Fatalf("合法查询串应绑定成功: %v", err)
	}
	if req.Name != "abc" {
		t.Fatalf("带 tag 的字段应绑上，实际 %+v", req)
	}
	if req.ListQuery != "name=abc&page=3" {
		t.Fatalf("ListQuery 必须按 listquery tag 回填，实际 %q", req.ListQuery)
	}

	c, _ = newContext(http.MethodPost, "/x?name=q", "name=body&page=9")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, err = ParseQuery[listReq](c)
	if err != nil {
		t.Fatalf("查询串绑定应成功: %v", err)
	}
	if req.Name != "q" {
		t.Fatalf("查询入口不该读表单体，实际 %+v", req)
	}
}

// 表单入口只读 body，不合并查询串；BindForm 在反射绑定之后拿到全部值。
func TestParseFormRunsBindFormAfterReflection(t *testing.T) {
	c, _ := newContext(http.MethodPost, "/x?name=from-query", "page=2&extra=a&extra=b")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, err := ParseForm[sampleReq](c)
	if err != nil {
		t.Fatalf("合法表单应绑定成功: %v", err)
	}
	if req.Name != "" || req.Page != 2 {
		t.Fatalf("只该读 body，实际 %+v", req)
	}
	if len(req.Extra) != 2 || req.Extra[0] != "a" || req.Extra[1] != "b" {
		t.Fatalf("BindForm 应在反射绑定之后收到全部值，实际 %+v", req.Extra)
	}
}

func TestParseFormReadsMultiValueBody(t *testing.T) {
	type multiReq struct {
		TagIDs []string `form:"tagIds"`
	}
	c, _ := newContext(http.MethodPost, "/x", "tagIds=t1&tagIds=t2&tagIds=t3")
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, err := ParseForm[multiReq](c)
	if err != nil {
		t.Fatalf("多值表单应能绑定: %v", err)
	}
	if len(req.TagIDs) != 3 || req.TagIDs[0] != "t1" || req.TagIDs[2] != "t3" {
		t.Fatalf("同名多值没收成切片：%v", req.TagIDs)
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
