package richtext

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestRichTextHTMLLinkProtocols(t *testing.T) {
	kept := []struct {
		name string
		src  string
		want string
	}{
		{"tel 电话链接保留", `<a href="tel:13800000000">打电话</a>`, `<a href="tel:13800000000">打电话</a>`},
		{"sms 短信链接保留", `<a href="sms:13800000000">发短信</a>`, `<a href="sms:13800000000">发短信</a>`},
		{"ftp 链接保留", `<a href="ftp://files.example/pub">文件</a>`, `<a href="ftp://files.example/pub">文件</a>`},
		{"ftps 链接保留", `<a href="ftps://files.example/pub">文件</a>`, `<a href="ftps://files.example/pub">文件</a>`},
		{"callto 链接保留", `<a href="callto:skype-user">呼叫</a>`, `<a href="callto:skype-user">呼叫</a>`},
		{"cid 链接保留", `<a href="cid:part1@example">附件</a>`, `<a href="cid:part1@example">附件</a>`},
		{"xmpp 链接保留", `<a href="xmpp:user@example.com">聊天</a>`, `<a href="xmpp:user@example.com">聊天</a>`},
		{"matrix 链接保留", `<a href="matrix:u/user:example.com">聊天</a>`, `<a href="matrix:u/user:example.com">聊天</a>`},
		{"http/https 原口径不变", `<a href="https://example.com/a?b=1&amp;c=2">站外</a>`, `<a href="https://example.com/a?b=1&amp;c=2">站外</a>`},
		{"锚点与站内相对路径保留", `<a href="/admin/articles">列表</a><a href="#top">回顶</a>`, `<a href="/admin/articles">列表</a><a href="#top">回顶</a>`},
		{"协议大小写不敏感（大写 TEL）", `<a href="TEL:13800000000">打电话</a>`, `<a href="TEL:13800000000">打电话</a>`},
	}
	for _, c := range kept {
		t.Run(c.name, func(t *testing.T) {
			got := SanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("SanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
			if again := SanitizeRichHTML(got); again != got {
				t.Errorf("非幂等: %q -> %q", got, again)
			}
		})
	}

	dropped := []struct {
		name string
		src  string
		want string
	}{
		{"javascript 协议被剥", `<a href="javascript:alert(1)">点我</a>`, `<a>点我</a>`},
		{"大小写混合 JaVaScRiPt 被剥", `<a href="JaVaScRiPt:alert(1)">点我</a>`, `<a>点我</a>`},
		{"data 协议被剥", `<a href="data:text/html;base64,PHNjcmlwdD4=">点我</a>`, `<a>点我</a>`},
		{"vbscript 协议被剥", `<a href="vbscript:msgbox(1)">点我</a>`, `<a>点我</a>`},
		{"前导空格绕过被剥", "<a href=\" javascript:alert(1)\">点我</a>", `<a>点我</a>`},
		{"前导制表符绕过被剥", "<a href=\"	javascript:alert(1)\">点我</a>", `<a>点我</a>`},
		{"协议中间插制表符绕过被剥", "<a href=\"java	script:alert(1)\">点我</a>", `<a>点我</a>`},
		{"实体编码的冒号同样被剥", `<a href="javascript&colon;alert(1)">点我</a>`, `<a>点我</a>`},
		{"不在白名单的协议被剥", `<a href="blob:https://example.com/x">点我</a>`, `<a>点我</a>`},
		{"img src 同样过协议白名单", `<img src="javascript:alert(1)" alt="图">`, `<img alt="图">`},
		{"img src 混合大小写同样被剥", `<img src="Data:text/html,<script>alert(1)</script>">`, `<img>`},
	}
	for _, c := range dropped {
		t.Run(c.name, func(t *testing.T) {
			got := SanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("SanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
			lower := strings.ToLower(got)
			for _, bad := range []string{"javascript:", "data:", "vbscript:", "blob:"} {
				if strings.Contains(lower, bad) {
					t.Errorf("输出残留危险协议 %q: %q", bad, got)
				}
			}
			if again := SanitizeRichHTML(got); again != got {
				t.Errorf("非幂等: %q -> %q", got, again)
			}
		})
	}
}

func TestRichTextHTMLPreLanguage(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"language 属性归一为 class", `<pre language="go">x := 1</pre>`, `<pre class="language-go">x := 1</pre>`},
		{"已有 language-xxx class 保留", `<pre class="language-python">print(1)</pre>`, `<pre class="language-python">print(1)</pre>`},
		{"混合 class 只保留语言那一个", `<pre class="code language-js other">1</pre>`, `<pre class="language-js">1</pre>`},
		{"c++ 这类带符号的语言保留", `<pre language="c++">int main(){}</pre>`, `<pre class="language-c++">int main(){}</pre>`},
		{"C# 保留", `<pre language="C#">x</pre>`, `<pre class="language-C#">x</pre>`},
		{"无属性 pre 不受影响", `<pre>code</pre>`, `<pre>code</pre>`},
		{"event 属性仍被剥（合法语言保留）", `<pre onclick="alert(1)" language="go">x</pre>`, `<pre class="language-go">x</pre>`},
		{"带引号的语言值丢弃整个属性", `<pre language='go" onload="alert(1)'>x</pre>`, `<pre>x</pre>`},
		{"带空格的语言值丢弃", `<pre language="go lang">x</pre>`, `<pre>x</pre>`},
		{"带尖括号的语言值丢弃", `<pre language="<script>">x</pre>`, `<pre>x</pre>`},
		{"超长语言值丢弃", `<pre language="` + strings.Repeat("a", 33) + `">x</pre>`, `<pre>x</pre>`},
		{"空语言值丢弃", `<pre language="">x</pre>`, `<pre>x</pre>`},
		{"class 里的危险语言值丢弃", `<pre class="language-<script>">x</pre>`, `<pre>x</pre>`},
		{"data-* 仍被剥", `<pre language="go" data-x="1">x</pre>`, `<pre class="language-go">x</pre>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("SanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
			for _, bad := range []string{"onload=", "onclick=", "<script"} {
				if strings.Contains(got, bad) {
					t.Errorf("输出残留危险内容 %q: %q", bad, got)
				}
			}
			if again := SanitizeRichHTML(got); again != got {
				t.Errorf("非幂等: %q -> %q", got, again)
			}
		})
	}
}

func TestRichTextHTMLFigureAltBackfill(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"缺 alt 时用图注回填",
			`<figure><img src="/a.webp"><figcaption>图注</figcaption></figure>`,
			`<figure><img src="/a.webp" alt="图注"><figcaption>图注</figcaption></figure>`,
		},
		{
			"图注在 img 之前也回填",
			`<figure><figcaption>图注</figcaption><img src="/a.webp"></figure>`,
			`<figure><figcaption>图注</figcaption><img src="/a.webp" alt="图注"></figure>`,
		},
		{
			"已有 alt 不被覆盖",
			`<figure><img src="/a.webp" alt="作者写的描述"><figcaption>图注</figcaption></figure>`,
			`<figure><img src="/a.webp" alt="作者写的描述"><figcaption>图注</figcaption></figure>`,
		},
		{
			"显式空 alt（装饰图）不被覆盖",
			`<figure><img src="/a.webp" alt=""><figcaption>图注</figcaption></figure>`,
			`<figure><img src="/a.webp" alt=""><figcaption>图注</figcaption></figure>`,
		},
		{
			"图注里的行内格式取纯文本",
			`<figure><img src="/a.webp"><figcaption>关于 <strong>降噪</strong> 的说明</figcaption></figure>`,
			`<figure><img src="/a.webp" alt="关于 降噪 的说明"><figcaption>关于 <strong>降噪</strong> 的说明</figcaption></figure>`,
		},
		{
			"空图注不回填",
			`<figure><img src="/a.webp"><figcaption>   </figcaption></figure>`,
			`<figure><img src="/a.webp"><figcaption>   </figcaption></figure>`,
		},
		{
			"figure 外的 img 不受影响",
			`<img src="/a.webp"><figcaption>图注</figcaption>`,
			`<img src="/a.webp"><figcaption>图注</figcaption>`,
		},
		{
			"恶意图注被转义",
			`<figure><img src="/a.webp"><figcaption>"><script>alert(1)</script></figcaption></figure>`,
			`<figure><img src="/a.webp" alt="&quot;&gt;alert(1)"><figcaption>&#34;&gt;alert(1)</figcaption></figure>`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SanitizeRichHTML(c.src)
			if got != c.want {
				t.Errorf("SanitizeRichHTML(%q) = %q, 期望 %q", c.src, got, c.want)
			}
			if again := SanitizeRichHTML(got); again != got {
				t.Errorf("非幂等: %q -> %q", got, again)
			}

			assertNoScriptNode(t, got)
		})
	}
}

