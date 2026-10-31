package richdoc

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder"
	accordionpkg "go_wp/internal/builder/components/accordion"
	textpkg "go_wp/internal/builder/components/text"
	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

func mustNode(t *testing.T, typ string, props any, children ...*core.Node) *core.Node {
	t.Helper()
	raw, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("props 序列化失败：%v", err)
	}
	return &core.Node{ID: newID(), Type: typ, Props: raw, Children: children}
}

func canvasAccordion(t *testing.T) *core.Node {
	t.Helper()
	first := mustNode(t, textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: "<p>第一项正文</p>"})
	second := mustNode(t, textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText,
		Text: `<p>第二项正文 <strong>加粗</strong></p><figure><img src="https://assets.example.test/img/logo.webp"><figcaption>第二项的插图</figcaption></figure>`})
	return mustNode(t, accordionpkg.Type, accordionpkg.Props{Items: []accordionpkg.Item{
		{Title: "折叠项一"},
		{Title: "折叠项二"},
	}}, first, second)
}

func TestAccordionExportsToDetails(t *testing.T) {
	acc := canvasAccordion(t)
	out, err := NodesToHTML([]*core.Node{acc})
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if !out.Lossless {
		t.Errorf("结构对应（数量相等、摘要非空）且没有组件状态时导出应为无损，实际警告：%v", out.Warnings)
	}
	for _, want := range []string{
		"<details><summary>折叠项一</summary><p>第一项正文</p></details>",
		"<details><summary>折叠项二</summary><p>第二项正文 <strong>加粗</strong></p>",

		`<figure><img src="https://assets.example.test/img/logo.webp" alt="第二项的插图"><figcaption>第二项的插图</figcaption></figure>`,
		"</details>",
	} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("导出结果缺 %q\n%s", want, out.HTML)
		}
	}
	if n := strings.Count(out.HTML, "<details>"); n != 2 {
		t.Errorf("两个折叠项应导出两个 <details>，实际 %d\n%s", n, out.HTML)
	}

	back, err := HTMLToNodes(out.HTML)
	if err != nil {
		t.Fatalf("二次导入失败：%v", err)
	}
	if len(back.Nodes) != 2 {
		t.Fatalf("两个折叠块应落成两个 core.text，实际 %d 个：%s", len(back.Nodes), out.HTML)
	}
	for _, want := range []string{"折叠项一", "第一项正文", "折叠项二", "第二项正文", "第二项的插图"} {
		found := false
		for _, n := range back.Nodes {
			textValue, _ := propsOf(t, n)["text"].(string)
			if strings.Contains(textValue, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("往返后内容丢了 %q\n%s", want, out.HTML)
		}
	}

	if strings.Contains(out.HTML, "［组件：") {
		t.Errorf("结构对应时不该出现占位文字：%s", out.HTML)
	}

	out2, err := NodesToHTML(back.Nodes)
	if err != nil {
		t.Fatalf("二次导出失败：%v", err)
	}
	if out2.HTML != out.HTML {
		t.Errorf("手风琴往返不收敛\n第一次 %q\n第二次 %q", out.HTML, out2.HTML)
	}
}

func TestAccordionExportCompilesToArtifact(t *testing.T) {
	out, err := NodesToHTML([]*core.Node{canvasAccordion(t)})
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	back, err := HTMLToNodes(out.HTML)
	if err != nil {
		t.Fatalf("二次导入失败：%v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("模板集加载失败：%v", err)
	}
	page := &builder.Page{
		Settings: builder.PageSettings{Layout: builder.PageLayout{Mode: "full"}},
		Root:     back.Nodes,
	}
	compiled, err := builder.Compile(page, builder.WithComponentSet(set), builder.WithProjectID("proj-test"))
	if err != nil {
		t.Fatalf("编译失败：%v", err)
	}
	for _, want := range []string{"折叠项一", "第一项正文", "折叠项二", "第二项正文", "第二项的插图", `src="https://assets.example.test/img/logo.webp"`} {
		if !strings.Contains(compiled.HTML, want) {
			t.Errorf("产物缺少 %q\n%s", want, compiled.HTML)
		}
	}
	if n := strings.Count(compiled.HTML, "<details"); n != 2 {
		t.Errorf("产物里应有 2 个折叠块，实际 %d\n%s", n, compiled.HTML)
	}
	if strings.Contains(compiled.HTML, "［组件：") {
		t.Errorf("产物里出现了占位文字：%s", compiled.HTML)
	}
}

func TestAccordionExportNestsContainerChild(t *testing.T) {
	inner := mustNode(t, textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: "<p>里层正文</p>"})
	nested := mustNode(t, accordionpkg.Type, accordionpkg.Props{Items: []accordionpkg.Item{{Title: "里层标题"}}}, inner)
	outer := mustNode(t, accordionpkg.Type, accordionpkg.Props{Items: []accordionpkg.Item{{Title: "外层标题"}}}, nested)

	out, err := NodesToHTML([]*core.Node{outer})
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	want := "<details><summary>外层标题</summary><details><summary>里层标题</summary><p>里层正文</p></details></details>"
	if !strings.Contains(out.HTML, want) {
		t.Errorf("嵌套折叠结构不符\n期望含 %q\n实际 %q", want, out.HTML)
	}

	back, err := HTMLToNodes(out.HTML)
	if err != nil {
		t.Fatalf("二次导入失败：%v", err)
	}
	if len(back.Nodes) != 1 {
		t.Fatalf("嵌套折叠块应落成一个 core.text，实际 %d 个", len(back.Nodes))
	}
	textValue, _ := propsOf(t, back.Nodes[0])["text"].(string)
	for _, needle := range []string{"外层标题", "里层标题", "里层正文"} {
		if !strings.Contains(textValue, needle) {
			t.Errorf("往返后丢了 %q：%q", needle, textValue)
		}
	}
}

