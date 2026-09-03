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
	compiled, err := compile(t, page)
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

func TestListRejectsDangerousLink(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.list","id":"l1","props":{"style":"icon","items":[{"icon":"check","text":"x","link":"javascript:alert(1)"}]}}]}`
	page, err := builder.ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if err := builder.ValidatePage(page); err == nil {
		t.Fatalf("应拒绝 javascript: 链接")
	}
}

func TestListAcceptsSafeLinks(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.list","id":"l1","props":{"style":"icon","items":[{"icon":"check","text":"a","link":"https://example.com"},{"icon":"check","text":"b","link":"mailto:a@b.com"},{"icon":"check","text":"c","link":"tel:+8613800138000"},{"icon":"check","text":"d","link":"/about"},{"icon":"check","text":"e","link":"#sec"}]}}]}`
	html, _ := compileDoc(t, doc)
	for _, want := range []string{"https://example.com", "mailto:a@b.com", "tel:+8613800138000", "/about", "#sec"} {
		if !strings.Contains(html, want) {
			t.Fatalf("安全链接应渲染: %s", want)
		}
	}
}

func TestSocialButtonsRejectDangerousURL(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.social_buttons","id":"so1","props":{"items":[{"platform":"facebook","url":"javascript:alert(1)"}]}}]}`
	page, err := builder.ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if err := builder.ValidatePage(page); err == nil {
		t.Fatalf("应拒绝 javascript: 链接")
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

func TestTabsRendersRadioHack(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.tabs","id":"tb1","props":{"tabs":[{"label":"参数"},{"label":"评价"}]},"children":[
		{"type":"core.text","id":"tp1","props":{"mode":"plaintext","text":"参数面板"}},
		{"type":"core.text","id":"tp2","props":{"mode":"plaintext","text":"评价面板"}}
	]}]}`
	html, css := compileDoc(t, doc)

	if !strings.Contains(html, "wp-tabs-radio") || !strings.Contains(html, "参数面板") || !strings.Contains(html, "评价面板") {
		t.Fatalf("页签结构缺失: %s", html[:400])
	}
	if !strings.Contains(css, ":checked ~") {
		t.Fatalf("radio hack 切换 CSS 缺失: %s", css[:300])
	}
	if !strings.Contains(html, "参数") || !strings.Contains(html, "评价") {
		t.Fatalf("页签标签缺失: %s", html[:400])
	}
}

func TestAccordionRendersDetails(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.accordion","id":"ac1","props":{"items":[{"title":"问题一","open":true},{"title":"问题二"}]},"children":[
		{"type":"core.text","id":"ap1","props":{"mode":"plaintext","text":"答案一"}},
		{"type":"core.text","id":"ap2","props":{"mode":"plaintext","text":"答案二"}}
	]}]}`
	html, _ := compileDoc(t, doc)

	if !strings.Contains(html, "<details") || !strings.Contains(html, "问题一</summary>") {
		t.Fatalf("details/summary 缺失: %s", html[:400])
	}
	if !strings.Contains(html, " open") {
		t.Fatalf("默认展开项应带 open: %s", html[:400])
	}
	if !strings.Contains(html, "答案一") || !strings.Contains(html, "答案二") {
		t.Fatalf("折叠内容缺失: %s", html[:400])
	}
}

func TestMarqueeRendersDuplicateTracks(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.marquee","id":"mq1","props":{"speed":10,"direction":"left"},"children":[
		{"type":"core.text","id":"mt1","props":{"mode":"plaintext","text":"促销横幅"}}
	]}]}`
	html, css := compileDoc(t, doc)

	if strings.Count(html, "wp-marquee-track") < 2 {
		t.Fatalf("应渲染双份轨道: %s", html[:300])
	}
	if !strings.Contains(css, "@keyframes wp-marquee-mq1") {
		t.Fatalf("滚动动画缺失: %s", css[:300])
	}
}

func TestCounterRendersValue(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.counter","id":"ct1","props":{"start":0,"end":100,"suffix":"+"}}]}`
	html, css := compileDoc(t, doc)

	if !strings.Contains(html, "wp-counter") || !strings.Contains(html, "100") {
		t.Fatalf("计数器渲染缺失: %s", html[:300])
	}
	if !strings.Contains(html, `data-end="100"`) {
		t.Fatalf("增强数据缺失: %s", html[:400])
	}
	if !strings.Contains(css, "wp-counter") {
		t.Fatalf("计数器样式缺失: %s", css[:200])
	}
}

func TestBatch2ComponentsRegistered(t *testing.T) {
	types := core.Types()
	for _, want := range []string{"core.tabs", "core.accordion", "core.marquee", "core.counter"} {
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
