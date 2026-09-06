package rating

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 评分校验：值/上限边界。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法", &Props{}, false},
		{"值在范围内合法", &Props{Value: 3.5, Max: 5}, false},
		{"值等于 max 合法", &Props{Value: 5, Max: 5}, false},
		{"值超过 max 拒绝", &Props{Value: 5.5, Max: 5}, true},
		{"值为负拒绝", &Props{Value: -1}, true},
		{"max 缺省默认 5 且值=5 合法", &Props{Value: 5}, false},
		{"max 缺省时值=6 拒绝", &Props{Value: 6}, true},
		{"max 超过 10 拒绝", &Props{Max: 11}, true},
		{"max=10 合法", &Props{Max: 10}, false},
		{"max 为负拒绝", &Props{Max: -1}, true},
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

// TestEffectiveMax 评分缺省上限。
func TestEffectiveMax(t *testing.T) {
	if got := effectiveMax(&Props{}); got != 5 {
		t.Fatalf("缺省 max=%d, want=5", got)
	}
	if got := effectiveMax(&Props{Max: 7}); got != 7 {
		t.Fatalf("显式 max=%d, want=7", got)
	}
}

// TestBuildView 评分视图：星形填充状态列表。
func TestBuildView(t *testing.T) {
	tests := []struct {
		name     string
		props    *Props
		wantLen  int
		wantFull int // 实心星数（前缀）
		wantHalf int // 半星数
	}{
		{"缺省 5 星全空", &Props{}, 5, 0, 0},
		{"整数 3 星", &Props{Value: 3, Max: 5}, 5, 3, 0},
		{"小数 3.5 含半星", &Props{Value: 3.5, Max: 5}, 5, 3, 1},
		{"满值 5 星", &Props{Value: 5, Max: 5}, 5, 5, 0},
		{"自定义 max=3", &Props{Value: 2, Max: 3}, 3, 2, 0},
		{"小数 4.2 半星", &Props{Value: 4.2, Max: 5}, 5, 4, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := BuildView(tt.props)
			if len(v.Stars) != tt.wantLen {
				t.Fatalf("len(Stars)=%d, want=%d", len(v.Stars), tt.wantLen)
			}
			full, half := 0, 0
			for _, s := range v.Stars {
				switch s.StarSVG {
				case starFull:
					full++
				case starHalf:
					half++
				case starEmpty:
				default:
					t.Fatalf("未知星形形态: %q", s.StarSVG)
				}
			}
			if full != tt.wantFull || half != tt.wantHalf {
				t.Fatalf("full=%d half=%d, want full=%d half=%d", full, half, tt.wantFull, tt.wantHalf)
			}
			if v.Label == "" {
				t.Error("aria-label 不能为空")
			}
		})
	}
}

// TestBuildViewHalfStar 半星 SVG 结构（clipPath 左半填充）。
func TestBuildViewHalfStar(t *testing.T) {
	v := BuildView(&Props{Value: 2.5, Max: 5})
	var halfSVG string
	for _, s := range v.Stars {
		if s.StarSVG == starHalf {
			halfSVG = s.StarSVG
		}
	}
	if halfSVG == "" {
		t.Fatal("2.5 分应包含半星")
	}
	for _, want := range []string{"clipPath", "wp-star-half", "clip-path=\"url(#wp-star-half)\""} {
		if !strings.Contains(halfSVG, want) {
			t.Errorf("半星 SVG 缺少 %q\n%s", want, halfSVG)
		}
	}
}

// TestCompileCSS 评分样式编译：容器/星形尺寸/配色。
func TestCompileCSS(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{Value: 4, Max: 5}, b)
	css := b.String()
	wants := []string{"display: inline-flex", ".wp-star", "color: #f59e0b", "width: 1.25em"}
	for _, want := range wants {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}
}
