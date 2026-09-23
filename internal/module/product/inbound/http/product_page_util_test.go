package producthttp

// product_page_util_test.go — HTMX 写表单「分档出口」的最小验证（本批铺设的地基）。
//
// 只钉两件后续各写表单都要依赖、错了会**静默失效**的事：
//   · 分档判据：带 HX-Request 的请求走片段 / HX-Redirect，不带的一律走原生 302；
//   · 回填快照：字段值、多值顺序、勾选态、缺失键补零（模板访问缺失键会中断渲染）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
)

// hxStubRender 片段渲染的替身：只记录「渲染了哪个模板」并回显模板名。
//
// 本测试验的是分档与状态码，不是片段长相 —— 片段模板由后续各批自己建，
// 这里若去渲染真实模板，测试会跟着模板字段漂移（而它并不是本 helper 的契约）。
type hxStubRender struct{}

func (s *hxStubRender) Instance(name string, _ any) ginrender.Render {
	return &hxStubInstance{name: name}
}

type hxStubInstance struct{ name string }

func (i *hxStubInstance) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func (i *hxStubInstance) Render(w http.ResponseWriter) error {
	// Content-Type 由渲染实例自己写（gin v1.12 的 Context.Render 不再代劳，
	// 真实 jet 渲染器也在自己的 Render 里调 WriteContentType，这里照它的口径）。
	i.WriteContentType(w)
	_, err := w.Write([]byte("FRAG:" + i.name))
	return err
}

// newHXContext 造一个带（可选）HX-Request 头的请求上下文与记录器。
//
// HTMLRender 设在 `gin.CreateTestContext` 返回的 engine 上：那个 engine 就是 c 内部持有的
// 同一个实例（gin v1.12 的 `Context` 没有导出 `Engine()`，只能从这个返回值拿）。
func newHXContext(t *testing.T, hxHeader, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	engine.HTMLRender = &hxStubRender{}
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/products/create", strings.NewReader(body))
	if hxHeader != "" {
		c.Request.Header.Set("HX-Request", hxHeader)
	}
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c, rec
}

// 带 HX-Request: true 的请求渲染片段并 200；其余（无头 / false）一律不写响应，交调用方 302。
func TestHXFragmentOnlyAnswersHtmxRequests(t *testing.T) {
	data := gin.H{"Err": "名称不能为空"}

	c, rec := newHXContext(t, "true", "name=x")
	if !hxFragment(c, "admin/product/product_create_form.html", data) {
		t.Fatal("带 HX-Request: true 的请求应当走片段分支")
	}
	// gin 的状态码是**延迟写出**的（Context.Status 只记录，请求结束才刷）：测试没走
	// engine.ServeHTTP，所以先显式刷一次，再断言真正写进 HTTP 的码 —— 直接读 rec.Code
	// 会拿到 httptest 的默认 200，把「一个字没写」误判成「写了 200」。
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK {
		t.Errorf("片段分支状态码 = %d，want 200（htmx 只替换 2xx 的响应）", rec.Code)
	}
	if got, want := rec.Body.String(), "FRAG:admin/product/product_create_form.html"; got != want {
		t.Errorf("片段分支渲染内容 = %q，want %q", got, want)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("片段分支 Content-Type = %q，want 含 text/html", ct)
	}

	// " TRUE " 也算 htmx（大小写不敏感 + 去空白，与 redirectWhere 同口径）；
	// "" / "false" 是原生请求：helper 必须一个字都不写，否则调用方的 302 会变成「200 之后又 302」。
	for _, tc := range []struct {
		header string
		htmx   bool
	}{
		{" TRUE ", true},
		{"True", true},
		{"false", false},
		{"", false},
	} {
		c2, rec2 := newHXContext(t, tc.header, "name=x")
		handled := hxFragment(c2, "admin/product/product_create_form.html", data)
		if handled != tc.htmx {
			t.Errorf("HX-Request=%q：hxFragment = %v，want %v", tc.header, handled, tc.htmx)
		}
		if tc.htmx {
			continue
		}
		if rec2.Body.Len() != 0 {
			t.Errorf("HX-Request=%q：原生请求不该写响应体，实际 %q", tc.header, rec2.Body.String())
		}
		if rec2.Header().Get("HX-Redirect") != "" {
			t.Errorf("HX-Request=%q：原生请求不该带 HX-Redirect", tc.header)
		}
	}
}

