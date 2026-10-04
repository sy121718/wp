// product_bundle_page_test.go — 捆绑配置器片段与后台配置页（issue #20 验收 3/7/8）。
//
// 片段走真实的 runtimefragment 端点（白名单 + 方法匹配 + 参数收集），
// 页面走真实的 Jet 模板渲染：断言的是「访问面到底吐出了什么 HTML」，不是内部状态。
package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	runtimefragment "go_wp/internal/module/runtimefragment"
	"go_wp/internal/templates"
)

// oneLine 把多行 HTML 压成一行（失败信息里能看全，grep 也只截一行）。
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// newBundleFragmentEngine 装配只挂片段端点的测试引擎（真实 service + 真实模板）。
func newBundleFragmentEngine(t *testing.T) (*gin.Engine, *bundleFixture) {
	t.Helper()
	f := newBundleFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 不接管还原函数：注入保留到进程结束，与逐个 setter 时代相同。
	runtimefragment.MutateDepsForTest(func(d *runtimefragment.Deps) { d.BundleProvider = f.products })
	runtimefragment.SetupFragmentRoutes(engine)
	return engine, f
}

// TestBundleConfiguratorFragment 验收 3/7：
// 配置器与整单校验都在 runtimefragment 端点上——片段渲染选项、实时反馈上下限与可用量，
// 非法选择返回**页面提示**（200）而不是 500。
func TestBundleConfiguratorFragment(t *testing.T) {
	engine, f := newBundleFragmentEngine(t)
	if engine == nil {
		return
	}
	mainPrice := 129.0
	main := f.mkProduct(t, "片段套餐", "fragment-bundle", &mainPrice)
	addon := f.mkProduct(t, "片段子项", "fragment-addon", nil)
	v := f.firstVariant(t, addon.ID)
	// 库存只给 2 件：合法选择 2 件、超量 3 件都要能分辨出来（配置的单项上限放到 3，
	// 让「超库存」而不是「超单项上限」成为被拒原因）。
	f.addStock(t, v, 2)
	f.setBundleConfig(t, main.ID, bundleWith(v.ID, true, 1, 1, 3, 2, 5))

	// GET 渲染配置器：SKU、必选标记、可用量、套餐价、HTMX 属性都要在。
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_fragments/bundleConfigurator?productId="+main.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("配置器片段应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		v.SKUCode, "必选", "可用 2", "套餐价 129.00",
		`hx-post="/_fragments/bundleConfiguratorCheck"`,
		`name="variantId"`, `name="qty"`, `name="productId"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("配置器片段缺少 %q\n%s", want, body)
		}
	}

	// POST 合法选择 → 结论片段含套餐价与总件数。
	ok := postForm(engine, "/_fragments/bundleConfiguratorCheck", url.Values{
		"productId": {main.ID}, "variantId": {v.ID}, "qty": {"2"},
	})
	if ok.Code != http.StatusOK {
		t.Fatalf("校验片段应 200，实际 %d", ok.Code)
	}
	if got := ok.Body.String(); !strings.Contains(got, "套餐价 129.00") || !strings.Contains(got, "共 2 件") {
		t.Fatalf("合法选择的结论片段应含套餐价与总件数，实际 %s", got)
	}
	if !strings.Contains(ok.Body.String(), "role=\"status\"") {
		t.Fatalf("成功结论应带 role=status（无障碍播报），实际 %s", ok.Body.String())
	}

	// POST 非法选择（低于整单下限）→ 200 + 可读提示，不是 500。
	bad := postForm(engine, "/_fragments/bundleConfiguratorCheck", url.Values{
		"productId": {main.ID}, "variantId": {v.ID}, "qty": {"1"},
	})
	if bad.Code != http.StatusOK {
		t.Fatalf("非法选择也应是 200 + 提示，实际 %d", bad.Code)
	}
	if !strings.Contains(bad.Body.String(), "整单总件数未达到最小购买数量") {
		t.Fatalf("应提示整单下限，实际 %s", bad.Body.String())
	}
	if !strings.Contains(bad.Body.String(), "role=\"alert\"") {
		t.Fatalf("失败结论应带 role=alert，实际 %s", bad.Body.String())
	}

	// 超可用量 → 提示库存不足（只读真源）。
	over := postForm(engine, "/_fragments/bundleConfiguratorCheck", url.Values{
		"productId": {main.ID}, "variantId": {v.ID}, "qty": {"3"},
	})
	if over.Code != http.StatusOK || !strings.Contains(over.Body.String(), "超过了当前可用库存") {
		t.Fatalf("超库存应给可读提示，实际 %d %s", over.Code, oneLine(over.Body.String()))
	}

	// 非整数数量 → 参数提示（不是 500）。
	nan := postForm(engine, "/_fragments/bundleConfiguratorCheck", url.Values{
		"productId": {main.ID}, "variantId": {v.ID}, "qty": {"abc"},
	})
	if nan.Code != http.StatusOK || !strings.Contains(nan.Body.String(), "数量必须是整数") {
		t.Fatalf("非整数数量应给可读提示，实际 %d %s", nan.Code, nan.Body.String())
	}

	// 未知能力 404；GET 能力不接受 POST（方法不匹配 405）。
	notFound := httptest.NewRecorder()
	engine.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/_fragments/noSuchCapability", nil))
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("未知能力应 404，实际 %d", notFound.Code)
	}
	wrongMethod := postForm(engine, "/_fragments/bundleConfigurator", url.Values{"productId": {main.ID}})
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET 能力不接受 POST，应 405，实际 %d", wrongMethod.Code)
	}
}

// TestBundleConfiguratorResponsiveContract 验收 8：
// 产出必须适配桌面 / 平板 / 手机与鼠标 / 滚轮 / 触屏 / 键盘 ——
// 片段里要有折行容器、窄屏媒体查询、宽度上限与数字输入（触屏用 numeric 键盘）。
func TestBundleConfiguratorResponsiveContract(t *testing.T) {
	engine, f := newBundleFragmentEngine(t)
	if engine == nil {
		return
	}
	price := 66.0
	main := f.mkProduct(t, "多端套餐", "responsive-bundle", &price)
	addon := f.mkProduct(t, "多端子项", "responsive-addon", nil)
	v := f.firstVariant(t, addon.ID)
	f.addStock(t, v, 5)
	f.setBundleConfig(t, main.ID, bundleWith(v.ID, true, 1, 1, 0, 1, 0))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_fragments/bundleConfigurator?productId="+main.ID, nil))
	body := rec.Body.String()
	for _, want := range []string{
		"flex-wrap",                           // 桌面 / 平板 / 手机都由容器自己折行，不靠固定宽度
		"@media (max-width:520px)",            // 窄屏的确切断点
		"min(100%",                            // 宽度不写死（规范要求 min(100%, <设计宽度>)）
		`type="number"`,                       // 键盘与数字键盘输入
		`inputmode="numeric"`,                 // 触屏弹出数字键盘
		`step="1"`,                            // 只接受整数件
		`aria-live="polite"`,                  // 校验结论的播报区
		`aria-label="数量"`,                     // 无可见 label 时的无障碍名称
		`hx-target="next .sky-bundle-result"`, // 结果只替换自己的容器（多实例共存）
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("多端契约缺少 %q\n%s", want, body)
		}
	}
	// 数量输入必须带上该项的上下限（键盘与触屏都能用浏览器原生约束）。
	if !strings.Contains(body, `min="1"`) {
		t.Fatalf("数量输入应带 min 约束，实际 %s", body)
	}
}

// TestBundleProductsBundlePage 验收 1 的后台页面部分：
// 配置页渲染出选项表单（并行数组字段名），保存走页面写链路并真的落库。
func TestBundleProductsBundlePage(t *testing.T) {
	engine, f := newBundlePageEngine(t)
	if engine == nil {
		return
	}
	price := 158.0
	main := f.mkProduct(t, "后台套餐", "admin-bundle", &price)
	addon := f.mkProduct(t, "后台子项", "admin-addon", nil)
	v := f.firstVariant(t, addon.ID)
	f.addStock(t, v, 7)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/bundle?project="+f.projectID+"&product="+main.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("配置页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"捆绑配置", "选项数量上限", "整单最小总件数", "整单最大总件数",
		`name="variantId"`, `name="required"`, `name="defaultQty"`, `name="minQty"`, `name="maxQty"`,
		`name="maxOptions"`,
		v.SKUCode,
		// 前台配置器预览：同一份渲染在后台可见。
		`hx-get="/_fragments/bundleConfigurator?productId=` + main.ID + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("配置页缺少 %q", want)
		}
	}

	// 保存（并行数组表单）：一行真实选项 + 一行留空。
	written := postForm(engine, "/admin/products/bundle/save", url.Values{
		"productId":   {main.ID},
		"maxOptions":  {"20"},
		"minTotalQty": {"1"},
		"maxTotalQty": {"0"},
		"variantId":   {v.ID, ""},
		"required":    {"1", "1"},
		"defaultQty":  {"2", "1"},
		"minQty":      {"1", "1"},
		"maxQty":      {"3", "0"},
	})
	if written.Code != http.StatusFound {
		t.Fatalf("保存应 302 回本页，实际 %d body=%s", written.Code, written.Body.String())
	}
	if loc := written.Header().Get("Location"); strings.Contains(loc, "err=") {
		t.Fatalf("保存不应带错误回跳，实际 %q", loc)
	}
	detail, err := f.products.GetBundleConfig(context.Background(), &productdto.GetBundleConfigReq{ProductID: main.ID})
	if err != nil {
		t.Fatalf("读回配置失败: %v", err)
	}
	if len(detail.Options) != 1 {
		t.Fatalf("留空的行必须被跳过，应只落 1 项，实际 %d", len(detail.Options))
	}
	if detail.Options[0].VariantID != v.ID || detail.Options[0].DefaultQty != 2 || detail.Options[0].MaxQty != 3 {
		t.Fatalf("落库内容与表单不一致: %+v", detail.Options[0])
	}

	// 非法配置（整单下限不可达）→ 原请求 200 回填，且不落库。
	badWrite := postForm(engine, "/admin/products/bundle/save", url.Values{
		"productId":   {main.ID},
		"maxOptions":  {"20"},
		"minTotalQty": {"9"},
		"maxTotalQty": {"0"},
		"variantId":   {v.ID},
		"required":    {"1"},
		"defaultQty":  {"1"},
		"minQty":      {"1"},
		"maxQty":      {"1"},
	})
	if badWrite.Code != http.StatusOK || !strings.Contains(badWrite.Body.String(), `role="alert"`) ||
		!strings.Contains(badWrite.Body.String(), `value="9"`) {
		t.Fatalf("非法配置应原请求 200 回填并提示，实际 %d", badWrite.Code)
	}

	// 未选商品时页面仍然可渲染（骨架 + 选择下拉）。
	empty := httptest.NewRecorder()
	engine.ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/admin/products/bundle?project="+f.projectID, nil))
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), "— 选择捆绑商品 —") {
		t.Fatalf("未选商品时也应渲染骨架，实际 %d", empty.Code)
	}
}

// newBundlePageEngine 装配只挂捆绑配置页的测试引擎（真实 Jet 模板 + 真实 service）。
func newBundlePageEngine(t *testing.T) (*gin.Engine, *bundleFixture) {
	t.Helper()
	f := newBundleFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	engine.GET("/admin/products/bundle", handle.ProductBundlePage)
	engine.POST("/admin/products/bundle/save", handle.ProductBundleSave)
	return engine, f
}
