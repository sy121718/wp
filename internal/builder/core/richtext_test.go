package core

import (
	"strings"
	"testing"
)

// TestRichTextHTMLPlainText 存量纯文本兼容：无标签输入一律 HTML 转义 + 段落包装，
// 绝不能当 HTML 原样输出（"1 < 2 & 更多" 会被浏览器解析成标签）。
func TestRichTextHTMLPlainText(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"空串", "", ""},
		{"单段转义", "价格 1 < 2 & 更多", "<p>价格 1 &lt; 2 &amp; 更多</p>"},
		{"空行分段", "第一段\n\n第二段", "<p>第一段</p><p>第二段</p>"},
		{"段内换行转 br", "第一段\n\n第二段\n换行", "<p>第一段</p><p>第二段<br>换行</p>"},
		{"CRLF 归一化", "A\r\n\r\nB", "<p>A</p><p>B</p>"},
		{"多余空行跳过", "A\n\n\n\nB", "<p>A</p><p>B</p>"},
		{"纯空白", "   \n\t\n", ""},
		{"非 ASCII 尖括号视为文本", "特殊 <字符> 内容", "<p>特殊 &lt;字符&gt; 内容</p>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RichTextHTML(c.src); got != c.want {
				t.Errorf("RichTextHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
		})
	}
}

// TestRichTextHTMLRichText 富文本输入走白名单清洗：合法结构保留、脚本剥壳、h1 降级。
func TestRichTextHTMLRichText(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"段落与强调保留", "<p>正文 <strong>加粗</strong> &amp; 更多</p>", "<p>正文 <strong>加粗</strong> &amp; 更多</p>"},
		{"script 剥壳保留文本", "<p>a</p><script>alert(1)</script>", "<p>a</p>alert(1)"},
		{"h1 降级 h2", "<h1>标题</h1>", "<h2>标题</h2>"},
		{"img 事件属性剥离", `<img src="https://e.com/a.jpg" onerror="alert(1)">`, `<img src="https://e.com/a.jpg">`},
		{"危险协议 img 剥壳", `<img src="javascript:alert(1)" alt="x">`, `<img alt="x">`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RichTextHTML(c.src); got != c.want {
				t.Errorf("RichTextHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
		})
	}
}

// TestRichTextHTMLLengthLimit 超长输入（富文本与纯文本同口径）判空，防产物膨胀。
func TestRichTextHTMLLengthLimit(t *testing.T) {
	long := strings.Repeat("x", MaxRichLen+1)
	if got := RichTextHTML(long); got != "" {
		t.Errorf("纯文本超长应判空，实际输出长度 %d", len(got))
	}
	if got := RichTextHTML("<p>" + long + "</p>"); got != "" {
		t.Errorf("富文本超长应判空，实际输出长度 %d", len(got))
	}
}

// TestRichTextHTMLIdempotent 幂等：段落化/清洗结果再次处理必须稳定
// （否则编辑器回填老数据会产生二次转义漂移）。
func TestRichTextHTMLIdempotent(t *testing.T) {
	srcs := []string{
		"第一段\n\n第二段\n换行",
		"1 < 2 & 更多",
		"<p>x <strong>y</strong></p><script>alert(1)</script>",
		"<h1>标题</h1><ul><li>项</li></ul>",
	}
	for _, src := range srcs {
		once := RichTextHTML(src)
		if again := RichTextHTML(once); again != once {
			t.Errorf("非幂等: RichTextHTML(%q) = %q, 再处理得 %q", src, once, again)
		}
	}
}

// TestHasRichMarkup 标签探测：只有真标签才算富文本。
func TestHasRichMarkup(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"", false},
		{"1 < 2 & 更多", false},
		{"特殊 <字符> 文本", false},
		{"<b>粗</b>", true},
		{"<script>alert(1)</script>", true},
		{"<div>非白名单标签也算富文本</div>", true},
	}
	for _, c := range cases {
		if got := HasRichMarkup(c.src); got != c.want {
			t.Errorf("HasRichMarkup(%q) = %v, 期望 %v", c.src, got, c.want)
		}
	}
}
