package text

import (
	"strings"
	"testing"
)

// TestSanitizeRichHTMLImg 验证 img 白名单：合法 img 保留、危险协议/事件属性剥离。
func TestSanitizeRichHTMLImg(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "合法 img 完整属性保留",
			src:  `<p><img src="https://example.com/a.jpg" alt="说明" width="300" height="200" loading="lazy"></p>`,
			want: `<p><img src="https://example.com/a.jpg" alt="说明" width="300" height="200" loading="lazy"></p>`,
		},
		{
			name: "自闭合 img 输出为 void 元素",
			src:  `<img src="/img/x.png" alt="" />`,
			want: `<img src="/img/x.png" alt="">`,
		},
		{
			name: "javascript src 被拒",
			src:  `<img src="javascript:alert(1)" alt="x">`,
			want: `<img alt="x">`,
		},
		{
			name: "data src 被拒",
			src:  `<img src="data:image/png;base64,xxx">`,
			want: `<img>`,
		},
		{
			name: "事件属性 onerror/onclick 被剥离",
			src:  `<img src="https://e.com/a.jpg" onerror="alert(1)" onclick="x()">`,
			want: `<img src="https://e.com/a.jpg">`,
		},
		{
			name: "非法 width 被拒",
			src:  `<img src="https://e.com/a.jpg" width="abc" height="200px">`,
			want: `<img src="https://e.com/a.jpg" height="200px">`,
		},
		{
			name: "figure/figcaption 包裹保留",
			src:  `<figure><img src="https://e.com/a.jpg"><figcaption>图片说明</figcaption></figure>`,
			want: `<figure><img src="https://e.com/a.jpg"><figcaption>图片说明</figcaption></figure>`,
		},
		{
			name: "异常闭合 </img> 被忽略",
			src:  `<img src="https://e.com/a.jpg"></img>`,
			want: `<img src="https://e.com/a.jpg">`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("sanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
		})
	}
}

// TestSanitizeRichHTMLTextXSS C2 存储型 XSS 回归：文本节点必须重新转义，
// 实体编码的 <script>/<img> 解码后不得以真标签输出。
// 说明：Tokenizer 会把 &lt; 等实体解码为原始字符，修复后经 html.EscapeString
// 重新转义，输出往返等价（got == src），浏览器渲染为纯文本、不可执行。
func TestSanitizeRichHTMLTextXSS(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"实体编码 script", `&lt;script&gt;alert(1)&lt;/script&gt;`},
		{"实体编码 img onerror", `&lt;img src=x onerror=alert(1)&gt;`},
		{"双重编码 script", `&amp;lt;script&amp;gt;`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichHTML(c.src)
			// 修复后文本重新转义，编码输入应往返等价（渲染为字面文本）。
			if got != c.src {
				t.Errorf("sanitizeRichHTML(%q) = %q, 期望往返等价", c.src, got)
			}
			// 兜底断言：任何可执行标签形式都不得出现。
			for _, dangerous := range []string{"<script", "<img", "<iframe", "javascript:"} {
				if strings.Contains(got, dangerous) {
					t.Errorf("输出含可执行标签 %q: %q", dangerous, got)
				}
			}
		})
	}
}

// TestSanitizeRichHTMLScriptStripped 明文脚本标签剥壳：保留其文本内容（转义后），标签本体剥离。
func TestSanitizeRichHTMLScriptStripped(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"明文 script 剥壳保留文本", `<script>alert(1)</script>`, `alert(1)`},
		{"大写 SCRIPT 剥壳", `<SCRIPT>alert(1)</SCRIPT>`, `alert(1)`},
		{"script 混排正常段落", `<p>a</p><script>alert(1)</script>`, `<p>a</p>alert(1)`},
		{"非白名单 div 剥壳", `<div><p>x</p></div>`, `<p>x</p>`},
		// script 为 RAWTEXT 元素，tokenizer 不解码其内部实体（&lt; 保持字面），
		// 剥壳转义后输出 &amp;lt;，浏览器渲染为字面文本 "1 &lt; 2"，语义等价且不可执行。
		{"script 内文本实体转义", `<script>1 &lt; 2</script>`, `1 &amp;lt; 2`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("sanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
			// 兜底断言：剥壳后不得残留任何形式的 script 标签
			// （混排用例的 <p> 为白名单标签属正常输出，故只拦 script）。
			if strings.Contains(strings.ToLower(got), "<script") {
				t.Errorf("输出残留 script 标签: %q", got)
			}
		})
	}
}

// TestSanitizeRichHTMLRichTextPreserved 合法白名单富文本清洗后语义不变。
// 转义说明：文本中的 & < > 会被 html.EscapeString 规范化为等价实体
// （tokenizer 先解码为原始字符、EscapeString 再转回实体，往返一致），
// 浏览器渲染语义与输入完全等价，因此直接断言输出与输入一致。
// （注意 " ' 会被规范化为 &#34;/&#39; 数字实体，渲染等价但字节不同，故不纳入精确断言。）
func TestSanitizeRichHTMLRichTextPreserved(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "标题/段落/强调/列表/引用/链接/图片/换行复合文档",
			src: `<h2>标题</h2><p>加粗 <b>bold</b>、<strong>strong</strong>、<em>em</em></p>` +
				`<ul><li>项一</li><li>项二</li></ul><blockquote>引用</blockquote>` +
				`<a href="https://example.com/page" target="_blank" rel="nofollow noopener">链接</a>` +
				`<br><img src="https://example.com/i.png" alt="图" loading="lazy">` +
				`<h3>h3</h3><h4>h4</h4>`,
		},
		{
			name: "文本实体往返等价",
			src:  `<p>a &amp; b &lt;c&gt;</p>`,
		},
		{
			name: "有序列表与行内代码",
			src:  `<ol><li><code>x := 1</code></li></ol>`,
		},
		{
			name: "站内相对链接与锚点",
			src:  `<p><a href="/docs/intro">站内</a><a href="#sec2">锚点</a></p>`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichHTML(c.src)
			if got != c.src {
				t.Errorf("合法富文本被改写:\n got  = %q\n want = %q", got, c.src)
			}
		})
	}
}
