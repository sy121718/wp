package countdown

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 倒计时校验：目标时间必填且可解析。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"缺目标时间", &Props{}, true},
		{"RFC3339 合法", &Props{TargetDate: "2026-01-01T00:00:00Z"}, false},
		{"RFC3339 带时区合法", &Props{TargetDate: "2026-01-01T00:00:00+08:00"}, false},
		{"日期时间合法", &Props{TargetDate: "2026-01-01 00:00:00"}, false},
		{"日期合法", &Props{TargetDate: "2026-01-01"}, false},
		{"非法格式", &Props{TargetDate: "not-a-date"}, true},
		{"空串", &Props{TargetDate: ""}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtra(tt.props, "n1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExtra(%+v) err=%v, wantErr=%v", tt.props, err, tt.wantErr)
			}
		})
	}
}

// TestBuildViewTargetDate 倒计时视图：目标时间解析规范化 + 单元列表。
func TestBuildViewTargetDate(t *testing.T) {
	tests := []struct {
		name       string
		props      *Props
		wantTarget string
		wantFirst  string // 期望第一个单元标识
		wantLen    int
	}{
		{"RFC3339 规范化", &Props{TargetDate: "2026-01-01T00:00:00+08:00", ShowDays: true}, "2025-12-31T16:00:00Z", "days", 4},
		{"无时区转 UTC", &Props{TargetDate: "2026-01-01 00:00:00", ShowDays: false}, "2026-01-01T00:00:00Z", "hours", 3},
		{"不显示天则无 days 位", &Props{TargetDate: "2026-01-01", ShowDays: false}, "2026-01-01T00:00:00Z", "hours", 3},
		{"非法目标时间置空", &Props{TargetDate: "bad"}, "", "hours", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := BuildView(tt.props)
			if v.TargetData != tt.wantTarget {
				t.Fatalf("TargetData=%q, want=%q", v.TargetData, tt.wantTarget)
			}
			if len(v.Units) != tt.wantLen {
				t.Fatalf("len(Units)=%d, want=%d", len(v.Units), tt.wantLen)
			}
			if len(v.Units) > 0 && v.Units[0].Unit != tt.wantFirst {
				t.Fatalf("Units[0].Unit=%q, want=%q", v.Units[0].Unit, tt.wantFirst)
			}
			// 最后一个单元不跟分隔符，其余均跟。
			if len(v.Units) > 0 {
				if v.Units[len(v.Units)-1].Sep {
					t.Errorf("最后一个单元不应带分隔符")
				}
				if len(v.Units) > 1 && !v.Units[0].Sep {
					t.Errorf("非末尾单元应带分隔符")
				}
			}
		})
	}
}

// TestBuildViewShowDays 倒计时 ShowDays 增强属性与 data-target 确定性。
func TestBuildViewShowDays(t *testing.T) {
	on := BuildView(&Props{TargetDate: "2026-01-01", ShowDays: true})
	off := BuildView(&Props{TargetDate: "2026-01-01", ShowDays: false})
	if on.ShowDaysData != "1" {
		t.Errorf("ShowDays=true 应输出 data-show-days=1, got %q", on.ShowDaysData)
	}
	if off.ShowDaysData != "0" {
		t.Errorf("ShowDays=false 应输出 data-show-days=0, got %q", off.ShowDaysData)
	}
}

// TestCompileCSS 倒计时样式编译：数字位/分隔符/标签关键声明。
func TestCompileCSS(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{TargetDate: "2026-01-01"}, b)
	css := b.String()
	wants := []string{
		"display: inline-flex",
		".cd-num",
		".cd-sep",
		".cd-label",
		"font-variant-numeric: tabular-nums",
		"font-size: 2rem",
	}
	for _, want := range wants {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}
}
