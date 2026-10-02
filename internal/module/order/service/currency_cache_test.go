package orderservice

// currency_cache_test.go — 新建订单的币种标签跟随全局默认值（Y2）。
//
// orders.currency 是**下单当时的快照**（快照原则）：本函数只在建单时取值，
// 改配置只影响此后的新订单，历史订单不动。

import (
	"context"
	"testing"

	"go_wp/pkg/i18n"
)

func TestOrderCurrencyFollowsGlobalDefault(t *testing.T) {
	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{DefaultCurrency: "USD"}, nil
	})
	if got := orderCurrency(); got != "USD" {
		t.Fatalf("新建订单币种应跟随全局默认（USD），实际 %s", got)
	}
	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{}, nil
	})
	if got := orderCurrency(); got != "CNY" {
		t.Fatalf("未配置时应回退 CNY，实际 %s", got)
	}
	t.Log("注入 USD → USD；未配置 → CNY（历史订单的快照不受影响）")
}
