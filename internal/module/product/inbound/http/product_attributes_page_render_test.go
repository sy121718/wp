package producthttp

// product_attributes_page_render_test.go — 属性库整页的渲染出口（真 Jet + 真模板）。
//
// 属性页此前由 product_taxonomy_page_render_test.go 的 taxonomyPages 表覆盖（桩直调
// CountAttributes / ListAttributes）。取数下沉之后那两条路径不再经过控制器，所以本文件
// 接住同一族判据，改用**读模型桩**：
//
//	?project=<不存在的 id>    → 400 + no-store
//	正常档                    → 200、完整页壳、行与抽屉表单真的渲染出来、参数转发到读模型
//
// 「读模型没接上 → 500」这一档已从运行期消失（读模型是构造实参，漏接=编译错误，
// 判据见 product_reader_injection_test.go），所以下表里没有它。
//
// 反面判据与 taxonomy 那批同源：只断言「失败档是红色」挡不住「把取数整段跳过」——
// 所以成功档必须断言**该取的数真的取了**（这里体现为读模型收到的入参）。

import (
	"go_wp/internal/adminui"
	productenums "go_wp/internal/module/product/enums"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

// attributesPageRequest 挂上属性页并 GET 一次（真 Jet 渲染器 + 真模板）。
func attributesPageRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	// 三个操作按钮码：模板用 isset(.Buttons[...]) 决定是否渲染行内抽屉与页头新建入口
	//（本用例要验的正是「行与抽屉真的渲染出来了」，所以权限位必须给全）。
	// 键名是 internal/middleware/adminctx 的私有约定（button_set），该包不导出键常量
	//（见 page/inbound/http/page_translations_failure_test.go 的同一写法）。
	engine.Use(func(c *gin.Context) {
		c.Set("button_set", map[string]bool{
			"product.attribute_create": true,
			"product.attribute_update": true,
			"product.attribute_delete": true,
		})
		c.Next()
	})
	engine.GET("/admin/product-attributes", adminui.PageSlot(ProductAttributesPageTemplate), h.ProductAttributesPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// attributesPageEnv 读模型桩 + 工程清单（与 taxonomyEnv 同形，只是读模型走视图）。
func attributesPageEnv(view *productdto.ProductAttributesPageResp, err error) (*ProductPageHandle, *productPageReaderStub) {
	reader := &productPageReaderStub{attributesPageView: view, attributesPageErr: err}
	h := &ProductPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: reader, reader: reader,
	}
	return h, reader
}

// TestProductAttributesPageSuccessRendersRowsAndDrawers 正常档：行、两个抽屉模板、参数转发一次到位。
func TestProductAttributesPageSuccessRendersRowsAndDrawers(t *testing.T) {
	view := productAttributesPageViewStub(func(v *productdto.ProductAttributesPageResp) {
		v.SelectedProject = "p1"
		v.AttributeDetails = []*productdto.AttributeResp{
			{ID: "a1", ProjectID: "p1", Key: "color", Name: "颜色", IsVariation: true, ValueCount: 3},
		}
		v.Total = 1
	})
	h, reader := attributesPageEnv(view, nil)

	rec := attributesPageRequest(t, h, "/admin/product-attributes?project=p1&keyword=col&variation=1&page=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("正常档应为 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	if got := rec.Header().Get("Cache-Control"); got == "no-store" {
		t.Fatal("正常档不该 no-store（那是失败页的档）")
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<html", "</body>", "</html>",
		// 行与抽屉外壳：行装配仍留在控制器，漏装会让页面看起来「没有属性组」。
		`color`, `颜色`,
		`tpl-attr-edit-a1`, `tpl-attr-values-a1`, `tpl-attr-create`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺 %q，body=%s", want, headOf(body))
		}
	}
	// 参数转发：四个查询串参数一个都不能丢（丢掉 page 会让翻页回到第 1 页而链接显示第 2 页）。
	if len(reader.attributesPageReqs) != 1 {
		t.Fatalf("读模型应被调用一次，实际 %d 次", len(reader.attributesPageReqs))
	}
	got := reader.attributesPageReqs[0]
	if got.ProjectID != "p1" || got.Keyword != "col" || got.Variation != "1" || got.Page != 2 {
		t.Fatalf("查询串没原样转发给读模型：%+v", got)
	}
}

// TestProductAttributesPageUnknownProjectIsBadRequest 未知工程：400 + no-store，仍是「这一页」。
func TestProductAttributesPageUnknownProjectIsBadRequest(t *testing.T) {
	view := productAttributesPageViewStub(func(v *productdto.ProductAttributesPageResp) {
	})
	h, _ := attributesPageEnv(view, productenums.Err(productenums.ErrInvalidParam))
	rec := attributesPageRequest(t, h, "/admin/product-attributes?project=does-not-exist")
	assertProductJSONFailure(t, "属性页未知工程", rec, http.StatusBadRequest, "请求参数无效")
}

// 读模型没接上这一档已从运行期消失：读模型是构造实参（ProductPageHandle.reader），
// 漏接 = 编译错误，判据见 product_reader_injection_test.go —— 运行期不再有
// 「类型断言失败 → 500 兜底」这条路径，所以这里不再有对应用例。

// TestProductAttributesPageNilViewIsGuarded 读模型返回 nil 视图：不 panic，交 500 JSON。
//
// 契约上视图恒非 nil，但契约是**约定**而不是类型保证 —— 控制器那一层守卫必须存在，
// 否则一次装配/实现失误会变成 nil 解引用（整个进程 recover 成一张无信息错误页）。
func TestProductAttributesPageNilViewIsGuarded(t *testing.T) {
	reader := &productPageReaderStub{}
	reader.attributesPageView = nil
	h := &ProductPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: reader, reader: reader,
	}
	rec := attributesPageRequest(t, h, "/admin/product-attributes?project=p1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("nil 视图应兜成 500，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("nil 视图兜底也走 JSON 出口，Content-Type=%q", ct)
	}
}

