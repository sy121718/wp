// product_publish_attempt_render_test.go — 发布历史（B11）在页面上的形状。
//
// 这一屏的 DOM 全部来自页面里的 Jet <template>（后端只回 JSON），所以「模板与接口字段
// 是否对齐」只能在渲染出的 HTML 上验 —— 字段名写错时 json-swap 只把空串填进去，
// 页面上什么都不报。
//
// 钉两件事：
//  1. 行模板的 data-field 与 dto 的 json 键逐一对应（少一个字段就是少一列静默空白）；
//  2. 触发源七档都有 data-when 分支（671 的 CHECK 是七值，少一档那次尝试显示成空白）。
package producthttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

// publishRecordBodyWithButtons 渲染发布记录页，并按需注入菜单按钮授权。
func publishRecordBodyWithButtons(t *testing.T, buttons map[string]bool) string {
	t.Helper()
	view := productPublishRecordPageViewStub(func(v *productdto.ProductPublishRecordPageResp) {
		v.Projects = []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}
		v.SelectedProject = "p1"
		v.Total = 1
		v.Records = []productdto.PublishRecordRowResp{{
			ProductID: "prod-1", ProductName: "已上线商品", ProductSlug: "live",
			HasInstance: true, Online: true,
		}}
	})
	reader := &productPageReaderStub{publishView: view}
	handle := &ProductPageHandle{reader: reader, products: reader}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/products/publish-records", func(c *gin.Context) {
		c.Set(adminctx.ButtonsKey, buttons)
		adminui.PageSlot(ProductPublishRecordPageTemplate)(c)
		handle.ProductPublishRecordPage(c)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/publish-records?project=p1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面应为 200，实际 %d：%s", rec.Code, headOf(rec.Body.String()))
	}
	return rec.Body.String()
}

func TestProductPublishRecordHistoryButtonAndTemplate(t *testing.T) {
	body := publishRecordBodyWithButtons(t, map[string]bool{"product.publish_attempts": true})

	for _, want := range []string{
		// 触发按钮：打 JSON 接口 + json-swap 声明
		"/api/product/publish-attempts?project=p1&amp;productId=prod-1",
		`hx-ext="json-swap"`,
		`data-json-template="#tpl-publish-attempt"`,
		`data-json-rows="rows"`,
		`data-json-key="id"`,
		`data-json-more="page"`,
		// 行模板的每一个 data-field 都在（少一个 = 一列静默空白）
		`data-field="createdAtText"`,
		`data-field="operatorName"`,
		`data-field="langsText"`,
		`data-field="artifactHash"`,
		`data-field="durationText"`,
		`data-field="attemptsText"`,
		`data-field="errorText"`,
		// 触发源七档（671 的 CHECK 是七值，少一档那次尝试显示成空白）
		`data-when="create"`, `data-when="manual"`, `data-when="template_switch"`,
		`data-when="update_url"`, `data-when="rollback"`, `data-when="override"`,
		`data-when="auto_rebuild"`,
		// 结果两档
		`data-when="success"`, `data-when="failed"`,
		// 加载更多：追加而不是整块重渲染
		`data-json-patch="append"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}
}

// 没有按钮授权时整块不渲染：没有触发按钮的行容器永远打不开。
func TestProductPublishRecordHistoryHiddenWithoutButton(t *testing.T) {
	body := publishRecordBodyWithButtons(t, map[string]bool{})
	if strings.Contains(body, "/api/product/publish-attempts") {
		t.Fatal("没有 product.publish_attempts 按钮授权时不该渲染发布历史入口")
	}
	if strings.Contains(body, `id="ph-box-prod-1"`) {
		t.Fatal("没有按钮时行容器也不该渲染（没人能打开它）")
	}
}
