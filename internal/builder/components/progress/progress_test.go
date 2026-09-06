package progress

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 进度条校验：值/上限边界。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法", &Props{}, false},
		{"value 小于 max 合法", &Props{Value: 50, Max: 100}, false},
		{"value 等于 max 合法", &Props{Value: 100, Max: 100}, false},
		{"value 超过 max 拒绝", &Props{Value: 101, Max: 100}, true},
		{"value 为负拒绝", &Props{Value: -1}, true},
		{"max 缺省默认 100 且 value=100 合法", &Props{Value: 100}, false},
		{"max 缺省时 value=101 拒绝", &Props{Value: 101}, true},
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

// TestPercent 进度百分比计算。
func TestPercent(t *testing.T) {
	tests := []struct {
		name  string
		props *Props
		want  int
	}{
		{"缺省 max 100 半程", &Props{Value: 50}, 50},
		{"50/200", &Props{Value: 50, Max: 200}, 25},
		{"满值", &Props{Value: 100, Max: 100}, 100},
		{"零值", &Props{}, 0},
		{"越界截断", &Props{Value: 200, Max: 100}, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percent(tt.props); got != tt.want {
				t.Fatalf("percent(%+v)=%d, want=%d", tt.props, got, tt.want)
			}
		})
	}
}

// TestBuildView 进度条视图：aria 属性与标签。
func TestBuildView(t *testing.T) {
	v := BuildView(&Props{Value: 60, Max: 120, Label: "加载中"})
	if v.ValueNow != "60" || v.ValueMax != "120" {
		t.Fatalf("aria 属性错误: now=%q max=%q", v.ValueNow, v.ValueMax)
	}
	if !v.HasLabel || v.Label != "加载中" {
		t.Fatalf("标签错误: HasLabel=%v Label=%q", v.HasLabel, v.Label)
	}
	// 缺省 max=100。
	v2 := BuildView(&Props{Value: 30})
	if v2.ValueMax != "100" {
		t.Fatalf("缺省 max 应为 100, got %q", v2.ValueMax)
	}
}

// TestCompileCSS 进度条样式编译：轨道/填充宽度/颜色。
func TestCompileCSS(t *testing.T) {
	tests := []struct {
		name  string
		props *Props
		wants []string
	}{
		{"缺省主色半程", &Props{Value: 50}, []string{"width: 50%", "background: var(--color-primary, #2563eb)", ".wp-progress-track"}},
		{"自定义色与比例", &Props{Value: 25, Max: 200, Color: "#0af"}, []string{"width: 12%", "background: #0af"}},
		{"满值", &Props{Value: 100}, []string{"width: 100%"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &core.CSSBuckets{}
			compileCSS("n1", tt.props, b)
			css := b.String()
			for _, want := range tt.wants {
				if !strings.Contains(css, want) {
					t.Errorf("CSS 缺少 %q\n%s", want, css)
				}
			}
		})
	}
}
