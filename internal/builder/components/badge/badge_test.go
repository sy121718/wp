package badge

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 徽章校验：必须有文字。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 拒绝", &Props{}, true},
		{"有文字合法", &Props{Text: "新品"}, false},
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

// TestBuildView 徽章视图透传文字。
func TestBuildView(t *testing.T) {
	v := BuildView(&Props{Text: "限时"})
	if v.Text != "限时" {
		t.Fatalf("BuildView Text=%q, want=限时", v.Text)
	}
}

// TestCompileCSS 徽章样式编译：三种变体 + 缺省/自定义主色。
func TestCompileCSS(t *testing.T) {
	tests := []struct {
		name  string
		props *Props
		wants []string
		not   []string
	}{
		{
			name:  "solid 缺省主色",
			props: &Props{Text: "A", Variant: VariantSolid},
			wants: []string{"background: var(--color-primary, #2563eb)", "color: #fff", "border-radius: 9999px"},
			not:   []string{"border: 1px", "color-mix"},
		},
		{
			name:  "outline 描边",
			props: &Props{Text: "A", Variant: VariantOutline, Color: "#f00"},
			wants: []string{"border: 1px solid #f00", "color: #f00", "background: transparent"},
		},
		{
			name:  "soft 浅底",
			props: &Props{Text: "A", Variant: VariantSoft, Color: "#0af"},
			wants: []string{"color-mix(in srgb, #0af 12%, transparent)", "color: #0af"},
		},
		{
			name:  "缺省变体为 solid",
			props: &Props{Text: "A"},
			wants: []string{"background: var(--color-primary, #2563eb)", "color: #fff"},
		},
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
			for _, n := range tt.not {
				if strings.Contains(css, n) {
					t.Errorf("CSS 不应包含 %q\n%s", n, css)
				}
			}
		})
	}
}
