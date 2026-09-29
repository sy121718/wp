package sitetz

import (
	"testing"
	"time"
)

// TestLocationFollowsEnvVar 环境变量指定的时区生效。
func TestLocationFollowsEnvVar(t *testing.T) {
	t.Setenv(envKey, "UTC")
	ResetForTest()
	t.Cleanup(ResetForTest)

	if Name() != "UTC" {
		t.Fatalf("应使用环境变量指定的时区，实际 %q", Name())
	}
	day, err := ParseDay("2026-09-14")
	if err != nil {
		t.Fatalf("解析日期失败: %v", err)
	}
	if day.Location() != time.UTC || day.Hour() != 0 {
		t.Fatalf("应按 UTC 解析到当日零点: %v", day)
	}
}

// TestLocationFallsBackOnBadName 时区名非法时回退本地时区而不是崩掉。
func TestLocationFallsBackOnBadName(t *testing.T) {
	t.Setenv(envKey, "Not/AZone")
	ResetForTest()
	t.Cleanup(ResetForTest)

	if Location() == nil {
		t.Fatal("非法时区名不应导致 nil 时区")
	}
	if _, err := ParseDay("2026-09-14"); err != nil {
		t.Fatalf("回退后解析仍应可用: %v", err)
	}
}

// TestParseDayRejectsBadInput 非法输入照常报错（不静默当成今天）。
func TestParseDayRejectsBadInput(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	if _, err := ParseDay("not-a-day"); err == nil {
		t.Fatal("非法日期应当报错")
	}
}

// TestParseDateTimeUsesSiteTimezone 「日期 + 时刻」按站点时区解释（审计 TX-011 的口径）。
//
// 这是定时上下线的排定入口依赖的判据：同一条字面量「2026-09-30 10:00」在 Asia/Shanghai
// 与 UTC 下必须是两个不同的绝对时刻 —— 若实现走 time.Local 或 UTC，部署固定时区之后
// 到点时刻会整体偏移几个小时，而且不报任何错（排定看起来成功了）。
func TestParseDateTimeUsesSiteTimezone(t *testing.T) {
	t.Setenv(envKey, "Asia/Shanghai")
	ResetForTest()
	t.Cleanup(ResetForTest)

	at, err := ParseDateTime("2026-09-30 10:00")
	if err != nil {
		t.Fatalf("解析日期+时刻失败: %v", err)
	}
	if hour := at.In(Location()).Hour(); hour != 10 {
		t.Fatalf("应按站点时区解释 10:00，实际 %d 点", hour)
	}
	// 同一字面量在 UTC 下解释出的绝对时刻必须不同（差 8 小时）。
	utc := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	if at.Equal(utc) {
		t.Fatal("站点时区下的 10:00 与 UTC 10:00 不该是同一时刻")
	}
	// 站点时区 10:00 的绝对时刻 = 同日 02:00 UTC，比「UTC 的 10:00」早 8 小时。
	if delta := at.UTC().Sub(utc); delta != -8*time.Hour {
		t.Fatalf("Asia/Shanghai 的 10:00 应比 UTC 的 10:00 早 8 小时（绝对时刻），实际差 %s", delta)
	}
}

// TestParseDateTimeAcceptsFormAndAbsoluteForms 分隔符、秒可选，带偏移量的绝对时刻不被改写。
func TestParseDateTimeAcceptsFormAndAbsoluteForms(t *testing.T) {
	t.Setenv(envKey, "Asia/Shanghai")
	ResetForTest()
	t.Cleanup(ResetForTest)

	forms := []string{"2026-09-30 10:00", "2026-09-30T10:00", "2026-09-30 10:00:00", "2026-09-30T10:00:00"}
	want, err := ParseDateTime(forms[0])
	if err != nil {
		t.Fatalf("解析基准形态失败: %v", err)
	}
	for _, form := range forms[1:] {
		got, perr := ParseDateTime(form)
		if perr != nil || !got.Equal(want) {
			t.Fatalf("形态 %q 应解析成同一时刻: got=%v err=%v", form, got, perr)
		}
	}
	// 纯日期 = 当日零点（与 ParseDay 同义）。
	if day, derr := ParseDateTime("2026-09-30"); derr != nil || day.In(Location()).Hour() != 0 {
		t.Fatalf("纯日期应解析成当日零点: %v err=%v", day, derr)
	}
	// 带偏移量的 RFC3339 是绝对时刻：不被站点时区改写。
	if abs, aerr := ParseDateTime("2026-09-30T10:00:00+08:00"); aerr != nil || !abs.Equal(want) {
		t.Fatalf("带偏移的绝对时刻应原样采用: %v err=%v", abs, aerr)
	}
	if _, berr := ParseDateTime(""); berr == nil {
		t.Fatal("空串应报错（不能静默当成零值时间）")
	}
	if _, cerr := ParseDateTime("2026/09/30 10:00"); cerr == nil {
		t.Fatal("不认识的时间格式应报错")
	}
}

// TestFormatDateTimeRoundTrip 读侧输出能被写侧原样解析回来（表单往返不丢时刻）。
func TestFormatDateTimeRoundTrip(t *testing.T) {
	t.Setenv(envKey, "Asia/Shanghai")
	ResetForTest()
	t.Cleanup(ResetForTest)

	at := time.Date(2026, 9, 30, 10, 30, 0, 0, Location())
	text := FormatDateTime(at)
	if text != "2026-09-30T10:30" {
		t.Fatalf("应输出站点时区下的 datetime-local 形态，实际 %q", text)
	}
	back, err := ParseDateTime(text)
	if err != nil || !back.Equal(at) {
		t.Fatalf("往返后应回到同一时刻: got=%v err=%v", back, err)
	}
	if zero := FormatDateTime(time.Time{}); zero != "" {
		t.Fatalf("零值应输出空串（让表单渲染成空输入框），实际 %q", zero)
	}
}
