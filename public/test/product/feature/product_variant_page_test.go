// Package feature product 模块 feature 测试 —— 后台组合生成页链路（issue #8）。
//
// 覆盖验收 1 与 2 的**后台页面部分**：
//
//	· GET /admin/products 渲染出组合生成面板（参与变体的属性组 + 其启用值）；
//	· POST /admin/products/variant/generate（mode=selected）按勾选生成组合，
//	  页面随之能看到每个变体的规格文本；
//	· mode 缺省且一个值都没勾选时**拒绝并提示**，不静默退化成「全部生成」；
//	· mode=all 走无表单路径（全部参与变体的组 × 全部启用值）。
//
// 用真实 Jet 模板渲染（与生产同一 template root），断言页面里到底有什么。
package feature

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
)

// grantProductPerms 让测试以「有写权限的用户」身份渲染页面。
//
// 页面上的写入口（新建变体 / 生成组合 / 添加评分）都由 PermSet 控制显隐，
// 而这套测试引擎只挂裸路由、没有权限中间件 —— 不注入的话这些按钮一律不渲染，
// 断言会失败在一个与业务无关的原因上。
func grantProductPerms(engine *gin.Engine) {
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{
			"product:create": true, "product:update": true,
			"product:variant_create": true, "product:variant_generate": true,
			"product:variant_delete": true, "product:delete": true,
		})
		c.Set(shell.ButtonsKey, map[string]bool{
			"product.create": true, "product.update": true,
			"product.variant_create": true, "product.variant_generate": true,
			"product.variant_delete": true, "product.delete": true,
		})

		c.Next()
	})
}

// newVariantPageEngine 装配一个只挂商品管理页的测试引擎（真实 Jet 模板 + 真实 service）。
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
	handle := producthttp.NewProductPageHandle(f.svc, f.projects)
	engine.GET("/admin/products", handle.ProductsPage)
	// 变体是商品的子资源，表格与生成入口都在商品详情页（列表页只回答「有哪些商品」）。
	engine.GET("/admin/products/detail", handle.ProductDetailPage)
	// 商品自身字段（名称 / URL 段 / SKU / 状态 / 价格 / SEO / 图集）与多语言入口在编辑页。
	engine.GET("/admin/products/edit", handle.ProductEditPage)
	engine.POST("/admin/products/update", handle.ProductsUpdate)
	engine.POST("/admin/products/variant/generate", handle.ProductsVariantGenerate)
	return engine, f
}

// getProductsPage 渲染商品管理页。
func getProductsPage(engine *gin.Engine, projectID string) string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/products?project="+projectID, nil)
	engine.ServeHTTP(rec, req)
	return rec.Body.String()
}

// editLocation 商品编辑页入口在**页面 HTML 里**的形态（Jet 把 & 转义成 &amp;）。
//
// 与 assertEditRedirect 的区别：那个比较的是 302 的 Location 头（未转义、参数按
// url.Values.Encode 的字母序），这个比较的是渲染出来的 href —— 两者形态不同，不能互相套用。
func editLocation(projectID, productID string) string {
	return "/admin/products/edit?project=" + projectID + "&amp;product=" + productID
}

// getProductEditPage 渲染商品编辑页 —— 商品自身字段、变体、评分、模板动作与多语言入口都在这里。
func getProductEditPage(engine *gin.Engine, projectID, productID string) string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/products/edit?project="+projectID+"&product="+productID, nil)
	engine.ServeHTTP(rec, req)
	return rec.Body.String()
}

// assertEditRedirect 断言一次商品级写操作回到了该商品的**编辑页**（PRG）。
//
// 为什么解析后比较、而不是比字符串前缀：handler 用 url.Values.Encode() 生成查询串
// （key 按字母序：product 在 project 之前），手拼的 "?project=..&product=.." 与它对不上，
// 前缀断言会以「回跳地址不对」的形式报出来，而真实行为是正确的。
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

// getProductDetailPage 渲染商品详情页 —— 只读页：变体 / 评分 / 捆绑构成都在这里，但没有写入口。
func getProductDetailPage(engine *gin.Engine, projectID, productID string) string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/products/detail?project="+projectID+"&product="+productID, nil)
	engine.ServeHTTP(rec, req)
	return rec.Body.String()
}

