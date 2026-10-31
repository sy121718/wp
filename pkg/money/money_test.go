package money

import (
	"math"
	"testing"
)

func TestFormatAudit(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0.00"},
		{math.Copysign(0, -1), "-0.00"},
		{1, "1.00"},
		{99, "99.00"},
		{99.5, "99.50"},
		{-1, "-1.00"},
		{-1234.567, "-1234.57"},
		{99.005, "99.00"},
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

func TestFormatYuan(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "-0"},
		{1, "1"},
		{99, "99"},
		{99.5, "99.5"},
		{99.05, "99.05"},
		{99.005, "99.005"},
		{-1, "-1"},
		{-1234.567, "-1234.567"},
		{0.1 + 0.2, "0.3"},
		{1e15, "1000000000000000"},
		{1e21, "1000000000000000000000"},
	}
	for _, c := range cases {
		if got := FormatYuan(c.in); got != c.want {
			t.Errorf("FormatYuan(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestCentsToYuanText(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{1, "0.01"},
		{99, "0.99"},
		{100, "1"},
		{9950, "99.5"},
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
