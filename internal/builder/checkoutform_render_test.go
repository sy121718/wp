package builder

import (
	"strings"
	"testing"

	"go_wp/internal/builder/extensions/checkoutform"
	ordercontract "go_wp/internal/module/order/contract"
	"go_wp/pkg/builder/core"
)

const checkoutDocument = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "结算", "description": "结算"}},
  "root": [{"id": "cf1", "type": "core.checkoutForm", "props": {"submitLabel": "提交订单"}}]
}`

const checkoutDocWithBilling = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "结算", "description": "结算"}},
  "root": [{"id": "cf1", "type": "core.checkoutForm", "props": {"submitLabel": "提交订单", "collectBilling": true}}]
}`

var _ = checkoutform.Type

func checkoutCountries() []core.CheckoutCountry {
	return []core.CheckoutCountry{
		{Code: "CN", Label: "中国"},
		{Code: "US", Label: "美国"},
		{Code: "JP", Label: "日本"},
	}
}

func checkoutCompileOptions(t *testing.T, extra ...CompileOption) []CompileOption {
	t.Helper()
	return append([]CompileOption{
		WithComponentSet(i18nTestComponentSet(t)),
		WithProjectID("p-1"),
		WithLanguage("zh-CN"),
		WithCheckoutCountries(checkoutCountries()),
	}, extra...)
}

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

func TestCheckoutFormRendersNativeForm(t *testing.T) {
	html := compileCheckoutDoc(t, checkoutDocument, checkoutCompileOptions(t)...)

	for _, want := range []string{

		`<div class="sky-c-cf1 sky-checkout" data-component-instance="cf1"`,
		`<section data-component-part="checkout" tabindex="-1"><form class="sky-checkout-form" method="post" action="/api/site/cart/checkout"`,
		`data-write-handler="commerce.checkout"`,
		`data-component-part="result"`,

		`data-write="custom" data-write-url="/api/site/cart/checkout"`,

		`<input type="hidden" name="projectId" value="p-1">`,
		`<input type="hidden" name="lang" value="zh-CN">`,
		`<button type="submit" class="sky-checkout-submit" data-requires-js hidden disabled>提交订单</button>`,
		`<fieldset class="sky-checkout-group" data-group="contact">`,
		`<fieldset class="sky-checkout-group" data-group="shipping">`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q\nHTML=%s", want, html)
		}
	}

	if strings.Contains(html, `action="/api/site/cart/checkout?`) {
		t.Fatalf("projectId / lang 应走隐藏字段而不是 URL 查询\nHTML=%s", html)
	}
}

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

	if strings.Contains(html, `<select id="sky-checkout-cf1-couponCode"`) ||
		strings.Contains(html, `<textarea id="sky-checkout-cf1-couponCode"`) {
		t.Fatalf("券码不该渲染成下拉或文本域\nHTML=%s", html)
	}
}

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

	if strings.Contains(html, "data-ui-select") {
		t.Fatalf("结算表单不得触发控件基座 JS（访问面零 JS）\nHTML=%s", html)
	}

	for _, key := range []string{"province", "city", "district"} {
		if !strings.Contains(html, `type="text" name="`+key+`"`) {
			t.Fatalf("%s 应为文本输入\nHTML=%s", key, html)
		}
	}
}

func TestCheckoutFormFieldOrderFollowsProps(t *testing.T) {
	html := compileCheckoutDoc(t, checkoutDocument, checkoutCompileOptions(t)...)

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

	for _, key := range []string{"shipName", "shipPhone"} {
		if strings.Contains(html, `name="`+key+`"`) {
			t.Fatalf("默认不该渲染 %q（缺省沿用联系人）\nHTML=%s", key, html)
		}
	}
}

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

func TestCheckoutFormMissingCountriesFailsBuild(t *testing.T) {
	p, err := ParsePage([]byte(checkoutDocument))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts := []CompileOption{
		WithComponentSet(i18nTestComponentSet(t)),
		WithProjectID("p-1"),
		WithLanguage("zh-CN"),
	}
	if _, err = Compile(p, opts...); err == nil {
		t.Fatal("表单含国家字段、未注入国家清单时应编译失败")
	} else if !strings.Contains(err.Error(), "国家") {
		t.Fatalf("错误信息应点明国家清单缺失，得到: %v", err)
	}
}

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
