// Package feature product 模块 feature 测试 —— 商品新建流程与批量「按规则改价」（本批）。
//
// 覆盖三件事（真实 PostgreSQL + 生产迁移 + 真实 service + 真实 Jet 模板）：
//
//  1. 新建商品：type 真的被读到（type=bundle 建出来就是捆绑容器、且没有变体），
//     成功后进**该商品的详情页**继续编辑（不是回列表）；
//     失败（bundle 没给容器价）回列表页并把中文结论经 ?err= 带回，不出现裸 key；
//  2. 属性组引用：抽屉的勾选列表按名重复提交（attributeIds 多值），服务端收全；
//     列表页给出可选项，用户不必再手打 UUID；
//  3. 批量改价：勾选若干商品 + 一条规则 → 逐个商品的全部变体改价并留痕；
//     捆绑容器（没有变体）被跳过且不静默；一个都没勾选时明确拒绝、不动任何价格。
package feature

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
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
	handle := producthttp.NewProductPageHandle(f.svc, f.projects)
	engine.GET("/admin/products", handle.ProductsPage)
	engine.GET("/admin/products/new", handle.ProductNewPage)
	engine.GET("/admin/products/detail", handle.ProductDetailPage)
	engine.POST("/admin/products/create", handle.ProductsCreate)
	engine.POST("/admin/products/bulk-pricing", handle.ProductsBulkPricing)
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