func TestRichTextHTMLFigureAltTruncated(t *testing.T) {
	long := strings.Repeat("图", 260)
	src := `<figure><img src="/a.webp"><figcaption>` + long + `</figcaption></figure>`
	got := SanitizeRichHTML(src)
	alt := attrValue(t, got, "img", "alt")
	if n := len([]rune(alt)); n != 200 {
		t.Errorf("alt 应截断到 200 字符，实际 %d", n)
	}
	if !strings.HasPrefix(alt, "图") {
		t.Errorf("alt 内容异常: %q", alt)
	}
	if again := SanitizeRichHTML(got); again != got {
		t.Errorf("非幂等: %q -> %q", got, again)
	}
}

func attrValue(t *testing.T, fragment, tag, attr string) string {
	t.Helper()
	doc, err := xhtml.Parse(strings.NewReader(fragment))
	if err != nil {
		t.Fatalf("输出无法解析：%v\n%s", err, fragment)
	}
	n := findTag(doc, tag)
	if n == nil {
		t.Fatalf("输出里找不到 <%s>：%s", tag, fragment)
	}
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, attr) {
			return a.Val
		}
	}
	return ""
}

func findTag(n *xhtml.Node, tag string) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findTag(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func assertNoScriptNode(t *testing.T, fragment string) {
	t.Helper()
	doc, err := xhtml.Parse(strings.NewReader(fragment))
	if err != nil {
		t.Fatalf("输出无法解析：%v\n%s", err, fragment)
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "script", "iframe", "style", "object", "embed":
				t.Fatalf("输出里出现了 %s 节点：%s", n.Data, fragment)
			}
			for _, a := range n.Attr {
				if strings.HasPrefix(strings.ToLower(a.Key), "on") {
					t.Fatalf("输出里出现了事件属性 %s：%s", a.Key, fragment)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}
