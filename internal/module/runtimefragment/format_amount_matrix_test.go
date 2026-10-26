package runtimefragment

// format_amount_matrix_test.go — 金额格式化行为契约（审计 CQ-013）。
//
// centsToYuanText：分 → 元展示口径，收敛后 = pkg/money.CentsToYuanText。

import (
	"testing"

	"go_wp/pkg/money"
)

var amountMatrixCents = []int64{0, 1, 99, 100, 9950, 9999, 100000000000, -1, -12345, 1000000000000000}

func TestAmountFormatMatrix(t *testing.T) {
	for _, c := range amountMatrixCents {
		got := centsToYuanText(c)
		if want := money.CentsToYuanText(c); got != want {
			t.Errorf("centsToYuanText(%d) = %q，与 pkg/money.CentsToYuanText 不一致（%q）", c, got, want)
		}
		t.Logf("AMOUNT_MATRIX	cents	%d	=> centsToYuanText=%q", c, got)
	}
}
