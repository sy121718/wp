package projectdto

import (
	"testing"
)

func TestParseYuanToCentsIntegerSplit(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"", 0},
		{"   ", 0},
		{"0", 0},
		{"0.00", 0},
		{"12", 1200},
		{"12.3", 1230},
		{"12.34", 1234},
		{"0.29", 29},
		{"0.07", 7},
		{"1.10", 110},
		{".5", 50},
		{"12.", 1200},
		{" 8.8 ", 880},
		{"10000", 1000000},
	}
	for _, tc := range cases {
		got, err := ParseYuanToCents(tc.raw)
		if err != nil {
			t.Errorf("ParseYuanToCents(%q) 报错：%v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseYuanToCents(%q) = %d，期望 %d", tc.raw, got, tc.want)
		}
	}
}

func TestParseYuanToCentsRejections(t *testing.T) {
	cases := []struct {
		raw  string
		want error
	}{
		{"-1", ErrShippingFeeNegative},
		{"-0.01", ErrShippingFeeNegative},
		{"abc", ErrShippingFeeFormat},
		{"12.345", ErrShippingFeeFormat},
		{"1,000", ErrShippingFeeFormat},
		{"+1", ErrShippingFeeFormat},
		{"1.2.3", ErrShippingFeeFormat},
		{".", ErrShippingFeeFormat},
		{"１", ErrShippingFeeFormat},
		{"10000.01", ErrShippingFeeTooLarge},
		{"99999999999999999999999", ErrShippingFeeTooLarge},
	}
	for _, tc := range cases {
		got, err := ParseYuanToCents(tc.raw)
		if err == nil {
			t.Errorf("ParseYuanToCents(%q) 应报错，实际得到 %d", tc.raw, got)
			continue
		}
		if err != tc.want {
			t.Errorf("ParseYuanToCents(%q) 错误 = %v，期望 %v", tc.raw, err, tc.want)
		}
		if got != 0 {
			t.Errorf("ParseYuanToCents(%q) 报错时不应返回值，实际 %d", tc.raw, got)
		}
	}
}

func TestFormatCentsAsYuan(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, ""},
		{-1, ""},
		{29, "0.29"},
		{1200, "12.00"},
		{1234, "12.34"},
		{1000000, "10000.00"},
	}
	for _, tc := range cases {
		if got := FormatCentsAsYuan(tc.cents); got != tc.want {
			t.Errorf("FormatCentsAsYuan(%d) = %q，期望 %q", tc.cents, got, tc.want)
		}
	}
}

func TestYuanCentsRoundTrip(t *testing.T) {
	for cents := int64(0); cents <= MaxShippingFeeCents; cents += 977 {
		text := FormatCentsAsYuan(cents)
		got, err := ParseYuanToCents(text)
		if err != nil {
			t.Fatalf("回显 %d 分得到 %q，解析失败：%v", cents, text, err)
		}
		if got != cents {
			t.Fatalf("往返漂移：%d → %q → %d", cents, text, got)
		}
	}
}

func TestNormalizeShippingPolicy(t *testing.T) {
	ok, err := NormalizeShippingPolicy(800, 10000)
	if err != nil || ok.BaseFeeCents != 800 || ok.FreeThresholdCents != 10000 {
		t.Fatalf("合法值应原样通过，实际 %+v err=%v", ok, err)
	}

	if _, err := NormalizeShippingPolicy(800, 5000); err != nil {
		t.Fatalf("门槛低于基础运费应合法（各自独立），实际报错 %v", err)
	}
	if _, err := NormalizeShippingPolicy(-1, 0); err != ErrShippingFeeNegative {
		t.Fatalf("负基础运费应报负数，实际 %v", err)
	}
	if _, err := NormalizeShippingPolicy(0, -1); err != ErrShippingFeeNegative {
		t.Fatalf("负门槛应报负数，实际 %v", err)
	}
	if _, err := NormalizeShippingPolicy(MaxShippingFeeCents+1, 0); err != ErrShippingFeeTooLarge {
		t.Fatalf("超上限应报超限，实际 %v", err)
	}
	if _, err := NormalizeShippingPolicy(0, MaxShippingFeeCents+1); err != ErrShippingFeeTooLarge {
		t.Fatalf("门槛超上限应报超限，实际 %v", err)
	}

	if _, err := NormalizeShippingPolicy(MaxShippingFeeCents, MaxShippingFeeCents); err != nil {
		t.Fatalf("上限本身应合法，实际 %v", err)
	}
}

func TestFloatMultiplyDrifts(t *testing.T) {
	got := int64(0.29 * 100)
	if got == 29 {
		t.Skip("本平台 0.29*100 恰好为 29：浮点漂移未复现")
	}
	if got != 28 {
		t.Fatalf("预期的漂移形态变了：int64(0.29*100) = %d", got)
	}
}
