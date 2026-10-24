// product_bundle_sku_required_test.go — 捆绑商品主体 SKU 必填（2026-09-19 用户拍板）。
//
// 上一版是「捆绑未填 SKU → 静默按 <商品段>_B 派生」，被否掉：编码是商品的对外身份，
// 必须让运营**看见并确认**。本文件钉住改动后的行为（真实 PostgreSQL + 生产迁移 +
// 真实 service + 真实 Jet 模板）：
//
//  1. POST 建 bundle 不带 sku → 302 回列表页，err 是一句中文（不是裸 key、也不是
//     「系统内部错误」），且商品不落库；
//  2. POST 建 bundle 带 sku=XX_1 → 落库为 XX_1_B（缺 _B 后缀由服务端补齐）；
//  3. 变体商品保持原状：不带 sku 仍可留空派生（本次只改捆绑）；
//  4. Update 路径只在**显式带 sku 键**时校验：nil 不改存量、显式空串仍拒。
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
	productmodel "go_wp/internal/module/product/model"
	"go_wp/internal/templates"
)

// newBundleSKUEngine 装配「列表 + 详情 + 新建 + 分类品牌」四个入口的测试引擎。
//
// 带上 /api/product/update 是为了走一条**只带其它字段、不带 sku** 的真实页面更新路径
// （UpdateReq.SKUCode 为 nil）——「捆绑必填」不该拦住任何不改 SKU 的更新。
func newBundleSKUEngine(t *testing.T) (*gin.Engine, *attrFixture) {
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
	// 挂分类 / 品牌没有页面 POST：编辑页表单走 /api/product/update（ui/write-base.js）。
	engine.POST("/api/product/update", producthttp.NewProductHandle(f.svc).Update)
	return engine, f
}

// bsCreateBundle 用新建表单的字段口径建一个捆绑商品（页面提交已迁 JSON 写基座，这里直调 service）。
func bsCreateBundle(t *testing.T, f *attrFixture, form url.Values) (*productdto.ProductResp, error) {
	t.Helper()
	base := url.Values{"projectId": {f.projectID}, "name": {"捆绑套餐"}, "type": {"bundle"}, "defaultPrice": {"199"}}
	for key, values := range form {
		base[key] = values
	}
	return cfCreate(f, base)
}

// TestBundleCreateWithoutSKUIsRejectedAndKeepsNothing 验收 1：
// 不带 sku 建 bundle 被 service 直接拒 —— 商品不落库，错误是能归口的业务 key。
//
// 中文文案不在这里断言：页面提交已改 JSON 写基座，错误文案由端点的白名单产出
// （product_err_fallback_tables_test.go 钉住 key → 文案），这里钉的是「会不会被拒」。
func TestBundleCreateWithoutSKUIsRejectedAndKeepsNothing(t *testing.T) {
	_, f := newBundleSKUEngine(t)
	if f == nil {
		return
	}
	if _, err := bsCreateBundle(t, f, url.Values{"slug": {"no-sku-bundle"}}); err == nil ||
		err.Error() != productenums.ErrBundleSKURequired {
		t.Fatalf("建捆绑不带 sku 应返回 %s，实际 %v", productenums.ErrBundleSKURequired, err)
	}
	// 被拒的商品不落库：静默半截状态（商品在、编码空）是最难排查的一种。
	list, err := f.svc.List(t.Context(), &productdto.ListReq{ProjectID: f.projectID, Size: 100})
	if err != nil {
		t.Fatalf("读商品列表失败: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("被拒的捆绑不该落库，实际 %d 个", len(list))
	}
}

// TestBundleCreateWithSKUKeepsValueAndSuffix 验收 2：
// 填了就不动它的本体 —— 只补恒定的 _B 后缀（XX_1 → XX_1_B；XX_B 原样）。
func TestBundleCreateWithSKUKeepsValueAndSuffix(t *testing.T) {
	_, f := newBundleSKUEngine(t)
	if f == nil {
		return
	}
	cases := []struct{ input, want string }{
		// 缺 _B 后缀：服务端补齐（本票的「保留」行为，不是静默派生）。
		{"XX_1", "XX_1_B"},
		{"XX_B", "XX_B"},
		// 小写后缀也算带上（大小写不敏感）。
		{"gift_b", "gift_b"},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			created, err := bsCreateBundle(t, f, url.Values{
				"slug": {"bundle-" + strings.ToLower(strings.ReplaceAll(c.input, "_", "-"))},
				"sku":  {c.input},
			})
			if err != nil {
				t.Fatalf("带 sku 的捆绑应建成功: %v", err)
			}
			detail, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: created.ID})
			if err != nil {
				t.Fatalf("读商品失败: %v", err)
			}
			if detail.SKUCode != c.want {
				t.Fatalf("主体 SKU 应为 %q，实际 %q", c.want, detail.SKUCode)
			}
			if detail.Type != productmodel.TypeBundle {
				t.Fatalf("类型应为 bundle，实际 %q", detail.Type)
			}
		})
	}
}

// TestVariantCreateWithoutSKUStillDerives 验收 3：变体商品保持现状 —— 不带 sku 可留空派生。
func TestVariantCreateWithoutSKUStillDerives(t *testing.T) {
	_, f := newBundleSKUEngine(t)
	if f == nil {
		return
	}
	created, err := cfCreate(f, url.Values{
		"projectId": {f.projectID}, "name": {"留空主体"}, "slug": {"variant-empty"},
		"type": {"variant"}, "defaultPrice": {"19.9"},
	})
	if err != nil {
		t.Fatalf("变体商品不带 sku 仍应建成功: %v", err)
	}
	detail, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: created.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if detail.SKUCode != "VARIANTEMPTY" {
		t.Fatalf("变体商品留空应按 URL 段派生 VARIANTEMPTY，实际 %q", detail.SKUCode)
	}
}

