// product_detail_bundle_panel_test.go — 商品详情页的「捆绑构成」区块（issue #20 与商品类型收口）。
//
// 走真实链路：真实 PostgreSQL + 生产迁移 + 真实 product / inventory service + 真实 Jet 模板，
// 断言的是「详情页到底吐出了什么 HTML」。覆盖：
//
//	· type=bundle 的商品在详情页看到成员清单（SKU / 所属商品 / 必选 / 数量 / 可用量）
//	  与后台价格口径（成员成本、成员挂牌价标注「不参与套餐价」）；
//	· 区块里的入口真的能落到配置器页面并选中同一个商品（参数名 projectId / productId）；
//	· 空配置、成员 SKU 已停用、成员 SKU 已被删除都有可读提示，不静默丢项；
//	· type=variant 的商品没有这一块（越权渲染比缺失更难发现）。
package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
)

// mkBundleProduct 建一个捆绑容器（type=bundle，只有容器价，不生成自己的首个变体）。
//
// 主体 SKU 必填（2026-09-19）：这里用与抽屉同一套派生口径给出建议值 ——
// 商品段 = slug 的大写字母数字 + _B（slug 是 ASCII，不必走 JS 那套 Unicode 处理）。
func (f *bundleFixture) mkBundleProduct(t *testing.T, name, slug string, price float64) *productdto.ProductResp {
	t.Helper()
	res, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug, Type: "bundle", DefaultPrice: &price,
		SKUCode: strings.ToUpper(strings.ReplaceAll(slug, "-", "")) + "_B",
	})
	if err != nil {
		t.Fatalf("建捆绑商品 %s 失败: %v", name, err)
	}
	return res
}

// newBundleDetailEngine 装配一个只挂商品详情页与捆绑配置页的测试引擎。
func newBundleDetailEngine(t *testing.T) (*gin.Engine, *bundleFixture) {
	t.Helper()
	f := newBundleFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	engine.GET("/admin/products/detail", handle.ProductDetailPage)
	engine.GET("/admin/products/bundle", handle.ProductBundlePage)
	return engine, f
}

// TestProductDetailBundleComposition 详情页按类型渲染捆绑构成，入口能落到配置器页面。
func TestProductDetailBundleComposition(t *testing.T) {
	engine, f := newBundleDetailEngine(t)
	if engine == nil {
		return
	}
	container := f.mkBundleProduct(t, "家庭套餐", "family-bundle", 199)
	memberPrice := 45.5
	member := f.mkProduct(t, "配件包", "addon-pack", &memberPrice)
	v := f.firstVariant(t, member.ID)
	f.addStock(t, v, 6)
	// 成本在入库之后写：入库链路会按单价回写成本，顺序反了这条断言会被覆盖。
	cost := 20.0
	if _, err := f.products.UpdateVariant(context.Background(), &productdto.UpdateVariantReq{
		ID: v.ID, ProjectID: f.projectID, CostPrice: &cost,
	}); err != nil {
		t.Fatalf("写变体成本失败: %v", err)
	}
	f.setBundleConfig(t, container.ID, bundleWith(v.ID, true, 2, 1, 3, 0, 0))

	body := productDetailBody(t, engine, f.projectID, container.ID)
	for _, want := range []string{
		"捆绑构成",
		"/admin/products/bundle?projectId=" + f.projectID + "&amp;productId=" + container.ID,
		"容器价（套餐价）", "199",
		v.SKUCode, "配件包", "必选",
		"成员成本（后台口径）", "成员挂牌价（参考 · 不参与套餐价）",
		"45.5", "20", // 成员挂牌价（参考）与成员成本（后台口径）
		"不限", // 最大数量填 0 = 不设上限，不是「最大 0 件」
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("详情页缺少 %q\n%s", want, oneLine(body))
		}
	}
	// 数量与可用量（可用量 6 来自库存真源，不是商品侧缓存）。
	for _, want := range []string{"<td>2</td>", "<td>1</td>", "<td>3</td>", "<td>6</td>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("详情页缺少成员数量 / 可用量 %q\n%s", want, body)
		}
	}

	// 入口落到配置器页面时必须已选中同一个商品：否则点进去是一张空骨架，
	// 而页面上看不出哪里不对。
	entry := httptestGET(t, engine, "/admin/products/bundle?projectId="+f.projectID+"&productId="+container.ID)
	if !strings.Contains(entry, "value=\""+container.ID+"\" selected") {
		t.Fatalf("配置页应预选中该商品\n%s", oneLine(entry))
	}
	if !strings.Contains(entry, "保存配置") {
		t.Fatalf("配置页应渲染选项规则表单（配置读取成功）\n%s", oneLine(entry))
	}

	// type=variant 的商品没有这一块，页面其余部分照常渲染。
	memberBody := productDetailBody(t, engine, f.projectID, member.ID)
	if strings.Contains(memberBody, "捆绑构成") {
		t.Fatalf("variant 商品的详情页不该出现捆绑构成")
	}
	if !strings.Contains(memberBody, "各仓库存") {
		t.Fatalf("variant 商品的详情页被截断（变体表缺失）")
	}
}