func TestAccordionExportStructureMismatchIsHonest(t *testing.T) {
	child := mustNode(t, textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: "<p>只有一个内容块</p>"})
	child2 := mustNode(t, textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: "<p>第二个内容块</p>"})
	cases := map[string]*core.Node{
		"标题比内容多": mustNode(t, accordionpkg.Type, accordionpkg.Props{Items: []accordionpkg.Item{
			{Title: "一"}, {Title: "二"},
		}}, child),
		"内容比标题多": mustNode(t, accordionpkg.Type, accordionpkg.Props{Items: []accordionpkg.Item{
			{Title: "一"},
		}}, child, child2),
		"全空": mustNode(t, accordionpkg.Type, accordionpkg.Props{}),
	}
	for name, node := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := NodesToHTML([]*core.Node{node})
			if err != nil {
				t.Fatalf("导出失败：%v", err)
			}
			if out.Lossless {
				t.Errorf("结构对不上时必须记有损，实际 Lossless=true，警告：%v", out.Warnings)
			}
			if !strings.Contains(out.HTML, "［组件："+accordionpkg.Type+"｜") {
				t.Errorf("结构对不上时应导出占位文字：%s", out.HTML)
			}

			for _, leaked := range []string{"只有一个内容块", "第二个内容块"} {
				if strings.Contains(out.HTML, leaked) {
					t.Errorf("结构对不上时不该把内容块拼进标题下（泄漏 %q）：%s", leaked, out.HTML)
				}
			}
			if !hasWarning(out.Warnings, accordionpkg.Type, ActionPlaceholder) {
				t.Errorf("缺少 placeholder 警告：%v", out.Warnings)
			}
		})
	}
}

func TestAccordionExportEmptyTitleIsLossy(t *testing.T) {
	child := mustNode(t, textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: "<p>没有标题的正文</p>"})
	node := mustNode(t, accordionpkg.Type, accordionpkg.Props{Items: []accordionpkg.Item{{Title: "  "}}}, child)

	out, err := NodesToHTML([]*core.Node{node})
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if out.Lossless {
		t.Errorf("缺摘要必须记有损，实际 Lossless=true")
	}
	if !strings.Contains(out.HTML, "<details><summary></summary><p>没有标题的正文</p></details>") {
		t.Errorf("缺摘要时正文仍要导出：%s", out.HTML)
	}
	if !hasWarning(out.Warnings, accordionpkg.Type, ActionTrim) {
		t.Errorf("缺少 trim 警告：%v", out.Warnings)
	}
}

func TestAccordionExportStateIsLossy(t *testing.T) {
	child := mustNode(t, textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: "<p>正文</p>"})
	node := mustNode(t, accordionpkg.Type, accordionpkg.Props{
		Items:   []accordionpkg.Item{{Title: "一", Open: true}},
		OneOpen: true,
	}, child)

	out, err := NodesToHTML([]*core.Node{node})
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if out.Lossless {
		t.Errorf("组件状态无法在富文本里表达时必须记有损，实际 Lossless=true")
	}
	if !strings.Contains(out.HTML, "<details><summary>一</summary><p>正文</p></details>") {
		t.Errorf("内容仍要完整导出：%s", out.HTML)
	}

	if strings.Contains(out.HTML, " open") {
		t.Errorf("导出结果不该带 open 属性：%s", out.HTML)
	}
}

func hasWarning(warnings []Warning, tag string, action WarningAction) bool {
	for _, w := range warnings {
		if w.Tag == tag && w.Action == action {
			return true
		}
	}
	return false
}
