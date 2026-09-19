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
			// 图注回填：img 没有 alt 时用同级 figcaption 的纯文本补上（SEO 图片检查要的 alt）。
			// 回填只写 alt，figcaption 本身一个字都不改。
			name: "figure/figcaption 包裹保留且回填 alt",
			src:  `<figure><img src="https://e.com/a.jpg"><figcaption>图片说明</figcaption></figure>`,
			want: `<figure><img src="https://e.com/a.jpg" alt="图片说明"><figcaption>图片说明</figcaption></figure>`,
		},
		{
			name: "已有 alt 的图不被图注覆盖",
			src:  `<figure><img src="https://e.com/a.jpg" alt="作者写的"><figcaption>图片说明</figcaption></figure>`,
			want: `<figure><img src="https://e.com/a.jpg" alt="作者写的"><figcaption>图片说明</figcaption></figure>`,
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

// TestSanitizeRichHTMLHeadingLevels h1~h5 原样保留。
//
// 历史：这里原本断言 h1 统一降级为 h2（一页一个 H1 由页面标题承担）。
// 本轮产品要求编辑器提供 h1~h5 五个级别、白名单放行 h1，降级随之取消 ——
// 否则会出现「编辑器里点 H1、保存后变 H2」的所见非所存。
func TestSanitizeRichHTMLHeadingLevels(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"h1 原样保留", `<h1>大标题</h1>`, `<h1>大标题</h1>`},
		{"h1 属性一并剥离", `<h1 class="x" id="y">标题</h1>`, `<h1>标题</h1>`},
		{"五级标题全保留", `<h1>一</h1><h2>二</h2><h3>三</h3><h4>四</h4><h5>五</h5>`, `<h1>一</h1><h2>二</h2><h3>三</h3><h4>四</h4><h5>五</h5>`},
		{"自闭合 h1 归一为开标签", `<h1/>`, `<h1>`},
		{"再清洗幂等", `<h1>标题</h1>`, `<h1>标题</h1>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("sanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
			// 幂等：标题级别保留后输出再次清洗必须稳定（fuzz 不变式）。
			if again := sanitizeRichHTML(got); again != got {
				t.Errorf("标题清洗非幂等: %q -> %q", got, again)
			}
		})
	}
}

// TestSanitizeRichHTMLPreCodeBlock Trix 代码块输出 pre，必须在白名单内且内容转义。
func TestSanitizeRichHTMLPreCodeBlock(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"pre 代码块保留", `<pre>x := 1</pre>`, `<pre>x := 1</pre>`},
		{"pre 属性剥离", `<pre class="code" data-x="1">code</pre>`, `<pre>code</pre>`},
		{"pre 内实体往返等价", `<pre>a &lt; b</pre>`, `<pre>a &lt; b</pre>`},
		{"标题与代码块组合保留", `<h1>标题</h1><pre>code</pre>`, `<h1>标题</h1><pre>code</pre>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("sanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
			if again := sanitizeRichHTML(got); again != got {
				t.Errorf("pre 清洗非幂等: %q -> %q", got, again)
			}
		})
	}
}

// TestSanitizeRichHTMLBlockElements 富文本扩展的块级标签（表格 / 折叠块 / 水平线）：
// 合法结构保留、危险标签剥壳、事件属性全剥离 —— 与 core 包的
// TestRichTextHTMLBlockExtensions 是同一组契约，这里再从组件层（text 包的转发入口）钉一遍。
func TestSanitizeRichHTMLBlockElements(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"表格保留", `<table><thead><tr><th>列</th></tr></thead><tbody><tr><td>值</td></tr></tbody></table>`, `<table><thead><tr><th>列</th></tr></thead><tbody><tr><td>值</td></tr></tbody></table>`},
		{"表格属性剥离", `<table class="x"><tr><td colspan="2">a</td></tr></table>`, `<table><tr><td>a</td></tr></table>`},
		{"折叠块保留", `<details><summary>题</summary><p>答</p></details>`, `<details><summary>题</summary><p>答</p></details>`},
		{"折叠块 open 剥离", `<details open><summary>题</summary><p>答</p></details>`, `<details><summary>题</summary><p>答</p></details>`},
		{"水平线保留", `<p>上</p><hr><p>下</p>`, `<p>上</p><hr><p>下</p>`},
		{"iframe 剥壳", `<p>a</p><iframe src="https://evil.example/x"></iframe>`, `<p>a</p>`},
		{"事件属性剥离", `<details onclick="alert(1)"><summary onmouseover="x()">t</summary><p>c</p></details>`, `<details><summary>t</summary><p>c</p></details>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("sanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
			for _, dangerous := range []string{"<script", "<style", "<iframe", "onerror=", "onclick=", "onmouseover="} {
				if strings.Contains(got, dangerous) {
					t.Errorf("输出残留危险内容 %q: %q", dangerous, got)
				}
			}
			if again := sanitizeRichHTML(got); again != got {
				t.Errorf("非幂等: %q -> %q", got, again)
			}
		})
	}
}

// TestBuildViewKeepsDetailsFold 折叠块（details/summary）走组件渲染链路（core.text 富文本模式）：
// 清洗后的视图里折叠结构完整，脚本与事件属性被剔除。BuildView 的输出由 text.jet 用 unsafe
// 直接写进产物，所以这里是「清洗」与「渲染」之间唯一的关口 —— 只测 core 包的清洗函数
// 覆盖不到「视图字段给错/走错分支」，那样页面会安静地少掉折叠块。
func TestBuildViewKeepsDetailsFold(t *testing.T) {
	src := `<details onclick="alert(1)"><summary onmouseover="x()">折叠标题</summary><p>折叠正文<script>alert(1)</script></p></details>`
	view, err := BuildView(&Props{Mode: ModeRichText, Text: src}, nil)
	if err != nil {
		t.Fatalf("BuildView 失败：%v", err)
	}
	if view.IsPlain {
		t.Fatal("富文本模式不该走纯文本分支")
	}
	want := `<details><summary>折叠标题</summary><p>折叠正文alert(1)</p></details>`
	if view.SanitizedContent != want {
		t.Errorf("SanitizedContent = %q, 期望 %q", view.SanitizedContent, want)
	}
	for _, bad := range []string{"<script", "<style", "<iframe", "onclick=", "onmouseover="} {
		if strings.Contains(view.SanitizedContent, bad) {
			t.Errorf("视图残留危险内容 %q：%q", bad, view.SanitizedContent)
		}
	}
	if again := sanitizeRichHTML(view.SanitizedContent); again != view.SanitizedContent {
		t.Errorf("非幂等: %q -> %q", view.SanitizedContent, again)
	}
}
