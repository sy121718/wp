package richdoc

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

func propsOf(t *testing.T, n *core.Node) map[string]any {
	t.Helper()
	out := map[string]any{}
	if len(n.Props) == 0 {
		return out
	}
	if err := json.Unmarshal(n.Props, &out); err != nil {
		t.Fatalf("props 解析失败：%v", err)
	}
	return out
}

func TestHTMLToNodesBlockMapping(t *testing.T) {
	src := `<h2>二级标题</h2>
<p>一个段落</p>
<ul><li>第一项</li><li><a href="/x">第二项</a></li></ul>
<ol><li>步骤一</li></ol>
<blockquote>引用正文<cite>某人</cite></blockquote>
<pre>code()</pre>
<img src="/a.webp" alt="图一">
<figure><img src="/b.webp" alt="图二"><figcaption>图注</figcaption></figure>
<hr>
<table><caption>表题</caption><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>`

	res, err := HTMLToNodes(src)
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	gotTypes := make([]string, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		gotTypes = append(gotTypes, n.Type)
	}
	wantTypes := []string{
		"core.heading", "core.text", "core.list", "core.list", "core.quote",
		"core.text", "core.image", "core.image", "core.divider", "core.table",
	}
	if strings.Join(gotTypes, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("组件序列不符\n实际 %v\n预期 %v", gotTypes, wantTypes)
	}

	if p := propsOf(t, res.Nodes[0]); p["tag"] != "h2" || p["text"] != "二级标题" {
		t.Errorf("标题映射错误：%v", p)
	}
	if p := propsOf(t, res.Nodes[2]); p["style"] != "dot" {
		t.Errorf("ul 应为圆点样式：%v", p)
	}
	if p := propsOf(t, res.Nodes[3]); p["style"] != "number" {
		t.Errorf("ol 应为序号样式：%v", p)
	}

	if p := propsOf(t, res.Nodes[4]); p["author"] != "某人" {
		t.Errorf("引用作者未提取：%v", p)
	} else if text, _ := p["text"].(string); strings.Contains(text, "某人") {
		t.Errorf("作者名在正文里重复出现：%q", text)
	}

	if p := propsOf(t, res.Nodes[7]); p["caption"] != "图注" || p["src"] != "/b.webp" {
		t.Errorf("图片映射错误：%v", p)
	}

	if p := propsOf(t, res.Nodes[9]); p["caption"] != "表题" {
		t.Errorf("表格标题错误：%v", p)
	} else if hdrs, _ := p["headers"].([]any); len(hdrs) != 2 || hdrs[0] != "A" {
		t.Errorf("表头错误：%v", p["headers"])
	} else if rows, _ := p["rows"].([]any); len(rows) != 1 {
		t.Errorf("数据行错误：%v", p["rows"])
	}
}

func TestHTMLToNodesKeepsInlineFormatInText(t *testing.T) {
	src := `<p>前 <strong>粗</strong> 中 <a href="/l">链接</a> 后</p>`
	res, err := HTMLToNodes(src)
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	if len(res.Nodes) != 1 || res.Nodes[0].Type != "core.text" {
		t.Fatalf("行内格式应合并进一个 core.text，实际 %d 个节点", len(res.Nodes))
	}
	text, _ := propsOf(t, res.Nodes[0])["text"].(string)
	for _, want := range []string{"<strong>粗</strong>", `href="/l"`} {
		if !strings.Contains(text, want) {
			t.Errorf("行级格式丢失 %q：%q", want, text)
		}
	}
}

func TestHTMLToNodesSplitsImageInsideParagraph(t *testing.T) {

	src := `<p>文字<img src="/x.webp" alt="x">尾巴</p>`
	res, err := HTMLToNodes(src)
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	if len(res.Nodes) != 3 {
		t.Fatalf("应拆成 文字 / 图片 / 尾巴 三段，实际 %d 段", len(res.Nodes))
	}
	if res.Nodes[0].Type != "core.text" || res.Nodes[1].Type != "core.image" || res.Nodes[2].Type != "core.text" {
		t.Fatalf("拆分顺序错误：%s / %s / %s", res.Nodes[0].Type, res.Nodes[1].Type, res.Nodes[2].Type)
	}
}

