package builder

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// 普通页面无需系统槽位即可完整编译购物车、结算与本地订单结果。
func TestCommercePresetCompilesWithoutSiteSlots(t *testing.T) {
	raw, err := (BusinessPresetRegistry{}).ResolvePreset(context.Background(), "commerce.cart-checkout", 1)
	if err != nil {
		t.Fatal(err)
	}
	page, err := ParsePage(raw)
	if err != nil {
		t.Fatal(err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	result, err := Compile(page, WithCompileMode(CompileModePublish), WithComponentSet(set), WithProjectID("commerce-project"), WithCheckoutCountries(checkoutCountries()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-cart-icon="inline"`, `data-component-instance="commerce-cart"`,
		`data-component-part="cart"`, `data-write-handler="commerce.cart"`,
		`data-component-instance="commerce-checkout"`, `data-component-part="checkout"`,
		`data-component-part="result"`, `data-write-handler="commerce.checkout"`,
		`data-component-action="checkout"`, `data-action-instance="commerce-checkout"`,
		`data-component-action="back"`, `data-action-instance="commerce-cart"`,
		`data-component-action="success"`, `data-action-kind="local"`, `data-action-part="result"`,
	} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("完整预设缺少 %s", want)
		}
	}
	for _, old := range []string{`href="/cart`, `href="/checkout`, `/fragments/cart`, `HX-Redirect`} {
		if strings.Contains(result.HTML, old) {
			t.Errorf("完整预设仍含隐式旧出口 %s", old)
		}
	}
}
