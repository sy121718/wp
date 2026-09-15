package runtimefragment

// format_amount_matrix_test.go — 金额格式化行为契约（审计 CQ-013）。
//
// 本包内有**两种口径**，各自钉住：
//   - centsToYuanText：分 → 元展示口径，收敛后 = pkg/money.CentsToYuanText；
//   - formatAmount（bundle_configurator）：套餐配置器的前台展示文本，目前是**固定两位**，
//     与商品列表 / 详情产物里的最短表示（"99.5"）不同形。审计 CQ-013 没有点名它，
//     本测试如实钉住现状（断言它 == FormatAudit），不擅自把前台可见文本改掉 ——
//     要统一需要单独评估套餐配置器模板与前端消费方。

import (
	"math"
	"testing"

	"go_wp/pkg/money"
)

var amountMatrixYuan = []float64{
	0, 1, -1, 99, 99.5, 99.05, 99.005, 99.004, 0.005, 0.004,
	0.1 + 0.2, 1234.5678, 1234567890.12, 1e15, 1e21, -1234.567,
	100.0 / 3.0, math.Copysign(0, -1), // 负零单独造：字面量 -0.0 在 Go 里就是 +0
}

var amountMatrixCents = []int64{0, 1, 99, 100, 9950, 9999, 100000000000, -1, -12345, 1000000000000000}

func TestAmountFormatMatrix(t *testing.T) {
	for _, v := range amountMatrixYuan {
		got := formatAmount(v)
		if want := money.FormatAudit(v); got != want {
			t.Errorf("formatAmount(%v) = %q，与 pkg/money.FormatAudit 不一致（%q）", v, got, want)
		}
		t.Logf("AMOUNT_MATRIX\tyuan\t%v\t=> formatAmount=%q", v, got)
	}
	for _, c := range amountMatrixCents {
		got := centsToYuanText(c)
		if want := money.CentsToYuanText(c); got != want {
			t.Errorf("centsToYuanText(%d) = %q，与 pkg/money.CentsToYuanText 不一致（%q）", c, got, want)
		}
		t.Logf("AMOUNT_MATRIX\tcents\t%d\t=> centsToYuanText=%q", c, got)
	}
}
