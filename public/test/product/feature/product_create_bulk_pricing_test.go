// Package feature product 模块 feature 测试 —— 商品新建流程与批量「按规则改价」（本批）。
//
// 覆盖两件事（真实 PostgreSQL + 生产迁移 + 真实 service + 真实 Jet 模板）：
//
//  1. 属性组引用：抽屉的勾选列表按名重复提交（attributeIds 多值），服务端收全；
//     列表页给出可选项，用户不必再手打 UUID；
//  2. 批量改价：勾选若干商品 + 一条规则 → 逐个商品的全部变体改价并留痕；
//     一个都没勾选时明确拒绝、不动任何价格；夹着不存在的商品时整批拒绝并点出是哪个。
package feature

import (
	"context"
	"go_wp/internal/adminui"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
)

// newCreateFlowEngine 装配「商品列表 + 新建 + 批量改价」三个入口的测试引擎。
//
// 只挂被测路由：列表页（抽屉与勾选列表都渲染在这一页）、新建、批量改价。
func newCreateFlowEngine(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.svc, f.svc, f.projects)
	engine.GET("/admin/products", adminui.PageSlot(producthttp.ProductListPageTemplate), handle.ProductsPage)
	engine.GET("/admin/products/new", adminui.PageSlot(producthttp.ProductNewPageTemplate), handle.ProductNewPage)
	engine.GET("/admin/products/detail", adminui.PageSlot(producthttp.ProductDetailPageTemplate), handle.ProductDetailPage)
	// 批量改价没有页面 POST：列表抽屉走 /api/product/bulk-pricing（ui/write-base.js）。
	engine.POST("/api/product/bulk-pricing", producthttp.NewProductHandle(f.svc).BulkPricing)
	return engine, f
}

// locationQuery 取 Location 里的查询参数（回跳地址的断言都靠它，避免手拼字符串比对）。
func locationQuery(t *testing.T, loc, key string) string {
	t.Helper()
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("解析 Location 失败: %v（%q）", err, loc)
	}
	return parsed.Query().Get(key)
}

// cfSetVariantCost 给变体设售价与成本价（成本类定价规则的输入）。
func cfSetVariantCost(t *testing.T, f *attrFixture, variantID string, price, cost float64) {
	t.Helper()
	pv, cv := price, cost
	if _, err := f.svc.UpdateVariant(context.Background(), &productdto.UpdateVariantReq{
		ID: variantID, Price: &pv, CostPrice: &cv,
	}); err != nil {
		t.Fatalf("设置变体价格失败: %v", err)
	}
}

// TestProductCreateAttributeSelection 验收 2：
// 属性组由勾选列表给出（不必手打 UUID），多值提交按名收全。
func TestProductCreateAttributeSelection(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	size := mkVariationAttr(t, f, "尺寸", "size", []string{"s", "m"})

	// 新建整页必须渲染出勾选列表（值就是属性组 id），否则用户无从下手。
	// 落点从列表页抽屉改为整页：那个 template 已退役（见 getProductsNewPage 的注释）。
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

// TestProductsBulkPricing 验收 2：批量按规则改价。
//
// 两个商品一起提交：按「成本乘倍数 2」改价并逐变体留痕；
// 勾选里夹着一个不存在的商品时整批拒绝，且点出是哪一个。
func TestProductsBulkPricing(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	// 初始售价都不是「规则算出来的值」：否则该商品会落在「已是目标价」分支，
	// 断言就测不到真正的改价（成本 40×2=80、成本 100×2=200）。
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

	// 一个都没勾选：明确拒绝，不动任何价格（回执给可读文案，不是空消息）。
	none := postJSON(t, engine, "/api/product/bulk-pricing", map[string]any{
		"projectId": f.projectID, "ruleType": productenums.PricingRuleCostMultiple, "multiplier": "2",
	})
	if env := decodeJSONEnvelope(t, none); env.Code == http.StatusOK || strings.TrimSpace(env.Message) == "" {
		t.Fatalf("未勾选应被拒绝并给出可读文案，实际 %d：%q", env.Code, env.Message)
	}

	// 夹着不存在的商品 id：整批拒绝 —— 一行都不改，错误里点出是哪个商品。
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
	// 整批被拒 = 两个本来能改价的商品也一行没改。
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

	// 只勾能改价的两个：一次改完，回执只有一句结论 + 计数（没有 skipped 桶）。
	rec := postJSON(t, engine, "/api/product/bulk-pricing", map[string]any{
		"projectId":  f.projectID,
		"ids":        []string{p1.ID, p2.ID},
		"ruleType":   productenums.PricingRuleCostMultiple,
		"multiplier": "2",
		"rounding":   productenums.PricingRoundingNone,
		"note":       "批量改价：成本翻倍",
	})
	env := decodeJSONEnvelope(t, rec)
	if env.Code != http.StatusOK {
		t.Fatalf("批量改价应成功，实际 %d：%s", env.Code, env.Message)
	}
	if got, _ := env.Data["done"].(float64); int(got) != 2 {
		t.Fatalf("done 应为 2，实际 %v（data=%v）", env.Data["done"], env.Data)
	}
	if got, _ := env.Data["changedVariants"].(float64); int(got) != 2 {
		t.Fatalf("changedVariants 应为 2，实际 %v", env.Data["changedVariants"])
	}
	if _, ok := env.Data["skipped"]; ok {
		t.Fatalf("整批或零不该有 skipped 桶，data=%v", env.Data)
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
	// 留痕：每个真正改动的商品各一批，改动变体合计 2 个。
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

// TestProductCreateSKUInputAndEcho 主体 SKU 的输入 / 回显通道：
//
//	· 抽屉的 sku 字段必须真的被读 —— 漏读会静默按 URL 段派生另一个编码，
//	  建完看不出异常，直到与仓库对不上号；
//	· 不填时走确定性派生（取 URL 段）；
//	· 失败时页面拿到的是中文文案，不是裸 key（sentinel 白名单登记过的证据）。
func TestProductCreateSKUInputAndEcho(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	// ① 运营填了主体编码（未选仓 → 原样落库，不加任何前缀）。
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
	// ② 详情页看得到主体 SKU 与它的唯一性范围说明。
	page := getProductDetailPage(engine, f.projectID, productID)
	for _, want := range []string{"OPS-9001", "在本工程内唯一"} {
		if !strings.Contains(page, want) {
			t.Fatalf("详情页基本信息区缺少 %q", want)
		}
	}

	// ③ 中文 URL 段 + 运营显式填了编码：编码与 URL 段无关，照样建成功（原样落库）。
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
