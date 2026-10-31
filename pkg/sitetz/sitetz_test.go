package sitetz

import (
	"testing"
	"time"
)

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

func TestParseDayRejectsBadInput(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	if _, err := ParseDay("not-a-day"); err == nil {
		t.Fatal("非法日期应当报错")
	}
}

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

	utc := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	if at.Equal(utc) {
		t.Fatal("站点时区下的 10:00 与 UTC 10:00 不该是同一时刻")
	}

	if delta := at.UTC().Sub(utc); delta != -8*time.Hour {
		t.Fatalf("Asia/Shanghai 的 10:00 应比 UTC 的 10:00 早 8 小时（绝对时刻），实际差 %s", delta)
	}
}

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

	if day, derr := ParseDateTime("2026-09-30"); derr != nil || day.In(Location()).Hour() != 0 {
		t.Fatalf("纯日期应解析成当日零点: %v err=%v", day, derr)
	}

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
