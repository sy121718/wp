package builder

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// TestBackgroundPattern 图案背景：20 种平铺输出 + 与渐变/背景图互斥校验。
func TestBackgroundPattern(t *testing.T) {
	compileDoc := func(t *testing.T, props string) (string, error) {
		doc := `{"settings":{"layout":{"mode":"full"},"base":{}},"root":[{"id":"sec","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}},"visual":` + props + `}}]}`
		page, err := ParsePage([]byte(doc))
		if err != nil {
			t.Fatalf("ParsePage: %v", err)
		}
		set, err := templates.NewEmbeddedComponentSet()
		if err != nil {
			t.Fatalf("组件模板 Set: %v", err)
		}
		compiled, err := Compile(page, WithComponentSet(set))
		if err != nil {
			return "", err
		}
		return compiled.CSS + compiled.ThemeVarsCSS, nil
	}

	// 各图案均有 background-image 输出（抽样：点阵/棋盘/蜂窝）。
	for _, kind := range []string{"dots", "checkerboard", "honeycomb", "zigzag", "ripple"} {
		css, err := compileDoc(t, `{"pattern":"`+kind+`"}`)
		if err != nil {
			t.Fatalf("%s 编译失败: %v", kind, err)
		}
		if !strings.Contains(css, "background-image") {
			t.Errorf("%s 未输出图案背景", kind)
		}
	}

	// 自定义图案色。
	css, err := compileDoc(t, `{"pattern":"dots","patternColor":"#ff0000"}`)
	if err != nil {
		t.Fatalf("图案色编译失败: %v", err)
	}
	if !strings.Contains(css, "#ff0000") {
		t.Errorf("图案色未生效")
	}

	// 互斥：图案 + 渐变 应报错。
	if _, err := compileDoc(t, `{"pattern":"dots","bgGradient":"linear-gradient(#000,#fff)"}`); err == nil {
		t.Errorf("图案与渐变应互斥")
	}
}
