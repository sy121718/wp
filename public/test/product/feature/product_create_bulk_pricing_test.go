package feature

import (
	"context"
	"go_wp/internal/adminui"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/text"
)

func newCreateFlowEngine(t *testing.T, middleware ...gin.HandlerFunc) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { pageview.FailScene(c, "template", err) }))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(middleware...)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.svc, f.svc)
	engine.GET("/admin/products", pageview.UseRenderer(adminui.RenderPage), handle.ProductsPage)
	engine.GET("/admin/products/new", pageview.UseRenderer(adminui.RenderPage), handle.ProductNewPage)
	engine.GET("/admin/products/detail", pageview.UseRenderer(adminui.RenderPage), handle.ProductDetailPage)

	write := producthttp.NewProductHandle(f.svc)
	engine.POST("/api/product/create", pageview.HTMLFragment, write.Create)
	engine.POST("/api/product/update", pageview.HTMLFragment, write.Update)
	engine.POST("/api/product/delete", pageview.HTMLFragment, write.Delete)
	engine.POST("/api/product/bulk-delete", pageview.HTMLFragment, write.BulkDelete)
	engine.POST("/api/product/rating/add", pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), write.AddRating)
	engine.POST("/api/product/rating/delete", pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), write.DeleteRating)
	engine.POST("/api/product/bulk-pricing", write.BulkPricing)
	engine.POST("/api/product/seo-score", pageview.HTMLFragment, handle.SeoScore)
	engine.POST("/api/product/category/seo-score", pageview.HTMLFragment, handle.CategorySeoScore)
	engine.POST("/api/product/brand/seo-score", pageview.HTMLFragment, handle.BrandSeoScore)
	return engine, f
}

func locationQuery(t *testing.T, loc, key string) string {
	t.Helper()
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("解析 Location 失败: %v（%q）", err, loc)
	}
	return parsed.Query().Get(key)
}

func cfSetVariantCost(t *testing.T, f *attrFixture, variantID string, price, cost float64) {
	t.Helper()
	pv, cv := price, cost
	if _, err := f.svc.UpdateVariant(context.Background(), &productdto.UpdateVariantReq{
		ID: variantID, Price: &pv, CostPrice: &cv,
	}); err != nil {
		t.Fatalf("设置变体价格失败: %v", err)
	}
}

func TestProductCreateAttributeSelection(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	size := mkVariationAttr(t, f, "尺寸", "size", []string{"s", "m"})

	page := getProductsNewPage(engine, f.projectID)
	for _, want := range []string{"name=\"attributeIds\"", "颜色（color）", "尺寸（size）"} {
		if !strings.Contains(page, want) {
			t.Fatalf("新建整页的属性组勾选列表缺少 %q", want)
		}
	}

	created, err := cfCreate(f, url.Values{
		"projectId": {f.projectID}, "name": {"勾选商品"}, "slug": {"picked-product"},
		"type":         {"variant"},
		"attributeIds": {color.ID, size.ID},
	})
	if err != nil {
		t.Fatalf("带属性组建商品失败: %v", err)
	}
	detail, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: created.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if len(detail.AttributeIDs) != 2 {
		t.Fatalf("勾选两个属性组应都收下，实际 %v", detail.AttributeIDs)
	}
	for _, want := range []string{color.ID, size.ID} {
		found := false
		for _, got := range detail.AttributeIDs {
			found = found || got == want
		}
		if !found {
			t.Fatalf("属性组 %s 没有落库，实际 %v", want, detail.AttributeIDs)
		}
	}
}

