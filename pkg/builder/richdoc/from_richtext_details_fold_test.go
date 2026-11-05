package richdoc_test

import (
	"go_wp/pkg/builder/richdoc"
	"strings"
	"testing"

	"go_wp/pkg/builder"
	"go_wp/internal/templates"
)

func TestDetailsFoldSurvivesImportAndExport(t *testing.T) {
	src := `<p>前文</p>
<details onclick="alert(1)"><summary onmouseover="x()">折叠标题</summary><p>折叠正文</p><script>alert(1)</script></details>
<p>后文</p>`

	res, err := richdoc.HTMLToNodes(src)
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

	out, err := richdoc.NodesToHTML(res.Nodes)
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

	again, err := richdoc.HTMLToNodes(out.HTML)
	if err != nil {
		t.Fatalf("二次导入失败：%v", err)
	}
	out2, err := richdoc.NodesToHTML(again.Nodes)
	if err != nil {
		t.Fatalf("二次导出失败：%v", err)
	}
	if out2.HTML != out.HTML {
		t.Errorf("折叠块往返不收敛\n第一次 %q\n第二次 %q", out.HTML, out2.HTML)
	}

	empty, err := richdoc.HTMLToNodes("<details><summary></summary></details>")
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

	res, err := richdoc.HTMLToNodes(src)
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
