package feature

import (
	"go_wp/internal/adminui"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	json "go_wp/pkg/jsonx"
)

func grantProductPerms(engine *gin.Engine) {
	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"product:create": true, "product:update": true,
			"product:variant_create": true, "product:variant_generate": true,
			"product:variant_delete": true, "product:delete": true,
			"product:rating_add": true, "product:rating_delete": true,
			"product:seo_score": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"product.create": true, "product.update": true,
			"product.variant_create": true, "product.variant_generate": true,
			"product.variant_delete": true, "product.delete": true})

		c.Next()
	})
}

func newVariantPageEngine(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handle := producthttp.NewProductPageHandle(f.svc, f.svc)
	engine.GET("/admin/products", pageview.UseRenderer(adminui.RenderPage), handle.ProductsPage)

	engine.GET("/admin/products/detail", pageview.UseRenderer(adminui.RenderPage), handle.ProductDetailPage)

	engine.GET("/admin/products/edit", pageview.UseRenderer(adminui.RenderPage), handle.ProductEditPage)
	return engine, f
}

func getProductsPage(engine *gin.Engine, projectID string) string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/products?project="+projectID, nil)
	engine.ServeHTTP(rec, req)
	return rec.Body.String()
}

func editLocation(projectID, productID string) string {
	return "/admin/products/edit?project=" + projectID + "&amp;product=" + productID
}

func getProductEditPage(engine *gin.Engine, projectID, productID string) string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/products/edit?project="+projectID+"&product="+productID, nil)
	engine.ServeHTTP(rec, req)
	return rec.Body.String()
}

func assertEditRedirect(t *testing.T, loc, projectID, productID string) {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("Location 无法解析：%v（%s）", err, loc)
	}
	if u.Path != "/admin/products/edit" {
		t.Fatalf("商品级写操作应回编辑页，实际 Location=%q", loc)
	}
	if u.Query().Get("project") != projectID || u.Query().Get("product") != productID {
		t.Fatalf("回跳应带该工程与该商品，实际 Location=%q", loc)
	}
}

func getProductDetailPage(engine *gin.Engine, projectID, productID string) string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/products/detail?project="+projectID+"&product="+productID, nil)
	engine.ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestProductsPageRendersVariantGeneratePanel(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	product := mkVariationProduct(t, f, "面板商品", "panel-product", []string{color.ID}, nil)

	body := getProductEditPage(engine, f.projectID, product.ID)
	for _, want := range []string{

		"生成组合", "生成勾选组合", "生成全部组合",
		`name="attr:` + color.ID + `"`,
		`value="` + color.Values[0].ID + `"`,
		`value="` + color.Values[1].ID + `"`,
		"参与变体", "规格",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}
}

func TestVariantGenerateSelectionsCreateCombinations(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	attr := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	product := mkVariationProduct(t, f, "流程商品", "flow-product", []string{attr.ID}, nil)

	if _, err := f.svc.GenerateVariants(t.Context(), &productdto.GenerateVariantsReq{
		ProjectID: f.projectID, ProductID: product.ID,
		Selections: []productdto.VariantSelectionReq{{
			AttributeID: attr.ID, ValueIDs: []string{attr.Values[0].ID, attr.Values[1].ID},
		}},
	}); err != nil {
		t.Fatalf("按勾选生成变体失败: %v", err)
	}
	detail, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: product.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if detail.VariantCount != 2 {
		t.Fatalf("勾选两个值应生成 2 个组合，实际 %d", detail.VariantCount)
	}
	detailBody := getProductDetailPage(engine, f.projectID, product.ID)
	display := productDisplayData(t, detailBody)
	if len(display.Attributes) != 1 || display.Attributes[0].ID != attr.ID || display.Attributes[0].Name != "颜色" || len(display.Variants) != 2 {
		t.Fatalf("页面缺少完整属性与变体数据：%+v", display)
	}
	labels := map[string]string{}
	for _, value := range display.Attributes[0].Values {
		labels[value.Key] = value.Label
	}
	if labels["red"] != "RED" || labels["blue"] != "BLUE" {
		t.Fatalf("属性值标签被丢失：%v", labels)
	}
	seen := map[string]bool{}
	for _, variant := range display.Variants {
		var values map[string]string
		if err := json.Unmarshal(variant.OptionValues, &values); err != nil {
			t.Fatal(err)
		}
		seen[values["color"]] = true
		if !strings.Contains(detailBody, `<span data-product-spec="`+variant.ID+`"></span>`) {
			t.Fatalf("变体 %s 缺少前端规格显示位置", variant.ID)
		}
	}
	if len(seen) != 2 || !seen["red"] || !seen["blue"] {
		t.Fatalf("页面变体组合不完整：%v", seen)
	}
}

func TestVariantGenerateEmptySelectionsExpandsAll(t *testing.T) {
	_, f := newVariantPageEngine(t)
	if f == nil {
		return
	}
	attr := mkVariationAttr(t, f, "尺寸", "size", []string{"s", "m"})
	product := mkVariationProduct(t, f, "全部生成商品", "all-mode", []string{attr.ID}, nil)

	if _, err := f.svc.GenerateVariants(t.Context(), &productdto.GenerateVariantsReq{
		ProjectID: f.projectID, ProductID: product.ID,
	}); err != nil {
		t.Fatalf("空选择生成失败: %v", err)
	}
	got, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: product.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if got.VariantCount != 2 {
		t.Fatalf("全部启用值应生成 2 个组合，实际 %d", got.VariantCount)
	}
}

func TestProductEditVariantsUseJSONWriteBase(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	attr := mkVariationAttr(t, f, "颜色", "color", []string{"red"})
	product := mkVariationProduct(t, f, "基座商品", "base-product", []string{attr.ID}, nil)

	body := getProductEditPage(engine, f.projectID, product.ID)
	for _, want := range []string{
		`hx-post="/api/product/variant/save"`,
		`data-write-builder="product-variant-rows"`,
		`data-write-reload`,
		`hx-post="/api/product/variant/create"`,
		`data-preview-url="/api/product/variant/preview"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("变体区缺少基座声明 %q", want)
		}
	}
	for _, bad := range []string{
		`action="/admin/products/variant/save"`,
		`action="/admin/products/variant/create"`,
		`action="/admin/products/variant/generate"`,
		`data-preview-url="/admin/products/variant/preview"`,
		`name="rows"`,
	} {
		if strings.Contains(body, bad) {
			t.Fatalf("变体区不应再出现旧提交形态 %q", bad)
		}
	}
}

func productDisplayData(t *testing.T, body string) *productdto.ProductResp {
	t.Helper()
	const opening = `<script type="application/json" id="product-display-data">`
	_, rest, found := strings.Cut(body, opening)
	if !found {
		t.Fatal("商品页面缺少显示数据")
	}
	raw, _, closed := strings.Cut(rest, "</script>")
	if !closed {
		t.Fatal("商品显示数据没有闭合")
	}
	var result productdto.ProductResp
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("商品显示数据不是原始 JSON：%v", err)
	}
	return &result
}
