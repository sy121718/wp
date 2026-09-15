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
