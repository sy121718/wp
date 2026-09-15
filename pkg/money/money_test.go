package money

// money_test.go — 金额格式化的边界（审计 CQ-013）：零、负零、负数、大数、小数位。
//
// 两个口径的要求相反，所以两侧都要钉住：审计侧必须定长（"99.00"），展示侧必须
// 保持最短表示（"99"）—— 任何一侧被「统一」掉，都会在线上表现为假变更记录或
// 「同一个价看起来变了」。

import (
	"math"
	"testing"
)

// TestFormatAudit 审计口径：固定两位小数。
func TestFormatAudit(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0.00"},
		{math.Copysign(0, -1), "-0.00"}, // 负零：定长文本同样要稳定
		{1, "1.00"},
		{99, "99.00"},
		{99.5, "99.50"},
		{-1, "-1.00"},
		{-1234.567, "-1234.57"},
		{99.005, "99.00"}, // 二进制浮点：99.005 的真实值略小于 99.005
		{0.005, "0.01"},
		{1e15, "1000000000000000.00"},
		{1e21, "1000000000000000000000.00"},
	}
	for _, c := range cases {
		if got := FormatAudit(c.in); got != c.want {
			t.Errorf("FormatAudit(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestFormatYuan 展示口径：最短表示（整数不带小数尾巴）。
func TestFormatYuan(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "-0"}, // 负零在展示侧同样保留符号位
		{1, "1"},
		{99, "99"},
		{99.5, "99.5"},
		{99.05, "99.05"},
		{99.005, "99.005"}, // 展示侧不截断：截断会让页面上的价与实际值不符
		{-1, "-1"},
		{-1234.567, "-1234.567"},
		{0.1 + 0.2, "0.3"}, // 浮点误差按最短表示抹平
		{1e15, "1000000000000000"},
		{1e21, "1000000000000000000000"},
	}
	for _, c := range cases {
		if got := FormatYuan(c.in); got != c.want {
			t.Errorf("FormatYuan(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestCentsToYuanText 分 → 元：换算正确且与 FormatYuan 同口径。
func TestCentsToYuanText(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{1, "0.01"},
		{99, "0.99"},
		{100, "1"},
		{9950, "99.5"}, // 99.50 元读作 99.5：与产物里的价字节一致
		{9999, "99.99"},
		{-1, "-0.01"},
		{-12345, "-123.45"},
		{100000000000, "1000000000"},
		{1000000000000000, "10000000000000"},
	}
	for _, c := range cases {
		got := CentsToYuanText(c.in)
		if got != c.want {
			t.Errorf("CentsToYuanText(%d) = %q，期望 %q", c.in, got, c.want)
		}
		if want := FormatYuan(float64(c.in) / 100); got != want {
			t.Errorf("CentsToYuanText(%d) = %q 与 FormatYuan 口径不一致（%q）", c.in, got, want)
		}
	}
}
