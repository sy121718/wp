package masterdatacontract

import (
	"math"
	"testing"

	"go_wp/pkg/money"
)

var amountMatrixYuan = []float64{
	0, 1, -1, 99, 99.5, 99.05, 99.005, 99.004, 0.005, 0.004,
	0.1 + 0.2, 1234.5678, 1234567890.12, 1e15, 1e21, -1234.567,
	100.0 / 3.0, math.Copysign(0, -1),
}

func TestAmountFormatMatrix(t *testing.T) {
	for _, v := range amountMatrixYuan {
		got := FormatPrice(v)
		if want := money.FormatAudit(v); got != want {
			t.Errorf("FormatPrice(%v) = %q，与 pkg/money.FormatAudit 不一致（%q）", v, got, want)
		}
		t.Logf("AMOUNT_MATRIX\tyuan\t%v\t=> %q", v, got)
	}
}
