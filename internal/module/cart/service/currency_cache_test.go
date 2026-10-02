package cartservice

// currency_cache_test.go — 购物车展示币种跟随全局默认值（Y2）。
//
// 币种是**标签**不是算术：金额本来是数值（分），改币种只改标签。按产品口径货币由后台
// 全局限定为单值、前台不提供货币选择（用户只能改地区），因此这里读全局默认值
// 不改变任何金额计算。值来自 pkg/i18n 的进程内缓存（不在请求路径查库）。

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
	// 配置不可用（未注入 / 读失败）时回退代码内常量 —— 兜底在 pkg/i18n 内部完成，
	// 购物车侧只透传，所以这里断言的是 i18n 的兜底结果。
	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{}, nil
	})
	if got := cartCurrency(); got != "CNY" {
		t.Fatalf("未配置时应回退 CNY，实际 %s", got)
	}
	t.Logf("注入 USD → %s；未配置 → %s", "USD", "CNY")
}
