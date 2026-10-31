package producthttp

import (
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
)

func publishRecordBodyWithButtons(t *testing.T, buttons map[string]bool) string {
	t.Helper()
	view := publishRecordListStub(func(v *productcontract.PublishRecordList) {
		v.Total = 1
		v.Records = []productdto.PublishRecordRowResp{{
			ProductID: "prod-1", ProductName: "已上线商品", ProductSlug: "live",
			HasInstance: true, Online: true,
		}}
	})
	reader := &publicationReaderStub{publishView: view}
	handle := &ProductPageHandle{reader: reader, products: reader}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/products/publish-records", func(c *gin.Context) {
		c.Set(adminctx.ButtonsKey, buttons)
		pageview.UseRenderer(adminui.RenderPage)(c)
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

		"/api/product/publish-attempts?project=p1&amp;productId=prod-1",
		`hx-ext="json-swap"`,
		`data-json-template="#tpl-publish-attempt"`,
		`data-json-rows="rows"`,
		`data-json-key="id"`,
		`data-json-more="page"`,

		`data-field="createdAt" data-field-format="datetime"`,
		`data-field="operatorName"`,
		`data-field="langs" data-field-format="list"`,
		`data-field="artifactHash"`,
		`data-field="durationMs" data-field-format="duration"`,
		`data-field="attempts" data-field-min="2" data-field-suffix="x"`,
		`class="ph-error" data-when="failed" data-when-field="outcome"`,

		`data-when="create"`, `data-when="manual"`, `data-when="template_switch"`,
		`data-when="update_url"`, `data-when="rollback"`, `data-when="override"`,
		`data-when="auto_rebuild"`,

		`data-when="success"`, `data-when="failed"`,

		`data-json-patch="append"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}
}

func TestProductPublishRecordHistoryHiddenWithoutButton(t *testing.T) {
	body := publishRecordBodyWithButtons(t, map[string]bool{})
	if strings.Contains(body, "/api/product/publish-attempts") {
		t.Fatal("没有 product.publish_attempts 按钮授权时不该渲染发布历史入口")
	}
	if strings.Contains(body, `id="ph-box-prod-1"`) {
		t.Fatal("没有按钮时行容器也不该渲染（没人能打开它）")
	}
}
