package productservice

// format_amount_matrix_test.go — 金额格式化行为契约（审计 CQ-013）。
//
// 收敛后本包不再有自己的实现：测试断言 formatPrice == pkg/money.FormatYuan，
// 并保留矩阵输出作为对照记录（历史结论：本包与 dashboard 的 formatAmount
// 逐字节重复，都是**最短表示**的展示口径）。

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

func TestAmountFormatMatrix(t *testing.T) {
	for _, v := range amountMatrixYuan {
		got := formatPrice(v)
		if want := money.FormatYuan(v); got != want {
			t.Errorf("formatPrice(%v) = %q，与 pkg/money.FormatYuan 不一致（%q）", v, got, want)
		}
		t.Logf("AMOUNT_MATRIX\tyuan\t%v\t=> %q", v, got)
	}
}