// TestProductCreateBundleTypeAndGoToDetail 验收 1：
// type=bundle 必须真的生效（否则会退化成 variant 并生成首个变体），成功去详情页。
func TestProductCreateBundleTypeAndGoToDetail(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	rec := postForm(engine, "/admin/products/create", url.Values{
		"projectId": {f.projectID}, "name": {"夏季套餐"}, "slug": {"summer-bundle"},
		"type": {"bundle"}, "defaultPrice": {"199"},
		// 捆绑主体 SKU 必填（2026-09-19）：抽屉会预填建议值，这里直接给一个。
		"sku": {"SUMMER-BUNDLE"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("新建应 302，实际 %d：%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	// 新建抽屉只有几个字段，全量录入在详情页 —— 创建成功必须直接落到那一页。
	if !strings.HasPrefix(loc, "/admin/products/detail?") {
		t.Fatalf("新建成功后应进该商品的详情页继续编辑，实际 Location=%q", loc)
	}
	if locProject := locationQuery(t, loc, "project"); locProject != f.projectID {
		t.Fatalf("详情页回跳丢了工程上下文：%q", loc)
	}
	productID := locationQuery(t, loc, "product")
	if productID == "" {
		t.Fatalf("详情页回跳缺少商品 id：%q", loc)
	}

	detail, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if detail.Type != productmodel.TypeBundle {
		t.Fatalf("type=bundle 建出来应为捆绑容器，实际 %q（漏读 type 会退化成 %q）",
			detail.Type, productmodel.TypeVariant)
	}
	if detail.VariantCount != 0 {
		t.Fatalf("捆绑容器不该有自己的变体，实际 %d 个", detail.VariantCount)
	}
	// 投影值之外再直接查一次变体表：判据落在「库里到底有没有那一行」上。
	var variantRows int64
	if err := f.db.Raw("SELECT COUNT(*) FROM product_variants WHERE product_id = ?", productID).
		Scan(&variantRows).Error; err != nil {
		t.Fatalf("查询变体行数失败: %v", err)
	}
	if variantRows != 0 {
		t.Fatalf("捆绑容器的变体表里不该有行，实际 %d 行", variantRows)
	}
}

// TestProductCreateBundleWithoutPriceReportsChinese 验收 1（失败路径）：
// 没给容器价的 bundle 被拒，且列表页拿到的是一句话，不是 ErrBundlePriceRequired 这个 key。
func TestProductCreateBundleWithoutPriceReportsChinese(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	rec := postForm(engine, "/admin/products/create", url.Values{
		"projectId": {f.projectID}, "name": {"无价套餐"}, "slug": {"no-price-bundle"},
		"type": {"bundle"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("被拒也应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/products?") {
		t.Fatalf("新建失败应回列表页（抽屉所在页），实际 Location=%q", loc)
	}
	msg := locationQuery(t, loc, "err")
	if !strings.Contains(msg, "捆绑商品必须自定价") {
		t.Fatalf("错误提示应是中文文案，实际 %q（原始 key 直出即 %s）", msg, productenums.ErrBundlePriceRequired)
	}
	if strings.Contains(msg, productenums.ErrBundlePriceRequired) {
		t.Fatalf("列表页不该出现裸 key，实际 %q", msg)
	}
	list, err := f.svc.List(t.Context(), &productdto.ListReq{ProjectID: f.projectID, Size: 100})
	if err != nil {
		t.Fatalf("读商品列表失败: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("被拒的商品不该落库，实际 %d 个", len(list))
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

	// 列表页必须渲染出勾选列表（值就是属性组 id），否则用户无从下手。
	page := getProductsPage(engine, f.projectID)
	for _, want := range []string{"name=\"attributeIds\"", "颜色（color）", "尺寸（size）"} {
		if !strings.Contains(page, want) {
			t.Fatalf("新建抽屉的属性组勾选列表缺少 %q", want)
		}
	}

	rec := postForm(engine, "/admin/products/create", url.Values{
		"projectId": {f.projectID}, "name": {"勾选商品"}, "slug": {"picked-product"},
		"type":         {"variant"},
		"attributeIds": {color.ID, size.ID},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("新建应 302，实际 %d：%s", rec.Code, rec.Body.String())
	}
	productID := locationQuery(t, rec.Header().Get("Location"), "product")
	detail, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
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

// TestProductsBulkPricing 验收 3：批量按规则改价。
//
// 两个变体商品 + 一个捆绑容器一起提交：前两个按「成本乘倍数 2」改价，
// 捆绑容器没有变体被跳过 —— 有跳过时走 ?err=（更显眼），且逐变体留痕。
func TestProductsBulkPricing(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	// 初始售价都不是「规则算出来的值」：否则该商品会落在「已是目标价」分支，
	// 断言就测不到真正的改价（成本 40×2=80、成本 100×2=200）。
	price1, price2, bundlePrice := 100.0, 210.0, 199.0
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
	bundle, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "套餐", Slug: "bulk-bundle",
		Type: productmodel.TypeBundle, DefaultPrice: &bundlePrice, SKUCode: "BULK-BUNDLE",
	})
	if err != nil {
		t.Fatalf("建捆绑容器失败: %v", err)
	}
	cfSetVariantCost(t, f, p1.Variants[0].ID, price1, 40)
	cfSetVariantCost(t, f, p2.Variants[0].ID, price2, 100)

	// 一个都没勾选：明确拒绝，不动任何价格。
	none := postForm(engine, "/admin/products/bulk-pricing", url.Values{
		"projectId": {f.projectID}, "ruleType": {productenums.PricingRuleCostMultiple}, "multiplier": {"2"},
	})
	if none.Code != http.StatusFound || !strings.Contains(locationQuery(t, none.Header().Get("Location"), "err"), "没有勾选") {
		t.Fatalf("未勾选应回列表页并明确提示，实际 %d %q", none.Code, none.Header().Get("Location"))
	}

	rec := postForm(engine, "/admin/products/bulk-pricing", url.Values{
		"projectId":  {f.projectID},
		"ids":        {p1.ID, p2.ID, bundle.ID},
		"ruleType":   {productenums.PricingRuleCostMultiple},
		"multiplier": {"2"},
		"rounding":   {productenums.PricingRoundingNone},
		"note":       {"批量改价：成本翻倍"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("批量改价应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/products?") {
		t.Fatalf("批量改价应回商品列表，实际 Location=%q", loc)
	}
	// 有跳过 → 走 err= 并说清跳过几个（不静默的部分成功）。
	msg := locationQuery(t, loc, "err")
	if !strings.Contains(msg, "1 个商品被跳过") || !strings.Contains(msg, "捆绑容器只有容器价") {
		t.Fatalf("捆绑容器没有变体应被计入跳过并说明原因，实际结论 %q", msg)
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
	// 捆绑容器的容器价不该被动：定价只改变体售价。
	gotBundle, serr := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: bundle.ID})
	if serr != nil {
		t.Fatalf("读捆绑容器失败: %v", serr)
	}
	if gotBundle.PriceMax != 199 {
		t.Fatalf("捆绑容器价不该被改价工具动到，实际 %v", gotBundle.PriceMax)
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
//	· 变体商品不填时走确定性派生（取 URL 段），捆绑商品不填则**明确拒绝**（必填，
//	  2026-09-19 用户拍板：编码必须被看见并确认，抽屉负责预填建议值）；
//	· 失败时页面拿到的是中文文案，不是裸 key（sentinel 白名单登记过的证据）。
func TestProductCreateSKUInputAndEcho(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	// ① 运营填了主体编码（未选仓 → 原样落库，不加任何前缀）。
	rec := postForm(engine, "/admin/products/create", url.Values{
		"projectId": {f.projectID}, "name": {"运营填编码"}, "slug": {"picked-sku"},
		"type": {"variant"}, "sku": {"OPS-9001"}, "defaultPrice": {"19.9"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("新建应 302，实际 %d：%s", rec.Code, rec.Body.String())
	}
	productID := locationQuery(t, rec.Header().Get("Location"), "product")
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

	// ③ 不填主体编码建捆绑：**明确拒绝**（2026-09-19 起不再静默按 URL 段派生 <商品段>_B）——
	// 抽屉会预填建议值让运营看见并确认，服务端不接受空值。
	bundleRec := postForm(engine, "/admin/products/create", url.Values{
		"projectId": {f.projectID}, "name": {"未填编码套餐"}, "slug": {"summer-set"},
		"type": {"bundle"}, "defaultPrice": {"99"},
	})
	if bundleRec.Code != http.StatusFound || !strings.HasPrefix(bundleRec.Header().Get("Location"), "/admin/products?") {
		t.Fatalf("没填编码的捆绑应被拒并回列表页，实际 %d %q", bundleRec.Code, bundleRec.Header().Get("Location"))
	}
	bundleMsg := locationQuery(t, bundleRec.Header().Get("Location"), "err")
	if !strings.Contains(bundleMsg, "必须填写主体 SKU") {
		t.Fatalf("拒绝理由应是中文文案，实际 %q", bundleMsg)
	}
	if strings.Contains(bundleMsg, productenums.ErrBundleSKURequired) {
		t.Fatalf("页面不该出现裸 key，实际 %q", bundleMsg)
	}

	// ④ 中文 URL 段 + 运营显式填了编码：编码与 URL 段无关，照样建成功（原样落库）。
	cn := postForm(engine, "/admin/products/create", url.Values{
		"projectId": {f.projectID}, "name": {"中文套餐"}, "slug": {"中文套餐"},
		"type": {"bundle"}, "defaultPrice": {"99"}, "sku": {"CN-BUNDLE"},
	})
	if cn.Code != http.StatusFound || !strings.HasPrefix(cn.Header().Get("Location"), "/admin/products/detail?") {
		t.Fatalf("填了编码的捆绑应建成功，实际 %d %q", cn.Code, cn.Header().Get("Location"))
	}
	cnID := locationQuery(t, cn.Header().Get("Location"), "product")
	bundle, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: cnID})
	if err != nil {
		t.Fatalf("读捆绑商品失败: %v", err)
	}
	if bundle.SKUCode != "CN-BUNDLE_B" {
		t.Fatalf("填了编码的捆绑应按规则补齐 _B 后缀得到 CN-BUNDLE_B，实际 %q", bundle.SKUCode)
	}
}