// TestProductDetailBundlePanelEmpty 空配置给出可读提示与配置入口，而不是空白表格。
func TestProductDetailBundlePanelEmpty(t *testing.T) {
	engine, f := newBundleDetailEngine(t)
	if engine == nil {
		return
	}
	container := f.mkBundleProduct(t, "空套餐", "empty-bundle", 88)
	body := productDetailBody(t, engine, f.projectID, container.ID)
	for _, want := range []string{
		"捆绑构成", "还没有配置捆绑构成",
		"/admin/products/bundle?projectId=" + f.projectID + "&amp;productId=" + container.ID,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("空配置的详情页缺少 %q\n%s", want, oneLine(body))
		}
	}
}

// TestProductDetailBundlePanelDisabledAndDeleted 成员被停用 / 被删除都在列表里说清楚。
func TestProductDetailBundlePanelDisabledAndDeleted(t *testing.T) {
	engine, f := newBundleDetailEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	container := f.mkBundleProduct(t, "两成员套餐", "two-member-bundle", 128)
	offMember := f.mkProduct(t, "停用配件", "off-addon", nil)
	offVariant := f.firstVariant(t, offMember.ID)
	deadMember := f.mkProduct(t, "待删配件", "dead-addon", nil)
	deadVariant := f.firstVariant(t, deadMember.ID)
	f.setBundleConfig(t, container.ID, productdto.BundleConfig{
		MaxOptions: productdto.BundleDefaultMaxOptions,
		Options: []productdto.BundleOption{
			{VariantID: offVariant.ID, Required: true, DefaultQty: 1, MinQty: 1},
			{VariantID: deadVariant.ID, Required: false, DefaultQty: 1, MinQty: 0},
		},
	})

	// 停用：SKU 仍在列表里，标注停用（不是静默消失）。
	disabled := false
	if _, err := f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{
		ID: offVariant.ID, ProjectID: f.projectID, Enabled: &disabled,
	}); err != nil {
		t.Fatalf("停用变体失败: %v", err)
	}
	body := productDetailBody(t, engine, f.projectID, container.ID)
	if !strings.Contains(body, offVariant.SKUCode) || !strings.Contains(body, "停用") {
		t.Fatalf("已停用的成员应仍列出并标注停用\n%s", oneLine(body))
	}

	// 删除变体：**批次 C 起被捆绑成员引用守卫拦下** —— 成员的身份是 variantId，
	// 硬删它会让这个套餐指向一个不存在的变体（守卫的四个引用面见
	// product_variant_delete_guard_test.go）。
	if err := f.products.DeleteVariant(ctx, &productdto.DeleteVariantReq{
		ID: deadVariant.ID, ProjectID: f.projectID,
	}); err == nil || !strings.Contains(err.Error(), productenums.VariantSkipBundleReferenced) {
		t.Fatalf("被捆绑引用的变体应拒绝删除（%s），实际 %v", productenums.VariantSkipBundleReferenced, err)
	}

	// 「配置指向已不存在的变体」这条渲染路径仍然存在：删掉**整个成员商品**会级联删掉它的变体
	//（守卫管的是变体硬删，不是商品级联 —— 商品删除是另一条受控路径）。
	// 配置里于是只剩变体 id，页面照常出一行并标注已删除（运维才有线索去修配置）。
	if err := f.products.Delete(ctx, &productdto.DeleteReq{ID: deadMember.ID, ProjectID: f.projectID}); err != nil {
		t.Fatalf("删除成员商品失败: %v", err)
	}
	body = productDetailBody(t, engine, f.projectID, container.ID)
	for _, want := range []string{"成员 SKU 已删除", deadVariant.ID, offVariant.SKUCode} {
		if !strings.Contains(body, want) {
			t.Fatalf("已删除 / 已停用的成员都不能静默丢项，缺少 %q\n%s", want, oneLine(body))
		}
	}
}

// httptestGET 发一个 GET 并断言 200，返回 HTML。
func httptestGET(t *testing.T, engine *gin.Engine, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s 应 200，实际 %d：%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
