package text

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestTextCSSLinkColorIsSeparateRule 链接色必须落在 `& a` 上，不能与正文色同规则。
//
// 两条 color 声明写进同一个选择器时后者覆盖前者，结果是「配了链接色 → 正文颜色被改掉，
// 而链接本身没变色」。这条不变量靠「两个选择器」维持，迁移到 CSS 文件后同样要钉住。
func TestTextCSSLinkColorIsSeparateRule(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Color: "#123456", LinkColor: "#ff0000"}, &b)
	out := b.String()
	if !strings.Contains(out, ".sky-c-t {\n  color: #123456;") {
		t.Errorf("正文色应落在容器上:\n%s", out)
	}
	if !strings.Contains(out, ".sky-c-t a {\n  color: #ff0000;") {
		t.Errorf("链接色应落在后代 a 上:\n%s", out)
	}
}

// TestTextCSSOptionalDecls 段间距与截断为可选：未配置则不产出对应规则。
//
// 注意「可选」的边界：全空实例**不**产出任何规则（含富文本排版基线）——
// 基线的应用条件是 isRichText（模式非纯文本 + 有内容或绑定），见 text.go。
// 下面第二个用例带 ParagraphSpacing + LineClamp，因此走的是「有配置」那一支。
// 选择器里包含 ol：早先只写了 p / ul / blockquote，有序列表拿不到段间距。
func TestTextCSSOptionalDecls(t *testing.T) {
	var plain core.CSSBuckets
	compileCSS("t", &Props{}, &plain)
	if out := strings.TrimSpace(plain.String()); out != "" {
		t.Errorf("全空的正文组件不应产出任何规则，实际:\n%s", out)
	}

	var b core.CSSBuckets
	compileCSS("t", &Props{ParagraphSpacing: "12px", LineClamp: 3}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t p, .sky-c-t ul, .sky-c-t ol, .sky-c-t blockquote {",
		"margin-top: 12px",
		"margin-bottom: 12px",
		"-webkit-line-clamp: 3",
		"display: -webkit-box",
		"overflow: hidden",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
}
