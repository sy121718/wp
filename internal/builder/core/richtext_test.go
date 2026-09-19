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

// TestRichTextHTMLRichText 富文本输入走白名单清洗：合法结构保留、脚本剥壳、标题级别原样。
func TestRichTextHTMLRichText(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"段落与强调保留", "<p>正文 <strong>加粗</strong> &amp; 更多</p>", "<p>正文 <strong>加粗</strong> &amp; 更多</p>"},
		{"script 剥壳保留文本", "<p>a</p><script>alert(1)</script>", "<p>a</p>alert(1)"},
		{"h1 原样保留（不再降级 h2）", "<h1>标题</h1>", "<h1>标题</h1>"},
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

// TestRichTextHTMLBlockExtensions 富文本扩展（rich-editor）产出的块级标签必须过白名单：
// 表格 / 折叠块 / 水平线 / h1~h5 保留，可执行标签与 on* 事件属性照旧剥掉。
//
// 这个用例对应验收里的两条：「合法标签保留」与「脚本被剥掉」——
//
//	· 保留 —— 编辑器点一次表格或折叠块，产出的标签必须能存下来（否则用户在编辑器里
//	  做的事保存后消失，而且不会报任何错）；
//	· 剥离 —— 白名单之外的标签（script/style/iframe）一律剥壳保留文本，文本节点重新转义，
//	  属性白名单里没有任何 on* 事件属性。
func TestRichTextHTMLBlockExtensions(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"表格结构与单元格文本保留",
			`<table><thead><tr><th>列一</th><th>列二</th></tr></thead><tbody><tr><td>a</td><td>b</td></tr></tbody></table>`,
			`<table><thead><tr><th>列一</th><th>列二</th></tr></thead><tbody><tr><td>a</td><td>b</td></tr></tbody></table>`,
		},
		{"表格属性一律剥离", `<table class="x" border="1"><tr><td colspan="2">a</td></tr></table>`, "<table><tr><td>a</td></tr></table>"},
		{"折叠块保留", "<details><summary>标题</summary><p>内容</p></details>", "<details><summary>标题</summary><p>内容</p></details>"},
		{"折叠块 open 属性剥离", "<details open><summary>标题</summary><p>内容</p></details>", "<details><summary>标题</summary><p>内容</p></details>"},
		{"水平线保留", "<p>上</p><hr><p>下</p>", "<p>上</p><hr><p>下</p>"},
		{"自闭合水平线归一为开标签", "<p>上</p><hr/><p>下</p>", "<p>上</p><hr><p>下</p>"},
		{"h1~h5 全级别保留", "<h1>一</h1><h2>二</h2><h3>三</h3><h4>四</h4><h5>五</h5>", "<h1>一</h1><h2>二</h2><h3>三</h3><h4>四</h4><h5>五</h5>"},
		{"iframe 剥壳（无文本内容）", `<p>a</p><iframe src="https://evil.example/x"></iframe>`, "<p>a</p>"},
		{"事件属性剥离", `<details onclick="alert(1)"><summary onmouseover="x()">t</summary><p>c</p></details>`, "<details><summary>t</summary><p>c</p></details>"},
		{"表格内的脚本剥壳", "<table><tr><td><script>alert(1)</script></td></tr></table>", "<table><tr><td>alert(1)</td></tr></table>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("SanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
			for _, dangerous := range []string{"<script", "<style", "<iframe", "onerror=", "onclick="} {
				if strings.Contains(got, dangerous) {
					t.Errorf("输出残留危险内容 %q: %q", dangerous, got)
				}
			}
			if again := SanitizeRichHTML(got); again != got {
				t.Errorf("非幂等: %q -> %q", got, again)
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

// TestRichTextHTMLDetailsFold 折叠块（details / summary）走统一入口 RichTextHTML 的完整链路：
// 结构完整（summary 与正文在同一块里）、脚本剥壳、事件属性剥离、幂等；
// 纯文本提取（StripRichTags）也必须同时拿到标题与正文 —— 少一样，meta 描述与 JSON-LD
// 就会把「折叠块的标题」与「折叠块的正文」粘成一串，或者丢掉其中一半。
func TestRichTextHTMLDetailsFold(t *testing.T) {
	src := `<details open onclick="alert(1)"><summary onmouseover="x()">折叠标题</summary><p>折叠正文 <strong>加粗</strong></p><script>alert(1)</script></details>`
	want := `<details><summary>折叠标题</summary><p>折叠正文 <strong>加粗</strong></p>alert(1)</details>`

	got := RichTextHTML(src)
	if got != want {
		t.Fatalf("RichTextHTML(%q) = %q, 期望 %q", src, got, want)
	}
	for _, bad := range []string{"<script", "<style", "<iframe", "onclick=", "onmouseover=", " open"} {
		if strings.Contains(got, bad) {
			t.Errorf("输出残留 %q: %q", bad, got)
		}
	}
	if again := RichTextHTML(got); again != got {
		t.Errorf("非幂等: %q -> %q", got, again)
	}
	plain := StripRichTags(RichTextHTML(src))
	for _, wantText := range []string{"折叠标题", "折叠正文", "加粗"} {
		if !strings.Contains(plain, wantText) {
			t.Errorf("纯文本提取漏字 %q：%q", wantText, plain)
		}
	}
}
