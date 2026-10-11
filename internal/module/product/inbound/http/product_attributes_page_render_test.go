package producthttp

// product_attributes_page_render_test.go — 属性库整页的渲染出口（真 Jet + 真模板）。
//
// 属性页此前由 product_taxonomy_page_render_test.go 的 taxonomyPages 表覆盖（桩直调
// CountAttributes / ListAttributes）。取数下沉之后那两条路径不再经过控制器，所以本文件
// 接住同一族判据，改用**读模型桩**：
//
//	读模型没接上              → 500 + no-store（不是静默渲染成「没有属性组」）
//	?project=<不存在的 id>    → 400 + no-store
//	正常档                    → 200、完整页壳、行与抽屉表单真的渲染出来、参数转发到读模型
//
// 反面判据与 taxonomy 那批同源：只断言「失败档是红色」挡不住「把取数整段跳过」——
// 所以成功档必须断言**该取的数真的取了**（这里体现为读模型收到的入参）。

import (
	"errors"
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
func attributesPageRequest(t *testing.T, h *productPageHandle, target string) *httptest.ResponseRecorder {
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
	engine.GET("/admin/product-attributes", h.ProductAttributesPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// attributesPageEnv 读模型桩 + 工程清单（与 taxonomyEnv 同形，只是读模型走视图）。
func attributesPageEnv(view *productdto.ProductAttributesPageView, err error) (*productPageHandle, *productPageReaderStub) {
	reader := &productPageReaderStub{attributesPageView: view, attributesPageErr: err}
	h := &productPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: reader,
	}
	return h, reader
}

// TestProductAttributesPageSuccessRendersRowsAndDrawers 正常档：行、两个抽屉模板、参数转发一次到位。
func TestProductAttributesPageSuccessRendersRowsAndDrawers(t *testing.T) {
	view := productAttributesPageViewStub(func(v *productdto.ProductAttributesPageView) {
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
	view := productAttributesPageViewStub(func(v *productdto.ProductAttributesPageView) {
		v.LoadFailed = true
		v.HTTPStatus = http.StatusBadRequest
	})
	h, _ := attributesPageEnv(view, errors.New("product.invalidParam"))
	rec := attributesPageRequest(t, h, "/admin/product-attributes?project=does-not-exist")
	assertProductPageFailure(t, "属性页未知工程", rec, http.StatusBadRequest, "这一页的数据没能读出来")
}

// TestProductAttributesPageWithoutReadModelIsFiveHundred 装配缺陷：读模型没接上必须 500 + no-store。
//
// 反面判据：静默渲染成一张「没有属性组」的空表最省事，但那会让「装配没接上」与
// 「这个工程本来就没有属性组」在页面上长得一模一样，而前者要有人去看装配。
func TestProductAttributesPageWithoutReadModelIsFiveHundred(t *testing.T) {
	rec := attributesPageRequest(t, &productPageHandle{}, "/admin/product-attributes")
	assertProductPageFailure(t, "属性页读模型未注入", rec, http.StatusInternalServerError, "这一页的数据没能读出来")
}

// TestProductAttributesPageNilViewIsGuarded 读模型返回 nil 视图：不 panic，兜成 500 页。
//
// 契约上视图恒非 nil，但契约是**约定**而不是类型保证 —— 控制器那一层兜底必须存在，
// 否则一次装配/实现失误会让整页 500 且没有页壳（用户只能手改地址栏）。
func TestProductAttributesPageNilViewIsGuarded(t *testing.T) {
	reader := &productPageReaderStub{}
	reader.attributesPageView = nil
	h := &productPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: reader,
	}
	rec := attributesPageRequest(t, h, "/admin/product-attributes?project=p1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("nil 视图应兜成 500，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "</html>") {
		t.Fatal("nil 视图兜底也必须是完整页壳")
	}
}
