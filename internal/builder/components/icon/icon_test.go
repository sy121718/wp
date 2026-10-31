package icon

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法", &Props{}, false},
		{"合法图标", &Props{IconName: "heart"}, false},
		{"全部白名单图标", &Props{IconName: "search"}, false},
		{"非法图标", &Props{IconName: "x"}, true},
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

func TestBuildViewIcon(t *testing.T) {

	names := []string{"star", "heart", "check", "arrow-right", "arrow-left", "info", "close", "search"}
	for _, n := range names {
		v := BuildView(&Props{IconName: n})
		if v.IconSVG == "" {
			t.Errorf("图标 %q 未命中白名单", n)
		}
		if !strings.Contains(v.IconSVG, "<") {
			t.Errorf("图标 %q 输出非法 SVG 元素: %q", n, v.IconSVG)
		}
	}

	if v := BuildView(&Props{}); v.IconSVG != mustIconPath(t, defaultIconName) {
		t.Errorf("空图标名应兜底 star, got %q", v.IconSVG)
	}

	if v := BuildView(&Props{IconName: "nope"}); v.IconSVG != mustIconPath(t, defaultIconName) {
		t.Errorf("未知图标名应兜底 star, got %q", v.IconSVG)
	}
}

func mustIconPath(t *testing.T, name string) string {
	t.Helper()
	p, ok := iconPath(name)
	if !ok {
		t.Fatalf("图标 %q 不在白名单", name)
	}
	return p
}

func TestCompileCSS(t *testing.T) {
	tests := []struct {
		name  string
		props *Props
		wants []string
	}{
		{"默认尺寸颜色", &Props{}, []string{"width: 1.5em", "height: 1.5em", "color: currentColor"}},
		{"自定义尺寸", &Props{Size: "24px"}, []string{"width: 24px", "height: 24px"}},
		{"自定义颜色", &Props{Color: "#f00"}, []string{"color: #f00"}},
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