// TestProductAttributesPageUsesJSONWriteBase 属性页的写动作必须声明在 JSON 写基座上。
//
// 本批把 create/update/set-values/delete/bulk-delete 五个页面 POST 换成同名的 /api 端点，
// 页面侧只留下 data-write-* 声明。这类改动的失败模式是**静默**的：漏一处声明，按钮还在、
// 点了没反应（基座不认这个表单），而没有任何一条既有判据会红 —— 原来守这五条 htmx 出口的
// 用例（product_attribute_form_fail_test.go / product_attribute_form_fail_render_test.go）
// 已随功能删除。
func TestProductAttributesPageUsesJSONWriteBase(t *testing.T) {
	view := productAttributesPageViewStub(func(v *productdto.ProductAttributesPageResp) {
		v.SelectedProject = "p1"
		v.AttributeDetails = []*productdto.AttributeResp{
			{ID: "a1", ProjectID: "p1", Key: "color", Name: "颜色", IsVariation: true, ValueCount: 3},
		}
		v.Total = 1
	})
	h, _ := attributesPageEnv(view, nil)

	rec := attributesPageRequest(t, h, "/admin/product-attributes?project=p1")
	if rec.Code != http.StatusOK {
		t.Fatalf("正常档应为 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	body := rec.Body.String()
	for _, want := range []string{
		// 列表级：批量表单容器 + 批量按钮的就地声明。
		`data-list-bulk`,
		`data-write-api="/api/product/attribute/bulk-delete"`,
		`data-write-mode="bulk"`,
		// 行级：单条删除（端点只吃 ID，所以不带 data-write-extra）。
		`data-write-api="/api/product/attribute/delete"`,
		`data-write-mode="single"`,
		// 抽屉里的两条表单（编辑 = update、值编辑 = set-values）。
		`data-write-api="/api/product/attribute/update"`,
		`data-write-api="/api/product/attribute/set-values"`,
		// 状态位：基座把失败/成功文案写进它。
		`data-write-status`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("属性页缺少 %s —— 该写动作没接到 JSON 写基座上", want)
		}
	}
	// 负向：三条已删的页面 POST 不能再出现在页面里（残留即「点了走整页跳转」）。
	for _, gone := range []string{
		"/admin/product-attributes/bulk-delete",
		"/admin/product-attributes/delete",
		"/admin/product-attributes/set-values",
	} {
		if strings.Contains(body, gone) {
			t.Fatalf("属性页仍引用已删除的页面 POST %q", gone)
		}
	}
}
