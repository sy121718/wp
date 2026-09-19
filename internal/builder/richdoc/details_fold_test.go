package richdoc

// details_fold_test.go — 折叠块（details / summary）在 richdoc 这条链路上的保真背书。
//
// 背景：rich-editor 扩展把「折叠块」做成 Trix 的原子附件，提交时展开为
// <details><summary>标题</summary>正文</details>，服务端白名单（core/richtext.go）认识这两个标签。
// 但导入器一度把 details 当通用结构壳（structuralTags）剥掉 —— 用户在编辑器里写的折叠块
// 导入画布后会变成一串裸段落，既不报错也没有警告意义上的「结构丢失」可查。
//
// 这个文件钉三件事：
//  1. 导入：折叠块整块落进一个 core.text 的富文本字段（结构完整，不是被拆散）；
//  2. 清洗：script 剥壳、on* 事件属性全部剔除 —— 折叠块不能成为 XSS 的载体；
//  3. 渲染：把导入结果交给真实编译器，产物里 <details>/<summary> 与正文原样在（含行内格式）。
//     只测转换不测编译会漏掉「字段名/组件类型漂移」，自造的 props 与组件定义分叉时照样全绿。

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/templates"
)

func TestDetailsFoldSurvivesImportAndExport(t *testing.T) {
	src := `<p>前文</p>
<details onclick="alert(1)"><summary onmouseover="x()">折叠标题</summary><p>折叠正文</p><script>alert(1)</script></details>
<p>后文</p>`

	res, err := HTMLToNodes(src)
	if err != nil {
		t.Fatalf("转换失败：%v", err)
	}
	gotTypes := make([]string, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		gotTypes = append(gotTypes, n.Type)
	}
	if strings.Join(gotTypes, ",") != "core.text,core.text,core.text" {
		t.Fatalf("折叠块应整体落进一个 core.text（与前后段落并列），实际 %v", gotTypes)
	}

	fold, _ := propsOf(t, res.Nodes[1])["text"].(string)
	for _, want := range []string{
		"<details>", "</details>",
		"<summary>折叠标题</summary>",
		"<p>折叠正文</p>",
	} {
		if !strings.Contains(fold, want) {
			t.Errorf("折叠结构缺 %q：%q", want, fold)
		}
	}
	for _, bad := range []string{"<script", "<style", "<iframe", "onclick=", "onmouseover="} {
		if strings.Contains(fold, bad) {
			t.Errorf("折叠块里残留危险内容 %q：%q", bad, fold)
		}
	}

	// 导出方向同样是一次清洗 + 序列化：结构仍在，危险内容仍不出现。
	out, err := NodesToHTML(res.Nodes)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if !out.Lossless {
		t.Errorf("折叠块属可逆子集，导出不该有损：%v", out.Warnings)
	}
	for _, want := range []string{"<details>", "<summary>折叠标题</summary>", "<p>折叠正文</p>"} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("导出结果缺 %q：%q", want, out.HTML)
		}
	}
	for _, bad := range []string{"<script", "onclick=", "onmouseover="} {
		if strings.Contains(out.HTML, bad) {
			t.Errorf("导出结果残留危险内容 %q：%q", bad, out.HTML)
		}
	}

	// 往返收敛：导出结果再导入 + 再导出，字节必须一致（否则真实数据会逐次漂移）。
	again, err := HTMLToNodes(out.HTML)
	if err != nil {
		t.Fatalf("二次导入失败：%v", err)
	}
	out2, err := NodesToHTML(again.Nodes)
	if err != nil {
		t.Fatalf("二次导出失败：%v", err)
	}
	if out2.HTML != out.HTML {
		t.Errorf("折叠块往返不收敛\n第一次 %q\n第二次 %q", out.HTML, out2.HTML)
	}

	// 空折叠块不产生空节点（否则画布上会多一个看不见的节点）。
	empty, err := HTMLToNodes("<details><summary></summary></details>")
	if err != nil {
		t.Fatalf("空折叠块转换失败：%v", err)
	}
	if len(empty.Nodes) != 0 {
		t.Errorf("空折叠块应被忽略，实际产出 %d 个节点", len(empty.Nodes))
	}
}

func TestDetailsFoldRendersInCompiledArtifact(t *testing.T) {
	src := `<details><summary>折叠标题</summary><p>折叠正文 <strong>加粗</strong></p></details>
<details onclick="alert(1)"><summary onmouseover="x()">第二块</summary><p>带<script>alert(1)</script>正文</p></details>`

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

	html := compiled.HTML
	for _, want := range []string{
		"<details", "<summary", "折叠标题", "折叠正文", "<strong>加粗</strong>", "第二块",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("产物缺少 %q\n%s", want, html)
		}
	}
	// 每个折叠块都各自闭合（结构完整，而不是被压平成一段文字）。
	if n := strings.Count(html, "<details"); n != 2 {
		t.Errorf("产物里应有 2 个折叠块，实际 %d 个\n%s", n, html)
	}
	if n := strings.Count(html, "</details>"); n != 2 {
		t.Errorf("产物里应有 2 个折叠块闭合标签，实际 %d 个\n%s", n, html)
	}
	for _, bad := range []string{"<script", "<style", "<iframe", "onclick=", "onmouseover=", "data-trix-attachment"} {
		if strings.Contains(strings.ToLower(html), bad) {
			t.Errorf("产物残留危险内容 %q\n%s", bad, html)
		}
	}
}
