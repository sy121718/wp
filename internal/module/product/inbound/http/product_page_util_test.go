package producthttp

// product_page_util_test.go — HTMX 写表单「回填快照」的最小验证（本批铺设的地基）。
//
// 只钉一件后续各写表单都要依赖、错了会**静默失效**的事：
// 回填快照的字段值、多值顺序、勾选态、缺失键补零（模板访问缺失键会中断渲染）。
//
// 分档判据（isHXRequest）与出口（shell.RenderJump）的守卫在 product_jump_test.go：
// 原 redirectWhere / hxFragment 两个 helper 已随「结论走 shell.RenderJump」删除。

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
// 本测试验的是回填与分档，不是片段长相 —— 片段模板由后续各批自己建，
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
