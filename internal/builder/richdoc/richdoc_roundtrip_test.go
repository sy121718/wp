package richdoc

// richdoc_roundtrip_test.go — 「等价转换」的背书（docs/06-B 决策 5 要求 round-trip fuzz）。
//
// 决策 5 写的是"双向唯一、round-trip fuzz 背书"。这里的"等价"有确切含义，分两条断言：
//
//	1. **收敛**：HTML → 节点 → HTML → 节点 → HTML，第二次导出的字节必须与第一次**逐字节相同**
//	   （否则"等价"只是演示用的巧合，真实数据里会逐次漂移）；
//	2. **等价**：第一次导出无损时（Lossless=true），两轮得到的节点树必须同构
//	   （类型 + 字段一致，ID 除外 —— ID 每次新建，本来就不同）。
//
// 有损的输入（不可逆组件、被丢弃的标签）只要求第 1 条：内容已经按规则损失过一次，
// 再要求第 2 条会把"有损但稳定"误判成缺陷。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// roundTripSeeds 种子语料：覆盖可逆子集的每个分支 + 几类有损输入。
var roundTripSeeds = []string{
	"",
	"<p>只有一个段落</p>",
	"<h2>标题</h2><p>正文</p><p>第二段</p>",
	`<p>前 <strong>粗</strong> 中 <em>斜</em> 后 <a href="/l">链接</a></p>`,
	`<ul><li>甲</li><li><a href="/x">乙</a></li></ul>`,
	`<ol><li>一</li><li>二</li></ol>`,
	`<blockquote>引用正文<cite>某人</cite></blockquote>`,
	`<pre>func main() {}</pre>`,
	// 代码语言（Trix 的 language 属性 / 既有 class 两种来源，清洗时归一到 class）：
	// 它必须能穿过 导入 → props → 导出 这条链路，否则编辑器里选的 go 到画布上就没了。
	`<pre class="language-go">func main() {}</pre>`,
	`<img src="/a.webp" alt="图一" title="标题一">`,
	`<figure><img src="/b.webp" alt="图二"><figcaption>图注</figcaption></figure>`,
	// 没有 alt 的正文图（Trix 附件就是这样产出的）：往返必须收敛，
	// 且图注回填（alt 补写）不能在第二轮再变一次。
	`<figure><img src="/d.webp"><figcaption>没有 alt 的图注</figcaption></figure>`,
	`<figure><img src="/e.webp"><figcaption>图注里的 <strong>行内格式</strong></figcaption></figure>`,
	`<hr>`,
	`<table><caption>表题</caption><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>`,
	`<p>文字<img src="/c.webp" alt="c">尾巴</p>`,
	// 有损输入：仍必须收敛（只是不要求节点同构）
	`<div><p>壳里的段落</p></div><script>alert(1)</script>`,
	`<custom-tag>未知标签</custom-tag>`,
	`<ul><li>带<strong>格式</strong>的项</li></ul>`,
}

// nodesSignature 节点树的规范化签名（排除 ID：ID 每次新建，不具备可比性）。
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

// checkRoundTrip 对一段 HTML 跑完整往返并校验两条断言。
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
	// 二次导出必须仍然无损（一次无损不该在第二轮变成有损）。
	if html1.Lossless && !html2.Lossless {
		t.Fatalf("第一次无损、第二次却有损：%v", html2.Warnings)
	}
}

func TestRoundTripSeeds(t *testing.T) {
	for _, src := range roundTripSeeds {
		t.Run(shortName(src), func(t *testing.T) { checkRoundTrip(t, src) })
	}
}

// FuzzRichTextRoundTrip 随机 HTML 上的往返 fuzz（决策 5 的背书）。
//
// 断言只有三条，但都是"不能让步"的：不 panic、导出结果永远能被再导入、往返收敛。
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

// shortName 给子测试起个可读名字（直接用 HTML 会把测试名搞成一团）。
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
