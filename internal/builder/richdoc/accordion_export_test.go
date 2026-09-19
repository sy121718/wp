package richdoc

// accordion_export_test.go — 手风琴（core.accordion）的反向导出背书。
//
// 背景：导入方向把每个 <details> 整块落进 core.text 的富文本字段（见 details_fold_test.go），
// 但导出方向一度把 core.accordion 当成不可逆组件 —— 画布上建的折叠块导出成占位文字，
// 内容直接没了。这个文件钉四件事：
//
//  1. 结构对应时：items ↔ children 一一对应导成 <details><summary>…</summary>正文…</details>，
//     且导出结果能被再导入回来（往返收敛、内容不丢，含嵌套子组件的渲染结果）；
//  2. 结构对不上时（数量不等）：诚实地失败（占位 + Lossless=false），不把内容挂错标题；
//  3. 缺摘要：正文照常导出，但如实记损（导回画布时这一项会消失）；
//  4. 组件状态（默认展开 / 同时只开一个）在富文本里没有载体：内容完整但记损，
//     绝不把 Lossless 硬翻成 true。
//
// 最后一段把往返结果交给真实编译器跑一遍：只测转换不测编译，会漏掉"字段名/组件类型漂移"
// 这类自造 props 与组件定义分叉的问题。

import (
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/builder"
	accordionpkg "go_wp/internal/builder/components/accordion"
	textpkg "go_wp/internal/builder/components/text"
	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// mustNode 造一个组件节点（测试专用：props 直接给结构体，children 显式传入）。
func mustNode(t *testing.T, typ string, props any, children ...*core.Node) *core.Node {
	t.Helper()
	raw, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("props 序列化失败：%v", err)
	}
	return &core.Node{ID: newID(), Type: typ, Props: raw, Children: children}
}

// canvasAccordion 画布上的手风琴：两个折叠项，第二项正文里带一张有图注的图
// （图在富文本字段里 —— 正文的块级结构本来就是这个字段的一部分）。
func canvasAccordion(t *testing.T) *core.Node {
	t.Helper()
	first := mustNode(t, textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: "<p>第一项正文</p>"})
	second := mustNode(t, textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText,
		Text: `<p>第二项正文 <strong>加粗</strong></p><figure><img src="/img/logo.webp"><figcaption>第二项的插图</figcaption></figure>`})
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
		// 图注在 <figcaption>，而导出前过一次白名单清洗 —— alt 回填同时在这里生效。
		`<figure><img src="/img/logo.webp" alt="第二项的插图"><figcaption>第二项的插图</figcaption></figure>`,
		"</details>",
	} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("导出结果缺 %q\n%s", want, out.HTML)
		}
	}
	if n := strings.Count(out.HTML, "<details>"); n != 2 {
		t.Errorf("两个折叠项应导出两个 <details>，实际 %d\n%s", n, out.HTML)
	}

	// 往返：导出的 HTML 再导入 → 内容必须还在（折叠块落在 core.text 的富文本字段里）。
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
	// 导出的结果里不能再出现占位文字（那是导出方向的降级标记）。
	if strings.Contains(out.HTML, "［组件：") {
		t.Errorf("结构对应时不该出现占位文字：%s", out.HTML)
	}

	// 收敛：再导出一次，字节必须一致。
	out2, err := NodesToHTML(back.Nodes)
	if err != nil {
		t.Fatalf("二次导出失败：%v", err)
	}
	if out2.HTML != out.HTML {
		t.Errorf("手风琴往返不收敛\n第一次 %q\n第二次 %q", out.HTML, out2.HTML)
	}
}

// TestAccordionExportCompilesToArtifact 往返结果交给真实编译器：折叠块必须真的渲染出来。
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
	for _, want := range []string{"折叠项一", "第一项正文", "折叠项二", "第二项正文", "第二项的插图"} {
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

// TestAccordionExportNestsContainerChild 折叠项的内容本身是容器组件（嵌套手风琴）：
// 正文取的是该 children 的**渲染结果**，所以嵌套结构也要原样出现在 <details> 正文里。
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
	// 嵌套结构可再导入（details 分支整块接管）。
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

// TestAccordionExportStructureMismatchIsHonest 数量不等：占位 + Lossless=false，内容不串项。
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
			// 占位是给人看的，不能把内容挂到别人的标题下伪装成功。
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

// TestAccordionExportEmptyTitleIsLossy 缺摘要：正文照常导出，但如实记损。
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

// TestAccordionExportStateIsLossy 组件状态（默认展开 / 同时只开一个）没有富文本载体：
// 内容完整导出，但必须记损。
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
	// open 属性本来就在白名单外（core/richtext.go 刻意剥掉），导出里不该出现。
	if strings.Contains(out.HTML, " open") {
		t.Errorf("导出结果不该带 open 属性：%s", out.HTML)
	}
}

// hasWarning 是否含指定标签 + 动作的警告。
func hasWarning(warnings []Warning, tag string, action WarningAction) bool {
	for _, w := range warnings {
		if w.Tag == tag && w.Action == action {
			return true
		}
	}
	return false
}
