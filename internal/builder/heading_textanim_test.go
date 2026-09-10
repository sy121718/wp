package builder

// heading_textanim_test.go — 标题逐字/逐词入场动画的链路级验证（ParsePage → Compile → 产物）。
// 单元级断言见 internal/builder/components/heading/textanim_test.go；
// 本文件只证明用户配置确实落到产物 HTML 分段与 CSS 规则。

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

const headingTextAnimDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "textAnim", "description": "textAnim"}},
  "root": [
    {"id": "ha1", "type": "core.heading", "props": {"text": "标题动画", "textAnim": "chars", "textAnimDelay": 40}},
    {"id": "ha2", "type": "core.heading", "props": {"text": "高亮标题", "textAnim": "chars", "highlightColor": "#ffeb3b"}},
    {"id": "ha3", "type": "core.heading", "props": {"text": "普通标题"}}
  ]
}`

// compileHeadingTextAnimDoc 编译逐字动画用例文档，返回产物。
func compileHeadingTextAnimDoc(t *testing.T) *CompiledPage {
	t.Helper()
	p, err := ParsePage([]byte(headingTextAnimDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	res, err := Compile(p, WithComponentSet(set))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res
}

// TestCompileHeadingTextAnimHTML 逐字分段落到产物 HTML：每字一个 span；高亮盒与关闭态整段输出。
func TestCompileHeadingTextAnimHTML(t *testing.T) {
	res := compileHeadingTextAnimDoc(t)
	want := `<span class="sky-h-seg">标</span><span class="sky-h-seg">题</span><span class="sky-h-seg">动</span><span class="sky-h-seg">画</span>`
	if !strings.Contains(res.HTML, want) {
		t.Fatalf("产物缺少逐字分段 %s，HTML：%s", want, res.HTML)
	}
	if got := strings.Count(res.HTML, `class="sky-h-seg"`); got != 4 {
		t.Errorf("分段 span 数 = %d, want 4（仅 ha1 拆分）", got)
	}
	if !strings.Contains(res.HTML, `<span class="sky-heading-highlight">高亮标题</span>`) {
		t.Errorf("高亮盒应整段输出，HTML：%s", res.HTML)
	}
	if !strings.Contains(res.HTML, "普通标题") {
		t.Errorf("未开启动画的标题未输出原文，HTML：%s", res.HTML)
	}
}

// TestCompileHeadingTextAnimCSS 逐字动画 CSS 落到产物：分段基础规则 + 递增延迟 + 兜底档 + 关键帧。
func TestCompileHeadingTextAnimCSS(t *testing.T) {
	res := compileHeadingTextAnimDoc(t)
	css := res.CSS
	for _, want := range []string{
		".sky-c-ha1 .sky-h-seg {",
		"animation: sky-fade-up 0.6s ease backwards",
		".sky-c-ha1 .sky-h-seg:nth-child(1)",
		"animation-delay: 0ms",
		".sky-c-ha1 .sky-h-seg:nth-child(3)",
		"animation-delay: 80ms",
		".sky-c-ha1 .sky-h-seg:nth-child(n+21)",
		"animation-delay: 800ms",
		"@keyframes sky-fade-up",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("产物 CSS 缺少 %q", want)
		}
	}
	if strings.Contains(css, ".sky-c-ha2 .sky-h-seg") || strings.Contains(css, ".sky-c-ha3 .sky-h-seg") {
		t.Error("高亮盒/未开启节点不应产出分段规则")
	}
}