// TestProductsPageRendersVariantGeneratePanel 组合生成面板：
// 参与变体的属性组及其值必须以 checkbox 形式出现在页面里（name=attr:<组 id>）。
func TestProductsPageRendersVariantGeneratePanel(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	product := mkVariationProduct(t, f, "面板商品", "panel-product", []string{color.ID}, nil)

	// 组合生成的两个抽屉在**编辑页**（详情页只读）。
	body := getProductEditPage(engine, f.projectID, product.ID)
	for _, want := range []string{
		// 入口按钮文案是「生成组合」（行内不重复实体名），抽屉里是「生成勾选组合 / 生成全部组合」。
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

// TestProductsVariantGeneratePageFlow 页面写链路：
// 勾选后生成全部组合 → 页面显示每个变体的规格；没勾选 → 明确拒绝且不写变体。
func TestProductsVariantGeneratePageFlow(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	attr := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	product := mkVariationProduct(t, f, "流程商品", "flow-product", []string{attr.ID}, nil)

	// 默认 mode（=selected）但一个值都没勾选：拒绝，且不静默退化为全部生成。
	rec := postForm(engine, "/admin/products/variant/generate", url.Values{
		"projectId": {f.projectID}, "productId": {product.ID},
	})
	body := assertJumpErr(t, rec)
	// 被拒也留在编辑页（用户就在这一页操作），错误走提示页正文。
	assertEditRedirect(t, jumpBackHref(t, rec), f.projectID, product.ID)
	// 提示必须**可读**：这里原先断言 Location 里带 enums 裸 key（ErrVariationSelectionEmpty），
	// 而那正是「把 enums 常量铺到页面上」的形态（第三波 CQ-009 形态②）。
	// 现在 handler 统一经 productErrText 取词，断言的是中文文案 + 不再出现裸 key。
	if !strings.Contains(body, "未勾选任何属性值") {
		t.Fatalf("未勾选应提示「未勾选任何属性值」，body=%s", body)
	}
	if strings.Contains(body, productenums.ErrVariationSelectionEmpty) {
		t.Fatalf("提示页不应出现 enums 裸 key，body=%s", body)
	}
	if got, _ := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: product.ID}); got.VariantCount != 1 {
		t.Fatalf("拒绝时不应写入变体，实际 %d 个", got.VariantCount)
	}

	// 勾选两个值 → 生成两个组合。
	rec = postForm(engine, "/admin/products/variant/generate", url.Values{
		"projectId": {f.projectID}, "productId": {product.ID}, "mode": {"selected"},
		"attr:" + attr.ID: {attr.Values[0].ID, attr.Values[1].ID},
	})
	assertJumpOK(t, rec)
	assertEditRedirect(t, jumpBackHref(t, rec), f.projectID, product.ID)
	detail, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: product.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if detail.VariantCount != 2 {
		t.Fatalf("勾选两个值应生成 2 个组合，实际 %d", detail.VariantCount)
	}
	detailBody := getProductDetailPage(engine, f.projectID, product.ID)
	for _, want := range []string{"颜色 RED", "颜色 BLUE"} {
		if !strings.Contains(detailBody, want) {
			t.Fatalf("页面缺少规格文本 %q", want)
		}
	}
}

// TestProductsVariantGenerateAllMode mode=all 走无表单路径：不勾选也生成全部组合。
func TestProductsVariantGenerateAllMode(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	attr := mkVariationAttr(t, f, "尺寸", "size", []string{"s", "m"})
	product := mkVariationProduct(t, f, "全部生成商品", "all-mode", []string{attr.ID}, nil)

	rec := postForm(engine, "/admin/products/variant/generate", url.Values{
		"projectId": {f.projectID}, "productId": {product.ID}, "mode": {"all"},
	})
	assertJumpOK(t, rec)
	assertEditRedirect(t, jumpBackHref(t, rec), f.projectID, product.ID)
	got, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: product.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if got.VariantCount != 2 {
		t.Fatalf("mode=all 应生成 2 个组合，实际 %d", got.VariantCount)
	}
}