func TestProductsBulkPricing(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()

	price1, price2 := 100.0, 210.0
	p1, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "衬衫", Slug: "bulk-shirt", DefaultPrice: &price1,
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	p2, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "外套", Slug: "bulk-coat", DefaultPrice: &price2,
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	cfSetVariantCost(t, f, p1.Variants[0].ID, price1, 40)
	cfSetVariantCost(t, f, p2.Variants[0].ID, price2, 100)

	none := postJSON(t, engine, "/api/product/bulk-pricing", map[string]any{
		"projectId": f.projectID, "ruleType": productenums.PricingRuleCostMultiple, "multiplier": "2",
	})
	if env := decodeJSONEnvelope(t, none); env.Code == http.StatusOK || strings.TrimSpace(env.Message) == "" {
		t.Fatalf("未勾选应被拒绝并给出可读文案，实际 %d：%q", env.Code, env.Message)
	}

	const ghostID = "0b1f0d0c-0000-4000-8000-000000000000"
	blockedRec := postJSON(t, engine, "/api/product/bulk-pricing", map[string]any{
		"projectId":  f.projectID,
		"ids":        []string{p1.ID, p2.ID, ghostID},
		"ruleType":   productenums.PricingRuleCostMultiple,
		"multiplier": "2",
		"rounding":   productenums.PricingRoundingNone,
		"note":       "批量改价：成本翻倍",
	})
	benv := decodeJSONEnvelope(t, blockedRec)
	if benv.Code != http.StatusConflict {
		t.Fatalf("含不可处理条目应整批拒绝（409），实际 %d：%s", benv.Code, benv.Message)
	}
	blockedItems, _ := benv.Data["items"].([]any)
	seenGhost := false
	for _, raw := range blockedItems {
		row, _ := raw.(map[string]any)
		if id, _ := row["id"].(string); id != ghostID {
			continue
		}
		seenGhost = true
		if msg, _ := row["message"].(string); strings.TrimSpace(msg) == "" {
			t.Fatalf("条目应说清原因（不存在的商品），实际空消息")
		}
	}
	if !seenGhost {
		t.Fatalf("拒绝条目里应点出那个不存在的商品，items=%v", blockedItems)
	}

	for _, tc := range []struct {
		id   string
		want float64
		name string
	}{{p1.ID, price1, "衬衫"}, {p2.ID, price2, "外套"}} {
		got, serr := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: tc.id})
		if serr != nil {
			t.Fatalf("读商品失败: %v", serr)
		}
		if got.Variants[0].Price != tc.want {
			t.Fatalf("整批被拒时 %s 的价格不该被改，实际 %v（原 %v）", tc.name, got.Variants[0].Price, tc.want)
		}
	}

	rec := postJSON(t, engine, "/api/product/bulk-pricing", map[string]any{
		"projectId":  f.projectID,
		"ids":        []string{p1.ID, p2.ID},
		"ruleType":   productenums.PricingRuleCostMultiple,
		"multiplier": "2",
		"rounding":   productenums.PricingRoundingNone,
		"note":       "批量改价：成本翻倍",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("批量改价应返回 HTML 成功片段，实际 %d：%s", rec.Code, rec.Body.String())
	}

	got1, serr := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p1.ID})
	if serr != nil {
		t.Fatalf("读商品失败: %v", serr)
	}
	got2, serr := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p2.ID})
	if serr != nil {
		t.Fatalf("读商品失败: %v", serr)
	}
	if got1.Variants[0].Price != 80 {
		t.Fatalf("成本 40 × 2 应为 80，实际 %v", got1.Variants[0].Price)
	}
	if got2.Variants[0].Price != 200 {
		t.Fatalf("成本 100 × 2 应为 200，实际 %v", got2.Variants[0].Price)
	}

	history, herr := f.svc.ListPriceAdjustments(ctx, &productdto.ListPriceAdjustmentReq{
		ProjectID: f.projectID, Limit: 20,
	})
	if herr != nil {
		t.Fatalf("读调价留痕失败: %v", herr)
	}
	if len(history) == 0 {
		t.Fatalf("批量改价必须留痕，实际一条都没有")
	}
	changed := 0
	for _, h := range history {
		changed += h.ChangedCount
	}
	if changed != 2 {
		t.Fatalf("留痕里的改动变体应合计 2 个，实际 %d", changed)
	}
}

func TestProductCreateSKUInputAndEcho(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}

	created, err := cfCreate(f, url.Values{
		"projectId": {f.projectID}, "name": {"运营填编码"}, "slug": {"picked-sku"},
		"type": {"variant"}, "sku": {"OPS-9001"}, "defaultPrice": {"19.9"},
	})
	if err != nil {
		t.Fatalf("填了主体编码应建成功: %v", err)
	}
	productID := created.ID
	detail, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if detail.SKUCode != "OPS-9001" {
		t.Fatalf("抽屉填的主体编码应原样落库（未选仓不加前缀），实际 %q", detail.SKUCode)
	}

	page := getProductDetailPage(engine, f.projectID, productID)
	for _, want := range []string{"OPS-9001", "在本工程内唯一"} {
		if !strings.Contains(page, want) {
			t.Fatalf("详情页基本信息区缺少 %q", want)
		}
	}

	cn, err := cfCreate(f, url.Values{
		"projectId": {f.projectID}, "name": {"中文商品"}, "slug": {"中文商品"},
		"type": {"variant"}, "defaultPrice": {"99"}, "sku": {"CN-SKU"},
	})
	if err != nil {
		t.Fatalf("填了编码的商品应建成功: %v", err)
	}
	named, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: cn.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if named.SKUCode != "CN-SKU" {
		t.Fatalf("填了编码应原样落库得到 CN-SKU，实际 %q", named.SKUCode)
	}
}
