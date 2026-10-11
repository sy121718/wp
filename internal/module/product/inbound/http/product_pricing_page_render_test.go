package producthttp

// product_pricing_page_render_test.go — 定价页的渲染出口（真 Jet + 真模板）。
//
// 这一屏**没有取数失败档**（三处取数各自降级），所以页面上唯一的非 200 是**装配缺陷**：
// 控制器断言不到读模型 —— 那时必须 500 + no-store，而不是静默渲染成一张空参考表。
// 两条用例各钉一半：正常档真的把页面渲染出来且入参转发到了读模型；缺陷档按失败出口渲染。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
)

// pricingPageRequest 挂上定价页并 GET 一次（真 Jet 渲染器 + 真模板）。
func pricingPageRequest(t *testing.T, h *productPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模板根目录相对包目录：internal/module/product/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/product-pricing", h.ProductPricingPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// TestProductPricingPageRendersThroughReadModel 正常档：渲染完整页，查询串里的工程 id 转发给读模型。
func TestProductPricingPageRendersThroughReadModel(t *testing.T) {
	reader := &productPageReaderStub{pricingView: productPricingPageViewStub(nil)}
	h := &productPageHandle{products: reader}

	rec := pricingPageRequest(t, h, "/admin/product-pricing?project=p1")
	if rec.Code != http.StatusOK {
		t.Fatalf("正常档应为 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	body := rec.Body.String()
	for _, want := range []string{"<html", "</body>", "</html>", "定价工具"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺 %q，body=%s", want, headOf(body))
		}
	}
	if got := rec.Header().Get("Cache-Control"); got == "no-store" {
		t.Fatal("正常档不该 no-store（这一屏没有取数失败档，缓存它没有风险）")
	}
	if len(reader.pricingReqs) != 1 {
		t.Fatalf("读模型应被调用一次，实际 %d 次", len(reader.pricingReqs))
	}
	if got := reader.pricingReqs[0].ProjectID; got != "p1" {
		t.Fatalf("查询串里的工程 id 没转发给读模型：%q", got)
	}
	// 首次 GET 不带提交值：nil = 用库里的默认初值（带空表单会把默认规则冲掉）。
	if got := reader.pricingReqs[0].Form; got != nil {
		t.Fatalf("首次 GET 不该带提交表单：%+v", got)
	}
	if got := reader.pricingReqs[0].Preview; got != nil {
		t.Fatalf("首次 GET 不该带试算结果：%+v", got)
	}
}

// TestProductPricingPageWithoutReadModelIsFiveHundred 装配缺陷：读模型没接上必须 500 + no-store。
//
// 反面判据：这一屏的取数各自降级，所以「静默给一张空参考表」在实现上最省事 —— 但那会让
// 「装配没接上」与「这个工程本来就没有规则」在页面上长得一模一样，而前者要有人去看装配。
func TestProductPricingPageWithoutReadModelIsFiveHundred(t *testing.T) {
	rec := pricingPageRequest(t, &productPageHandle{}, "/admin/product-pricing")
	assertProductPageFailure(t, "定价页读模型未注入", rec, http.StatusInternalServerError, "定价工具")
}
