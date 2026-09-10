package builder

// image_skeleton_test.go — 图片懒加载三态 + 主题骨架屏（docs/09 §3 图片优化）。
import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

func TestImageSkeletonAndLoading(t *testing.T) {
	set, _ := templates.NewEmbeddedComponentSet()
	// 主题：懒加载开启 + 骨架开启
	theme := &ThemeSettings{Images: ThemeImages{LazyLoad: "on", Skeleton: true}}

	// 1) 组件未设置 → 继承主题：lazy + 骨架
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"src":"/storage/a.jpg","alt":"A"}}]}`
	page, _ := ParsePage([]byte(doc))
	c, err := Compile(page, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(c.HTML, `loading="lazy"`) {
		t.Errorf("应继承主题懒加载\n%s", c.HTML)
	}
	if !strings.Contains(c.HTML, "is-skeleton") {
		t.Errorf("应输出 is-skeleton 类\n%s", c.HTML)
	}
	if !strings.Contains(c.CSS, "sky-skeleton-shimmer") {
		t.Errorf("应输出骨架 keyframes\n%s", c.CSS)
	}

	// 2) 组件 off 覆盖主题：eager 且无骨架
	doc2 := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i2","type":"core.image","props":{"src":"/storage/b.jpg","alt":"B","loading":"off"}}]}`
	page2, _ := ParsePage([]byte(doc2))
	c2, err := Compile(page2, WithComponentSet(set), WithThemeSettings(theme))
	if err != nil {
		t.Fatalf("compile2: %v", err)
	}
	if !strings.Contains(c2.HTML, `loading="eager"`) {
		t.Errorf("组件 off 应覆盖主题为 eager\n%s", c2.HTML)
	}
	if strings.Contains(c2.HTML, "is-skeleton") {
		t.Errorf("立即加载不应有骨架\n%s", c2.HTML)
	}

	// 3) 主题关闭懒加载 → 默认即 eager
	themeOff := &ThemeSettings{Images: ThemeImages{LazyLoad: "off"}}
	c3, err := Compile(page, WithComponentSet(set), WithThemeSettings(themeOff))
	if err != nil {
		t.Fatalf("compile3: %v", err)
	}
	if !strings.Contains(c3.HTML, `loading="eager"`) {
		t.Errorf("主题关闭懒加载时默认应为 eager\n%s", c3.HTML)
	}
}
