package projectdto

// shipping_policy_dto_test.go — 运费金额的元/分换算与归一化判据。
//
// 这一层值得单独测的理由：它是**金额的边界**。换算差 1 分不会有任何报错、日志或页面异常，
// 只会在对账时以「总是差几分」的形式出现 —— 而那时已经没有任何线索指向这一行代码。

import (
	"testing"
)

// TestParseYuanToCentsIntegerSplit 元 → 分：整数拆分，不受浮点表示影响。
func TestParseYuanToCentsIntegerSplit(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"", 0},      // 留空 = 不配置
		{"   ", 0},   // 纯空白同义
		{"0", 0},     // 显式 0 = 不收运费
		{"0.00", 0},  //
		{"12", 1200}, //
		{"12.3", 1230},
		{"12.34", 1234},
		{"0.29", 29},  // 浮点乘的经典漂移值（见 TestFloatMultiplyDrifts）
		{"0.07", 7},   //
		{"1.10", 110}, //
		{".5", 50},    // 省略整数位（用户的常见写法）
		{"12.", 1200}, // 省略小数位
		{" 8.8 ", 880},
		{"10000", 1000000}, // 上限本身合法
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

// TestParseYuanToCentsRejections 三类非法输入各归各的原因，且都**不静默归零**。
func TestParseYuanToCentsRejections(t *testing.T) {
	cases := []struct {
		raw  string
		want error
	}{
		{"-1", ErrShippingFeeNegative},
		{"-0.01", ErrShippingFeeNegative},
		{"abc", ErrShippingFeeFormat},
		{"12.345", ErrShippingFeeFormat}, // 三位小数：不替用户四舍五入
		{"1,000", ErrShippingFeeFormat},  // 千分位：不猜他想要 1000 还是 1
		{"+1", ErrShippingFeeFormat},     // 不接受正号（表单里没有这回事）
		{"1.2.3", ErrShippingFeeFormat},
		{".", ErrShippingFeeFormat},
		{"１", ErrShippingFeeFormat},          // 全角数字
		{"10000.01", ErrShippingFeeTooLarge}, // 超上限
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

// TestFormatCentsAsYuan 分 → 元（表单回显），与解析同一套整数拆分。
func TestFormatCentsAsYuan(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, ""}, // 未配置显示为空，placeholder 说的「留空 = 不收运费」才成立
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

// TestYuanCentsRoundTrip 往返一致：回显的数字必须就是上次保存的那个。
//
// 不一致的表现是「保存一次、数字变一次」——用户会怀疑自己改错了东西，
// 然后反复保存（每保存一次就再漂一次）。
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

// TestNormalizeShippingPolicy 归一化判据（保存与读取共用）。
func TestNormalizeShippingPolicy(t *testing.T) {
	ok, err := NormalizeShippingPolicy(800, 10000)
	if err != nil || ok.BaseFeeCents != 800 || ok.FreeThresholdCents != 10000 {
		t.Fatalf("合法值应原样通过，实际 %+v err=%v", ok, err)
	}
	// 门槛低于基础运费**不算非法**：门槛是「买多少钱免运费」的独立商业参数，
	// 「满 50 元免 8 元运费」正是最常见的配置。
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
	// 边界：上限本身合法。
	if _, err := NormalizeShippingPolicy(MaxShippingFeeCents, MaxShippingFeeCents); err != nil {
		t.Fatalf("上限本身应合法，实际 %v", err)
	}
}

// TestFloatMultiplyDrifts 判据的必要性实证：浮点乘在 0.29 这类值上确实差 1 分。
//
// 这条测的不是被测代码，而是「为什么换算必须走整数拆分」。
// 若某个平台上 int64(0.29*100) 恰好等于 29，说明这层保护的必要性下降了 ——
// 那时删掉这条用例并同步改 shipping_policy_dto.go 的说明，而不是留一条与事实不符的注释。
func TestFloatMultiplyDrifts(t *testing.T) {
	got := int64(0.29 * 100)
	if got == 29 {
		t.Skip("本平台 0.29*100 恰好为 29：浮点漂移未复现")
	}
	if got != 28 {
		t.Fatalf("预期的漂移形态变了：int64(0.29*100) = %d", got)
	}
}