// redirectWhere：htmx 档走 HX-Redirect + 200（且不写 Location），原生档维持 302 + Location。
func TestRedirectWhereSplitsHtmxAndNative(t *testing.T) {
	const target = "/admin/products?project=p1&err=x"

	c, rec := newHXContext(t, "true", "")
	redirectWhere(c, target)
	if got := rec.Header().Get("HX-Redirect"); got != target {
		t.Errorf("htmx 档 HX-Redirect = %q，want %q", got, target)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("htmx 档不该写 Location（htmx 会自己跟随 302，那时响应头已经读不到）= %q", loc)
	}
	c.Writer.WriteHeaderNow() // 刷出延迟写的状态码（见上一条测试的说明）
	if rec.Code != http.StatusOK {
		t.Errorf("htmx 档状态码 = %d，want 200", rec.Code)
	}

	c2, rec2 := newHXContext(t, "", "")
	redirectWhere(c2, target)
	if got := rec2.Header().Get("Location"); got != target {
		t.Errorf("原生档 Location = %q，want %q", got, target)
	}
	if hxr := rec2.Header().Get("HX-Redirect"); hxr != "" {
		t.Errorf("原生档不该带 HX-Redirect = %q", hxr)
	}
	c2.Writer.WriteHeaderNow()
	if rec2.Code != http.StatusFound {
		t.Errorf("原生档状态码 = %d，want 302", rec2.Code)
	}
}

// formEchoData：单值取首个、多值保序、空值丢弃、复选框按「是否提交过」判定、列出即补零键。
func TestFormEchoDataKeepsSubmittedValues(t *testing.T) {
	c, _ := newHXContext(t, "true",
		"name=%E5%95%86%E5%93%81&sku=ABC_B&attributeIds=a1&attributeIds=a2&trackQuantity=1&empty=&warehouseIds=w1")
	data := formEchoData(c, "name", "sku", "type", "attributeIds", "trackQuantity", "empty")

	form, ok := data["FormEcho"].(gin.H)
	if !ok {
		t.Fatal("data[\"FormEcho\"] 不是 gin.H —— 模板里的 {{ .FormEcho.name }} 会直接取不到")
	}
	if got := form["name"]; got != "商品" {
		t.Errorf("FormEcho.name = %v，want 商品", got)
	}
	if got := form["sku"]; got != "ABC_B" {
		t.Errorf("FormEcho.sku = %v，want ABC_B", got)
	}
	// 提交里没有的字段也必须存在（空串）：Jet 对缺失的 map 键会中断整页渲染。
	if got, exists := form["type"]; !exists || got != "" {
		t.Errorf("FormEcho.type = %v（存在=%v），want 空串且键存在", got, exists)
	}
	if _, exists := form["warehouseIds"]; exists {
		t.Error("未列出的字段不该出现在 FormEcho 里（列出即补零，未列出即不出现）")
	}

	checked, ok := data["FormEchoChecked"].(gin.H)
	if !ok {
		t.Fatal("data[\"FormEchoChecked\"] 不是 gin.H")
	}
	if got := checked["trackQuantity"]; got != true {
		t.Errorf("FormEchoChecked.trackQuantity = %v，want true", got)
	}
	// 空值提交（empty=）不算勾选：判据是「字段在提交里存在且有值」。
	if got := checked["empty"]; got != false {
		t.Errorf("FormEchoChecked.empty = %v，want false（空值不算勾选）", got)
	}

	multi, ok := data["FormEchoMulti"].(gin.H)
	if !ok {
		t.Fatal("data[\"FormEchoMulti\"] 不是 gin.H")
	}
	list, _ := multi["attributeIds"].([]string)
	if len(list) != 2 || list[0] != "a1" || list[1] != "a2" {
		t.Errorf("FormEchoMulti.attributeIds = %v，want [a1 a2]（保提交顺序）", list)
	}
}
