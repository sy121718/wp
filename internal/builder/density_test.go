package builder

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

func TestThemeDensity(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"},"base":{}},"root":[{"id":"h","type":"core.heading","props":{"text":"标题"}}]}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("组件模板 Set: %v", err)
	}

	cases := []struct {
		density string
		wants   []string
	}{
		{"", nil},
		{"compact", []string{"--sky-density-pad: 8px", "--sky-density-gap: 16px", "--sky-density: compact"}},
		{"cozy", []string{"--sky-density-pad: 24px", "--sky-density-gap: 32px", "--sky-density: cozy"}},
	}
	for _, tc := range cases {
		t.Run("density="+tc.density, func(t *testing.T) {
			compiled, err := Compile(page, WithComponentSet(set),
				WithThemeSettings(&ThemeSettings{Surface: ThemeSurface{Density: tc.density}}))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if len(tc.wants) == 0 {
				if strings.Contains(compiled.CSS+compiled.ThemeVarsCSS, "sky-density") {
					t.Errorf("未设置档位时不应输出密度变量")
				}
				return
			}
			for _, want := range tc.wants {

				if !strings.Contains(compiled.CSS+compiled.ThemeVarsCSS, want) {
					t.Errorf("CSS 缺少 %q", want)
				}
			}
		})
	}
}
