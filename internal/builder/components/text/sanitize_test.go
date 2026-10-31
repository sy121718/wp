package text

import (
	"strings"
	"testing"
)

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

			if got != c.src {
				t.Errorf("sanitizeRichHTML(%q) = %q, 期望往返等价", c.src, got)
			}

			for _, dangerous := range []string{"<script", "<img", "<iframe", "javascript:"} {
				if strings.Contains(got, dangerous) {
					t.Errorf("输出含可执行标签 %q: %q", dangerous, got)
				}
			}
		})
	}
}

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

		{"script 内文本实体转义", `<script>1 &lt; 2</script>`, `1 &amp;lt; 2`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("sanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}

			if strings.Contains(strings.ToLower(got), "<script") {
				t.Errorf("输出残留 script 标签: %q", got)
			}
		})
	}
}

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

			if again := sanitizeRichHTML(got); again != got {
				t.Errorf("标题清洗非幂等: %q -> %q", got, again)
			}
		})
	}
}

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
