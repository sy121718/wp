package producthttp

// product_page_util_test.go — HTMX 写表单「回填快照」的最小验证（本批铺设的地基）。
//
// 只钉一件后续各写表单都要依赖、错了会**静默失效**的事：归一化后的字段值、多值顺序、
// 去首尾空白、空值丢弃。（勾选态与「缺键补零」原由 formEchoData 的三个 map 键承担，
// 现已换成 dto 的 XxxFormEcho 结构体 + adminui.FillForm —— 字段即清单、Go 类型即控件形态，
// 那两件事由各表单自己的反射 + 模板源码对账判据钉住。）
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

// formEchoFrom：单值取首个、多值保序、空值丢弃 —— 所有失败回填共用的快照口径。
//
// 商品建表单与两个属性抽屉都把这个快照喂给 adminui.FillForm（结构体字段即清单），
// 所以这里钉的是**所有**回填路径的输入：归一化口径一变，症状是「用户打的空格被原样回填 /
// 多值顺序被改」，两边都不报错、页面看起来正常。
func TestFormEchoSnapshotKeepsSubmittedValues(t *testing.T) {
	c, _ := newHXContext(t, "true",
		"name=%E5%95%86%E5%93%81&sku=%20ABC_B%20&attributeIds=a1&attributeIds=a2&empty=&warehouseIds=w1")
	echo := formEchoFrom(c)

	if got := echo.value("name"); got != "商品" {
		t.Errorf("value(name) = %q，want 商品", got)
	}
	// 去首尾空白：回填进输入框的该是用户实际打的字，不是他误带的空格。
	if got := echo.value("sku"); got != "ABC_B" {
		t.Errorf("value(sku) = %q，want ABC_B（去首尾空白）", got)
	}
	// 没提交的字段给空串：调用方拿到的永远是能直接进视图的值（缺失不等于 panic）。
	if got := echo.value("type"); got != "" {
		t.Errorf("value(type) = %q，want 空串", got)
	}
	// 多值保序：顺序在本域有语义（「按本表顺序第一个勾中的仓是认领仓」）。
	if got := echo.list("attributeIds"); len(got) != 2 || got[0] != "a1" || got[1] != "a2" {
		t.Errorf("list(attributeIds) = %v，want [a1 a2]（保提交顺序）", got)
	}
	// 空值提交（empty=）等同没填：留着空串会让「填过」与「没填」在回填里混掉。
	if got := echo.list("empty"); len(got) != 0 {
		t.Errorf("list(empty) = %v，want 空（空值丢弃）", got)
	}
}
