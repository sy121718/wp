package unit

// new_components_test.go — 第一批 WD 参照组件编译链路：
// slider（容器型轮播）/ list / infobox / social_buttons / video。

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"

	xhtml "golang.org/x/net/html"
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

	if !strings.Contains(html, "sky-slider") || !strings.Contains(html, "sky-slider-track") {
		t.Fatalf("缺少轮播结构: %s", html[:300])
	}
	if strings.Count(html, "sky-slide") < 2 {
		t.Fatalf("应有 2 个 slide: %s", html[:300])
	}
	if !strings.Contains(html, "Slide A") || !strings.Contains(html, "Slide B") {
		t.Fatalf("slide 内容应渲染: %s", html[:400])
	}
	if !strings.Contains(html, "sky-slider-prev") || !strings.Contains(html, "sky-slider-dots") {
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
	if !strings.Contains(html, "sky-list-icon") && !strings.Contains(html, "sky-list-marker") {
		t.Fatalf("缺少列表标记: %s", html[:300])
	}
	if !strings.Contains(html, "<svg") {
		t.Fatalf("图标应内联 SVG: %s", html[:400])
	}
	if !strings.Contains(css, "sky-list") {
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
	if !strings.Contains(css, "sky-infobox") {
		t.Fatalf("缺少信息框样式: %s", css[:200])
	}
}

func TestSocialButtonsRenderBrandIcons(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.social_buttons","id":"so1","props":{"color":"brand","items":[{"platform":"facebook","url":"https://fb.com"},{"platform":"instagram","url":"https://ig.com"}]}}]}`
	html, css := compileDoc(t, doc)

	if !strings.Contains(html, "sky-social-btn") {
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

	if !strings.Contains(html, "sky-tabs-radio") || !strings.Contains(html, "参数面板") || !strings.Contains(html, "评价面板") {
		t.Fatalf("页签结构缺失: %s", html[:400])
	}
	// 切换用 :has() 而不是「radio:checked ~ 面板」：radio 现在与标签同处导航容器（读屏才把
	// 单选组读成一组），已不是面板的前兄弟。
	if !strings.Contains(css, ":has(") || !strings.Contains(css, ":checked)") {
		t.Fatalf("页签切换 CSS 缺失（应为 :has(...:checked) 形式）: %s", css[:300])
	}
	// radio 不能 display:none —— 那会让它离开键盘序列，键盘用户无法切换页签。
	for _, line := range strings.Split(css, "\n") {
		if strings.Contains(line, "sky-tabs-radio") && strings.Contains(line, "display: none") {
			t.Fatalf("页签 radio 被 display:none 隐藏（键盘不可达）: %s", strings.TrimSpace(line))
		}
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
	markup, _ := compileDoc(t, doc)
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatalf("解析编译产物失败: %v", err)
	}
	var details []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "details" {
			details = append(details, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if len(details) != 2 {
		t.Fatalf("应有两个原生 details 折叠项，实际 %d: %s", len(details), markup)
	}
	for i, want := range []struct {
		title, answer string
		open          bool
	}{
		{title: "问题一", answer: "答案一", open: true},
		{title: "问题二", answer: "答案二"},
	} {
		item := details[i]
		if hasAttr(item, "open") != want.open {
			t.Errorf("第 %d 项默认展开状态错误: open=%v，期望 %v", i+1, hasAttr(item, "open"), want.open)
		}
		var summary *xhtml.Node
		for c := item.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode {
				summary = c
				break
			}
		}
		if summary == nil || summary.Data != "summary" {
			t.Fatalf("第 %d 项首个元素必须是原生 summary: %s", i+1, markup)
		}
		// 原生 summary 自带 Enter/Space 键盘切换；禁用或移出焦点序列会破坏它。
		if hasAttr(summary, "hidden") || hasAttr(summary, "disabled") || attr(summary, "tabindex") == "-1" || attr(summary, "aria-hidden") == "true" {
			t.Errorf("第 %d 项 summary 应保持键盘可达: %s", i+1, markup)
		}
		if !strings.Contains(textOf(summary), want.title) {
			t.Errorf("第 %d 项 summary 标题缺失: %s", i+1, markup)
		}
		if !strings.Contains(textOf(item), want.answer) {
			t.Errorf("第 %d 项折叠内容缺失: %s", i+1, markup)
		}
	}
}

func TestMarqueeRendersDuplicateTracks(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.marquee","id":"mq1","props":{"speed":10,"direction":"left"},"children":[
		{"type":"core.text","id":"mt1","props":{"mode":"plaintext","text":"促销横幅"}}
	]}]}`
	html, css := compileDoc(t, doc)

	if strings.Count(html, "sky-marquee-track") < 2 {
		t.Fatalf("应渲染双份轨道: %s", html[:300])
	}
	if !strings.Contains(css, "@keyframes sky-marquee-mq1") {
		t.Fatalf("滚动动画缺失: %s", css[:300])
	}
}

func TestCounterRendersValue(t *testing.T) {
	// 整数模式（decimals=0）：零 JS 计数——模板不输出 data-* 增强属性，
	// 数值由 @property <integer> + counter() 在 CSS 里生成。
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.counter","id":"ct1","props":{"start":0,"end":100,"suffix":"+"}}]}`
	html, css := compileDoc(t, doc)

	if !strings.Contains(html, "sky-counter") || !strings.Contains(html, "+") {
		t.Fatalf("计数器渲染缺失: %s", html[:300])
	}
	if strings.Contains(html, "data-counter=") {
		t.Fatalf("整数模式不应输出 data-* 增强属性（应走 CSS 计数）: %s", html[:400])
	}
	if !strings.Contains(css, "counter-reset: wpcount") || !strings.Contains(css, "content: counter(wpcount)") {
		t.Fatalf("CSS 计数规则缺失: %s", css[:400])
	}

	// 小数位模式：CSS counter 只支持整数，回退内嵌脚本增强（保留 data-*）。
	doc2 := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.counter","id":"ct2","props":{"start":0,"end":99.5,"decimals":1}}]}`
	html2, _ := compileDoc(t, doc2)
	if !strings.Contains(html2, `data-decimals="1"`) {
		t.Fatalf("小数模式应保留增强属性: %s", html2[:400])
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