func TestHTMLToNodesWarnsOnDegradation(t *testing.T) {
	src := `<div><p>壳里的段落</p></div><script>alert(1)</script><custom-tag>未知</custom-tag>`
	res, err := HTMLToNodes(src)
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}

	if len(res.Nodes) != 2 {
		t.Fatalf("剥壳后应剩 2 个段落，实际 %d", len(res.Nodes))
	}
	byAction := map[WarningAction][]string{}
	for _, w := range res.Warnings {
		byAction[w.Action] = append(byAction[w.Action], w.Tag)
	}
	if len(byAction[ActionUnwrap]) != 2 {
		t.Errorf("div 与未知标签都应记 unwrap，实际 %v", byAction[ActionUnwrap])
	}
	if len(byAction[ActionDrop]) != 1 || byAction[ActionDrop][0] != "script" {
		t.Errorf("script 应记 drop（内容真的没了），实际 %v", byAction[ActionDrop])
	}
}

func TestHTMLToNodesEmpty(t *testing.T) {
	for _, src := range []string{"", "   ", "<p></p>", "<div><span></span></div>"} {
		res, err := HTMLToNodes(src)
		if err != nil {
			t.Fatalf("%q 转换失败：%v", src, err)
		}
		if len(res.Nodes) != 0 {
			t.Errorf("%q 应产出零节点，实际 %d 个", src, len(res.Nodes))
		}
	}
}

func TestNodesToHTMLPlaceholderIsLossy(t *testing.T) {
	nodes := []*core.Node{
		{ID: "1", Type: "core.cardstack", Props: json.RawMessage(`{"collectionSource":"content:article"}`)},
	}
	res, err := NodesToHTML(nodes)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if res.Lossless {
		t.Error("含不可逆组件时必须标为有损")
	}
	if !strings.Contains(res.HTML, "core.cardstack") {
		t.Errorf("占位里应写明原组件类型：%q", res.HTML)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Action != ActionPlaceholder {
		t.Errorf("应有一条 placeholder 警告，实际 %v", res.Warnings)
	}
}

func TestImportedTreeCompiles(t *testing.T) {
	src := `<h2>编译验证标题</h2>
<p>段落正文 <strong>加粗</strong></p>
<ul><li>列表项</li></ul>
<ol><li>有序项</li></ol>
<blockquote>引用内容<cite>出处</cite></blockquote>
<img src="https://media.example.test/storage/image/x.webp" alt="图">
<figure><img src="https://media.example.test/storage/image/y.webp" alt="图二"><figcaption>图注</figcaption></figure>
<hr>
<table><caption>表</caption><thead><tr><th>H</th></tr></thead><tbody><tr><td>D</td></tr></tbody></table>`

	res, err := HTMLToNodes(src)
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("模板集加载失败：%v", err)
	}

	page := &builder.Page{
		Settings: builder.PageSettings{Layout: builder.PageLayout{Mode: "full"}},
		Root:     res.Nodes,
	}
	compiled, err := builder.Compile(page, builder.WithComponentSet(set), builder.WithProjectID("proj-test"))
	if err != nil {
		t.Fatalf("编译失败：%v", err)
	}
	t.Logf("编译产物：\n%s", compiled.HTML)

	for _, want := range []string{"编译验证标题", "段落正文", "列表项", "有序项", "引用内容", "图注", "表"} {
		if !strings.Contains(compiled.HTML, want) {
			t.Errorf("产物里缺少内容 %q", want)
		}
	}
	for _, want := range []string{"<h2", "<li", "<blockquote", "<img", "<table"} {
		if !strings.Contains(strings.ToLower(compiled.HTML), want) {
			t.Errorf("产物里缺少标签 %q", want)
		}
	}
}
