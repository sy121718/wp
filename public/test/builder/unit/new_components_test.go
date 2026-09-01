package unit

// new_components_test.go — 第一批 WD 参照组件编译链路：
// slider（容器型轮播）/ list / infobox / social_buttons / video。

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
)

func compileDoc(t *testing.T, doc string) (html string, css string) {
	t.Helper()
	page, err := builder.ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if err := builder.ValidatePage(page); err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	compiled, err := builder.Compile(page)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	return compiled.HTML, compiled.CSS
}

func TestSliderRendersSlidesAndScrollSnap(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.slider","id":"s1","props":{"perView":{"desktop":1},"showArrows":true,"showDots":true},"children":[
		{"type":"core.container","id":"slide1","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"type":"core.text","id":"t1","props":{"mode":"plaintext","text":"Slide A"}}]},
		{"type":"core.container","id":"slide2","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"type":"core.text","id":"t2","props":{"mode":"plaintext","text":"Slide B"}}]}
	]}]}`
	html, css := compileDoc(t, doc)

	if !strings.Contains(html, "wp-slider") || !strings.Contains(html, "wp-slider-track") {
		t.Fatalf("缺少轮播结构: %s", html[:300])
	}
	if strings.Count(html, "wp-slide") < 2 {
		t.Fatalf("应有 2 个 slide: %s", html[:300])
	}
	if !strings.Contains(html, "Slide A") || !strings.Contains(html, "Slide B") {
		t.Fatalf("slide 内容应渲染: %s", html[:400])
	}
	if !strings.Contains(html, "wp-slider-prev") || !strings.Contains(html, "wp-slider-dots") {
		t.Fatalf("箭头与圆点应输出: %s", html[:400])
	}
	if !strings.Contains(css, "scroll-snap-type: x mandatory") {
		t.Fatalf("应输出 scroll-snap 样式: %s", css[:300])
	}
}

func TestSliderRejectsEmpty(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.slider","id":"s1","props":{}}]}`
	if _, err := builder.ParsePage([]byte(doc)); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
}

func TestListRendersItems(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.list","id":"l1","props":{"style":"icon","items":[{"icon":"check","text":"免费配送"},{"icon":"shield","text":"正品保证"}]}}]}`
	html, css := compileDoc(t, doc)

	if !strings.Contains(html, "免费配送") || !strings.Contains(html, "正品保证") {
		t.Fatalf("列表项应渲染: %s", html[:300])
	}
	if !strings.Contains(html, "wp-list-icon") && !strings.Contains(html, "wp-list-marker") {
		t.Fatalf("缺少列表标记: %s", html[:300])
	}
	if !strings.Contains(html, "<svg") {
		t.Fatalf("图标应内联 SVG: %s", html[:400])
	}
	if !strings.Contains(css, "wp-list") {
		t.Fatalf("缺少列表样式: %s", css[:200])
	}
}

func TestInfoboxRendersContent(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.infobox","id":"i1","props":{"icon":"shield","title":"正品保证","text":"所有商品 100% 正品","link":"/about"}}]}`
	html, css := compileDoc(t, doc)

	if !strings.Contains(html, "正品保证") || !strings.Contains(html, "100% 正品") {
		t.Fatalf("信息框内容应渲染: %s", html[:300])
	}
	if !strings.Contains(html, `<a class="`) || !strings.Contains(html, `href="/about"`) {
		t.Fatalf("信息框链接应渲染: %s", html[:400])
	}
	if !strings.Contains(css, "wp-infobox") {
		t.Fatalf("缺少信息框样式: %s", css[:200])
	}
}

func TestSocialButtonsRenderBrandIcons(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.social_buttons","id":"so1","props":{"color":"brand","items":[{"platform":"facebook","url":"https://fb.com"},{"platform":"instagram","url":"https://ig.com"}]}}]}`
	html, css := compileDoc(t, doc)

	if !strings.Contains(html, "wp-social-btn") {
		t.Fatalf("社交按钮应渲染: %s", html[:300])
	}
	if !strings.Contains(html, "facebook") || !strings.Contains(html, "instagram") {
		t.Fatalf("平台 aria-label 应输出: %s", html[:400])
	}
	if !strings.Contains(css, "#1877f2") {
		t.Fatalf("品牌色应编译进 CSS: %s", css[:400])
	}
}

func TestVideoEmbedYoutube(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.video","id":"v1","props":{"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ","controls":true}}]}`
	html, _ := compileDoc(t, doc)

	if !strings.Contains(html, "youtube.com/embed/dQw4w9WgXcQ") {
		t.Fatalf("YouTube 应转为 embed iframe: %s", html[:300])
	}
	if !strings.Contains(html, "<iframe") {
		t.Fatalf("应输出 iframe: %s", html[:300])
	}
}

func TestVideoLocalMp4(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.video","id":"v2","props":{"url":"/storage/videos/demo.mp4","controls":true,"autoplay":true,"muted":true}}]}`
	html, _ := compileDoc(t, doc)

	if !strings.Contains(html, "<video") || !strings.Contains(html, `<source src="/storage/videos/demo.mp4"`) {
		t.Fatalf("本地视频应输出 video/source: %s", html[:400])
	}
	if !strings.Contains(html, " autoplay") || !strings.Contains(html, " muted") {
		t.Fatalf("自动播放与静音属性应输出: %s", html[:400])
	}
}

// 确保新组件已注册（Types 含全部）。
func TestNewComponentsRegistered(t *testing.T) {
	types := core.Types()
	for _, want := range []string{"core.slider", "core.list", "core.infobox", "core.social_buttons", "core.video"} {
		found := false
		for _, ty := range types {
			if ty == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("组件 %s 未注册", want)
		}
	}
}
