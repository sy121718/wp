package builder

// checkoutform_render_test.go — core.checkoutForm 的真实编译产物断言（含构建期数据注入）。
//
// 为什么必须是「真编译产物」而不是单测直调 BuildView：本组件的价值一半在于
// **烘进静态产物**的那部分 —— 国家下拉的选项、隐藏字段、片段地址、字段顺序。
// 这些只有在 renderView → 模板 → HTML 这条完整链路上才成立（模板写错 / 特征漏登记 /
// 注入没接上，BuildView 的单测都不会红）。
//
// 覆盖：
//  1. 表单壳与提交语义（原生 action + hx-post 指向片段端点）；
//  2. projectId / lang 作为隐藏字段（POST 只读表单体，URL query 到不了处理器）；
//  3. 国家是原生 <select>、选项构建期烘焙、默认国家预选中、且**不带** data-ui-select（零 JS）；
//  4. 字段顺序与分组（作者配置顺序 → 渲染顺序）；
//  5. 账单地址开关；
//  6. 构建期数据缺失时的行为（有国家字段却没注入清单 → 编译失败；缺工程 id → 提示而非失败）。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	ordercontract "go_wp/internal/module/order/contract"
)

// checkoutDocument 含一个结算表单节点的页面文档（原子组件，字段走清单默认选择）。
const checkoutDocument = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "结算", "description": "结算"}},
  "root": [{"id": "cf1", "type": "core.checkoutForm", "props": {"submitLabel": "提交订单"}}]
}`

// checkoutDocWithBilling 打开账单地址的文档。
const checkoutDocWithBilling = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "结算", "description": "结算"}},
  "root": [{"id": "cf1", "type": "core.checkoutForm", "props": {"submitLabel": "提交订单", "collectBilling": true}}]
}`

// checkoutCountries 装配层注入的国家清单（与 sys_area 的形态同源：码 + 展示名，已按语言算好）。
func checkoutCountries() []core.CheckoutCountry {
	return []core.CheckoutCountry{
		{Code: "CN", Label: "中国"},
		{Code: "US", Label: "美国"},
		{Code: "JP", Label: "日本"},
	}
}

// checkoutCompileOptions 结算表单用例的公共编译选项（工程 id + 语言 + 国家清单）。
func checkoutCompileOptions(t *testing.T, extra ...CompileOption) []CompileOption {
	t.Helper()
	return append([]CompileOption{
		WithComponentSet(i18nTestComponentSet(t)),
		WithProjectID("p-1"),
		WithLanguage("zh-CN"),
		WithCheckoutCountries(checkoutCountries()),
	}, extra...)
}

// compileCheckoutDoc 编译用例文档并返回产物 HTML（编译失败即用例失败）。
func compileCheckoutDoc(t *testing.T, doc string, opts ...CompileOption) string {
	t.Helper()
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	res, err := Compile(p, opts...)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res.HTML
}

// TestCheckoutFormRendersNativeForm 表单壳、提交语义与隐藏字段。
func TestCheckoutFormRendersNativeForm(t *testing.T) {
	html := compileCheckoutDoc(t, checkoutDocument, checkoutCompileOptions(t)...)

	for _, want := range []string{
		// 原生提交语义（无 JS 时整页提交照旧可用）。
		`<form class="sky-c-cf1 sky-checkout" method="post" action="/_fragments/checkout"`,
		// htmx 路径：提交后片段结果替换表单自身。
		`hx-post="/_fragments/checkout" hx-target="closest .sky-checkout" hx-swap="outerHTML"`,
		// projectId / lang 必须是隐藏字段：片段端 POST 只读表单体。
		`<input type="hidden" name="projectId" value="p-1">`,
		`<input type="hidden" name="lang" value="zh-CN">`,
		`<button type="submit" class="sky-checkout-submit">提交订单</button>`,
		`<fieldset class="sky-checkout-group" data-group="contact">`,
		`<fieldset class="sky-checkout-group" data-group="shipping">`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q\nHTML=%s", want, html)
		}
	}
	// 提交地址的 query 里不得再拼 projectId（POST 时到不了处理器，是「看着对」的坏写法）。
	if strings.Contains(html, `action="/_fragments/checkout?`) {
		t.Fatalf("projectId / lang 应走隐藏字段而不是 URL 查询\nHTML=%s", html)
	}
}