// TestBundleUpdateWithoutSKUKeyIsNotBlocked 验收 4：更新路径只在显式带 sku 键时校验。
//
// 三条分支都钉住：
//   - 只带别的字段（SKUCode 为 nil）→ 照常成功，存量编码一个字不改；
//     HTTP 侧走 /api/product/update（编辑页表单的真实路径，形态与分类品牌抽屉一致）；
//   - 显式带 sku → 按捆绑规则补齐后缀；
//   - 显式带空串 → 仍然明确拒绝（ErrContainerSkuInvalid），而不是被必填错误吞掉。
func TestBundleUpdateWithoutSKUKeyIsNotBlocked(t *testing.T) {
	engine, f := newBundleSKUEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	created, err := bsCreateBundle(t, f, url.Values{"slug": {"update-bundle"}, "sku": {"KEEP_B"}})
	if err != nil {
		t.Fatalf("建捆绑失败: %v", err)
	}
	productID := created.ID

	// ① HTTP 路径：不带 sku 键的更新不被必填规则拦住。
	upd := postJSON(t, engine, "/api/product/update", map[string]any{
		"projectId": f.projectID, "id": productID, "primaryCategoryId": "", "brandId": "",
	})
	// 成功 → 200 回执；不改 SKU 的更新不该被必填规则拦住。
	if got := decodeJSONEnvelope(t, upd); got.Code != http.StatusOK {
		t.Fatalf("不带 sku 键的更新应成功，实际 %d：%s", got.Code, got.Message)
	}
	detail, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if detail.SKUCode != "KEEP_B" {
		t.Fatalf("不带 sku 键的更新不该动存量编码，实际 %q", detail.SKUCode)
	}

	// ② 显式带 sku：走捆绑规则（缺 _B 后缀补齐）。
	next := "RENAMED_1"
	if _, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: productID, ProjectID: f.projectID, SKUCode: &next,
	}); err != nil {
		t.Fatalf("显式改主体编码不应失败: %v", err)
	}
	detail, err = f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if detail.SKUCode != "RENAMED_1_B" {
		t.Fatalf("显式改编码应补齐 _B 后缀得到 RENAMED_1_B，实际 %q", detail.SKUCode)
	}

	// ③ 显式空串：拒绝（沿用 ErrContainerSkuInvalid，不被必填错误顶替 —— 两者的可行动提示不同）。
	blank := "   "
	if _, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: productID, ProjectID: f.projectID, SKUCode: &blank,
	}); err == nil || err.Error() != productenums.ErrContainerSkuInvalid {
		t.Fatalf("显式空串应返回 %s，实际 %v", productenums.ErrContainerSkuInvalid, err)
	}
	detail, err = f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if detail.SKUCode != "RENAMED_1_B" {
		t.Fatalf("被拒的更新不该动库里的编码，实际 %q", detail.SKUCode)
	}
}

// TestBundleCreateServiceLevelRequiresSKU 服务层直连：捆绑不填主体 SKU 一律 ErrBundleSKURequired。
//
// 页面路径只是一层；导入 / 接口 / 脚本同样会让服务端在「运营没看见编码」的情况下建商品，
// 所以必填必须落在 service，而不是只靠抽屉的 required 属性。
func TestBundleCreateServiceLevelRequiresSKU(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	price := 199.0
	if _, err := f.svc.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "无编码套餐", Slug: "sku-required-bundle",
		Type: productmodel.TypeBundle, DefaultPrice: &price,
	}); err == nil || err.Error() != productenums.ErrBundleSKURequired {
		t.Fatalf("服务层建捆绑不带 sku 应返回 %s，实际 %v", productenums.ErrBundleSKURequired, err)
	}
}

// TestProductsDrawerCarriesBundleSKUEnhancement 抽屉侧渲染：
// 新建商品抽屉必须带上增强脚本依赖的那几个钩子（改版后漏一个，页面不会报错 ——
// 特征只是「预填不出现」，很难从截图上判断，所以在这里钉住）。
func TestProductsDrawerCarriesBundleSKUEnhancement(t *testing.T) {
	engine, f := newBundleSKUEngine(t)
	if engine == nil {
		return
	}
	// 落点是**新建整页**：建表单的抽屉已退役（列表页那个 template 没有任何 data-drawer-open
	// 指向它），片段与字段一个没少，只是渲染它的页面变成了 /admin/products/new。
	page := getProductsNewPage(engine, f.projectID)
	for _, want := range []string{
		"data-product-create-form",          // 脚本按它定位抽屉表单（按 action 匹配会被后续改动悄悄改掉）
		"data-sku-input",                    // 主体 SKU 输入框
		"data-sku-regenerate",               // 「重新生成」按钮
		"data-sku-placeholder-bundle",       // 捆绑态占位符（变体态回落到原来的 placeholder 文案）
		"data-sku-hint",                     // 「系统建议的唯一身份编码，可直接修改」
		"data-sku-manual",                   // 「请手填」提示行
		"这是系统建议的唯一身份编码",                     // 兜底文案真的渲染出来了（t() 未接 i18n 时也要有）
		"/static/js/product-create-form.js", // 增强已提升为共享脚本（抽屉与新建整页共用同一份，内联副本会分叉）
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("新建抽屉缺少 %q", want)
		}
	}
}
