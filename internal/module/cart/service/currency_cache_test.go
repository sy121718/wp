package cartservice

import (
	"context"
	"testing"

	"go_wp/pkg/i18n"
)

func TestCartCurrencyFollowsGlobalDefault(t *testing.T) {
	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{DefaultCurrency: "USD"}, nil
	})
	if got := cartCurrency(); got != "USD" {
		t.Fatalf("购物车币种应跟随全局默认（USD），实际 %s", got)
	}

	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{}, nil
	})
	if got := cartCurrency(); got != "CNY" {
		t.Fatalf("未配置时应回退 CNY，实际 %s", got)
	}
	t.Logf("注入 USD → %s；未配置 → %s", "USD", "CNY")
}