// TestCheckoutFormRendersCouponField 结算表单固定渲染一行优惠码，字段名与片段端一致。
//
// 钉的是**两端接得上**：组件输出 name="couponCode"，而 runtimefragment 的 renderCheckout
// 就读 paramOf(r, "couponCode")。名字写错属于静默失效：表单照常渲染、提交时券被当成没填，
// 页面上完全看不出异常 —— 与 projectId / lang 必须走隐藏字段（不能只挂 URL）是同一类判据。
func TestCheckoutFormRendersCouponField(t *testing.T) {
	html := compileCheckoutDoc(t, checkoutDocument, checkoutCompileOptions(t)...)

	for _, want := range []string{
		`<fieldset class="sky-checkout-group sky-checkout-coupon" data-group="coupon">`,
		`name="couponCode"`,
		`优惠码`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q\nHTML=%s", want, html)
		}
	}
	// 券码是普通文本输入：不是 country（没有选项）、不是 textarea（H5 里券码是短串）。
	if strings.Contains(html, `<select id="sky-checkout-cf1-couponCode"`) ||
		strings.Contains(html, `<textarea id="sky-checkout-cf1-couponCode"`) {
		t.Fatalf("券码不该渲染成下拉或文本域\nHTML=%s", html)
	}
}

// TestCheckoutFormCountrySelectBaked 国家字段：原生 select + 构建期烘焙选项 + 默认国家预选中。
func TestCheckoutFormCountrySelectBaked(t *testing.T) {
	html := compileCheckoutDoc(t, checkoutDocument, checkoutCompileOptions(t)...)

	if !strings.Contains(html, `<select id="sky-checkout-cf1-country" name="country">`) {
		t.Fatalf("国家字段应为原生 select\nHTML=%s", html)
	}
	for _, want := range []string{
		`<option value="CN" selected>中国</option>`,
		`<option value="US">美国</option>`,
		`<option value="JP">日本</option>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("国家选项缺 %q\nHTML=%s", want, html)
		}
	}
	// 零客户端 JS：不加控件基座的 data-ui-select（它是 JS 替身的触发特征）。
	if strings.Contains(html, "data-ui-select") {
		t.Fatalf("结算表单不得触发控件基座 JS（访问面零 JS）\nHTML=%s", html)
	}
	// 省 / 市 / 区保持文本输入（本批不做级联）。
	for _, key := range []string{"province", "city", "district"} {
		if !strings.Contains(html, `type="text" name="`+key+`"`) {
			t.Fatalf("%s 应为文本输入\nHTML=%s", key, html)
		}
	}
}

// TestCheckoutFormFieldOrderFollowsProps 字段顺序 = 作者配置顺序（清单顺序只是默认值）。
func TestCheckoutFormFieldOrderFollowsProps(t *testing.T) {
	html := compileCheckoutDoc(t, checkoutDocument, checkoutCompileOptions(t)...)

	// 默认选择按清单顺序：email → name → phone → shipName … → address … → remark。
	order := []string{"email", "name", "phone", "address", "remark"}
	prev := -1
	for _, key := range order {
		i := strings.Index(html, `name="`+key+`"`)
		if i < 0 {
			t.Fatalf("产物缺少字段 %q\nHTML=%s", key, html)
		}
		if i < prev {
			t.Fatalf("字段 %q 的出现位置早于前一个字段（顺序被改写）\nHTML=%s", key, html)
		}
		prev = i
	}
	// 默认不勾收货人 / 收货电话（cart.go 的缺省就是联系人姓名与电话）。
	for _, key := range []string{"shipName", "shipPhone"} {
		if strings.Contains(html, `name="`+key+`"`) {
			t.Fatalf("默认不该渲染 %q（缺省沿用联系人）\nHTML=%s", key, html)
		}
	}
}

// TestCheckoutFormBillingSwitch 账单地址开关：关时整组不渲染、开时出现在收货组之后。
func TestCheckoutFormBillingSwitch(t *testing.T) {
	plain := compileCheckoutDoc(t, checkoutDocument, checkoutCompileOptions(t)...)
	if strings.Contains(plain, `data-group="billing"`) {
		t.Fatalf("未开账单地址时不该渲染账单组\nHTML=%s", plain)
	}

	withBilling := compileCheckoutDoc(t, checkoutDocWithBilling, checkoutCompileOptions(t)...)
	if !strings.Contains(withBilling, `data-group="billing"`) {
		t.Fatalf("开了账单地址就该渲染账单组\nHTML=%s", withBilling)
	}
	if !strings.Contains(withBilling, `name="billAddress"`) {
		t.Fatalf("账单组缺少 bill* 字段\nHTML=%s", withBilling)
	}
	if strings.Index(withBilling, `data-group="billing"`) < strings.Index(withBilling, `data-group="shipping"`) {
		t.Fatalf("账单组应排在收货组之后\nHTML=%s", withBilling)
	}
}

// TestCheckoutFormRequiredFromCatalog 清单里的必填字段渲染 required 属性；可选字段没有。
func TestCheckoutFormRequiredFromCatalog(t *testing.T) {
	html := compileCheckoutDoc(t, checkoutDocument, checkoutCompileOptions(t)...)
	for _, f := range ordercontract.CheckoutFormFields() {
		if !f.Required {
			continue
		}
		needle := `name="` + f.Key + `" required`
		if !strings.Contains(html, needle) {
			t.Fatalf("必填字段 %q 应渲染 required\nHTML=%s", f.Key, html)
		}
	}
	if strings.Contains(html, `name="zip" required`) {
		t.Fatalf("邮编不是清单必填项，不该带 required\nHTML=%s", html)
	}
}

// TestCheckoutFormInvalidKeyRejected 非法 key 在**校验期**被拒（不静默跳过）。
func TestCheckoutFormInvalidKeyRejected(t *testing.T) {
	const doc = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "结算", "description": "结算"}},
  "root": [{"id": "cf1", "type": "core.checkoutForm", "props": {"fields": [
    {"key": "email"}, {"key": "name"}, {"key": "phone"}, {"key": "address"}, {"key": "luckyNumber"}
  ]}}]
}`
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if _, err = Compile(p, checkoutCompileOptions(t)...); err == nil {
		t.Fatal("非法字段 key 应让校验失败（不静默跳过）")
	} else if !strings.Contains(err.Error(), "luckyNumber") {
		t.Fatalf("错误信息应点出非法 key，得到: %v", err)
	}
}

// TestCheckoutFormRequiredCannotBeDropped 清单必填字段被关掉 → 校验失败。
func TestCheckoutFormRequiredCannotBeDropped(t *testing.T) {
	const doc = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "结算", "description": "结算"}},
  "root": [{"id": "cf1", "type": "core.checkoutForm", "props": {"fields": [
    {"key": "name"}, {"key": "phone"}, {"key": "address"}
  ]}}]
}`
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if _, err = Compile(p, checkoutCompileOptions(t)...); err == nil {
		t.Fatal("缺必填字段应让校验失败")
	} else if !strings.Contains(err.Error(), "email") {
		t.Fatalf("错误信息应点出缺哪个必填字段，得到: %v", err)
	}
}

// TestCheckoutFormMissingCountriesFailsBuild 构建期没注入国家清单、而表单里有国家字段 → 编译失败。
//
// 这是本组件「降级 vs 失败」的判据：静默降级会烘出一个只有默认国家的下拉，
// 访客被锁死在一个国家，而产物看起来完全正常。
func TestCheckoutFormMissingCountriesFailsBuild(t *testing.T) {
	p, err := ParsePage([]byte(checkoutDocument))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts := []CompileOption{
		WithComponentSet(i18nTestComponentSet(t)),
		WithProjectID("p-1"),
		WithLanguage("zh-CN"),
		// 刻意不注入 WithCheckoutCountries
	}
	if _, err = Compile(p, opts...); err == nil {
		t.Fatal("表单含国家字段、未注入国家清单时应编译失败")
	} else if !strings.Contains(err.Error(), "国家") {
		t.Fatalf("错误信息应点明国家清单缺失，得到: %v", err)
	}
}

// TestCheckoutFormWithoutProjectIDRendersNotice 缺工程 id → 提示视图，编译不失败。
//
// 判据：组件库「每个条目插入后都能编译」是既有契约（编辑器画布与组件库契约测试都没有
// 工程上下文），这条路径与作者配置无关，不该让它红。
func TestCheckoutFormWithoutProjectIDRendersNotice(t *testing.T) {
	p, err := ParsePage([]byte(checkoutDocument))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts := []CompileOption{
		WithComponentSet(i18nTestComponentSet(t)),
		WithLanguage("zh-CN"),
		WithCheckoutCountries(checkoutCountries()),
	}
	res, err := Compile(p, opts...)
	if err != nil {
		t.Fatalf("缺工程 id 不该让编译失败: %v", err)
	}
	if !strings.Contains(res.HTML, "sky-checkout-unavailable") {
		t.Fatalf("缺工程 id 应渲染不可用提示\nHTML=%s", res.HTML)
	}
	if strings.Contains(res.HTML, "hx-post") {
		t.Fatalf("提示分支不该输出 hx-*（会白送一份 htmx）\nHTML=%s", res.HTML)
	}
}
