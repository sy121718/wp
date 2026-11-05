package richdoc

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

var roundTripSeeds = []string{
	"",
	"<p>只有一个段落</p>",
	"<h2>标题</h2><p>正文</p><p>第二段</p>",
	`<p>前 <strong>粗</strong> 中 <em>斜</em> 后 <a href="/l">链接</a></p>`,
	`<ul><li>甲</li><li><a href="/x">乙</a></li></ul>`,
	`<ol><li>一</li><li>二</li></ol>`,
	`<blockquote>引用正文<cite>某人</cite></blockquote>`,
	`<pre>func main() {}</pre>`,

	`<pre class="language-go">func main() {}</pre>`,
	`<img src="/a.webp" alt="图一" title="标题一">`,
	`<figure><img src="/b.webp" alt="图二"><figcaption>图注</figcaption></figure>`,

	`<figure><img src="/d.webp"><figcaption>没有 alt 的图注</figcaption></figure>`,
	`<figure><img src="/e.webp"><figcaption>图注里的 <strong>行内格式</strong></figcaption></figure>`,
	`<hr>`,
	`<table><caption>表题</caption><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>`,
	`<p>文字<img src="/c.webp" alt="c">尾巴</p>`,

	`<div><p>壳里的段落</p></div><script>alert(1)</script>`,
	`<custom-tag>未知标签</custom-tag>`,
	`<ul><li>带<strong>格式</strong>的项</li></ul>`,
}

func nodesSignature(t *testing.T, nodes []*core.Node) string {
	t.Helper()
	var sb strings.Builder
	var walk func([]*core.Node)
	walk = func(list []*core.Node) {
		for _, n := range list {
			if n == nil {
				continue
			}
			sb.WriteString(n.Type)
			sb.WriteString("{")
			props := map[string]any{}
			if len(n.Props) > 0 {
				if err := json.Unmarshal(n.Props, &props); err != nil {
					t.Fatalf("props 解析失败：%v", err)
				}
			}
			keys := make([]string, 0, len(props))
			for k := range props {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sb.WriteString(fmt.Sprintf("%s=%v;", k, props[k]))
			}
			sb.WriteString("}")
			if len(n.Children) > 0 {
				sb.WriteString("[")
				walk(n.Children)
				sb.WriteString("]")
			}
		}
	}
	walk(nodes)
	return sb.String()
}

func checkRoundTrip(t *testing.T, src string) {
	t.Helper()

	first, err := HTMLToNodes(src)
	if err != nil {
		t.Fatalf("首次导入失败：%v", err)
	}
	html1, err := NodesToHTML(first.Nodes)
	if err != nil {
		t.Fatalf("首次导出失败：%v", err)
	}

	second, err := HTMLToNodes(html1.HTML)
	if err != nil {
		t.Fatalf("二次导入失败（导出结果必须是合法富文本 HTML）：%v\nHTML=%q", err, html1.HTML)
	}
	html2, err := NodesToHTML(second.Nodes)
	if err != nil {
		t.Fatalf("二次导出失败：%v", err)
	}

	if html2.HTML != html1.HTML {
		t.Fatalf("round-trip 不收敛\n输入    %q\n第一次  %q\n第二次  %q", src, html1.HTML, html2.HTML)
	}
	if html1.Lossless {
		s1 := nodesSignature(t, first.Nodes)
		s2 := nodesSignature(t, second.Nodes)
		if s1 != s2 {
			t.Fatalf("无损往返后节点树变了\n输入   %q\n第一次 %s\n第二次 %s\nHTML=%q", src, s1, s2, html1.HTML)
		}
	}

	if html1.Lossless && !html2.Lossless {
		t.Fatalf("第一次无损、第二次却有损：%v", html2.Warnings)
	}
}

func TestRoundTripSeeds(t *testing.T) {
	for _, src := range roundTripSeeds {
		t.Run(shortName(src), func(t *testing.T) { checkRoundTrip(t, src) })
	}
}

func FuzzRichTextRoundTrip(f *testing.F) {
	for _, s := range roundTripSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 20000 {
			t.Skip("超长输入走截断路径，另有专项测试")
		}
		checkRoundTrip(t, src)
	})
}

func shortName(src string) string {
	if strings.TrimSpace(src) == "" {
		return "empty"
	}
	name := src
	if len(name) > 40 {
		name = name[:40]
	}
	return strings.NewReplacer("<", "", ">", "", "/", "", " ", "_", "\"", "").Replace(name)
}
