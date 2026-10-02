package builder

// site_scripts_test.go — 站点自定义 Head / Body 代码注入（PIPE-8）的字节级与安全边界断言。
//
// 与 seo_verification_test.go / ga4_test.go 同形：空值零字节、设值进对应位置、
// 非法值被拒（绝不原样进产物）、同一构建输入产出同一字节。
//
// 另外钉住本项**特有**的四条（它们才是 PIPE-8 的安全论证能被执行的部分）：
//  1. 注入点受控：head 片段只出现在 </head> 之前，body 片段只出现在 </head> 之后、
//     </body> 之前 —— 位置错一处就等于把脚本塞进了别的解析上下文；
//  2. 结构性标签被拒：那是「整页 HTML 误粘贴」的唯一可靠信号，放过它 = 产物出现第二套骨架；
//  3. 片段内部字节不被改写（只去两端空白）：改写管理员的脚本是另一种静默失效；
//  4. 组件级标签（<header> / <html5-embed>）不被误伤：判据是词边界，不是前缀。

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// 两个样本各自带一个 marker，用来判断片段落在了产物的哪一段。
const (
	siteScriptHeadSample = "<script async src=\"https://cdn.example.com/tag.js\"></script>\n" +
		"<script>window.demoTag=1;</script>"
	siteScriptHeadMarker = "window.demoTag=1"
	siteScriptBodySample = "<script src=\"https://cdn.example.com/chat.js\" async></script>"
	siteScriptBodyMarker = "cdn.example.com/chat.js"
)

// TestSiteScriptsEmptyAddsNoBytes 未配置片段时产物一个字节都不多（空值零字节注入）。
func TestSiteScriptsEmptyAddsNoBytes(t *testing.T) {
	if got := buildHeadScripts(""); got != "" {
		t.Fatalf("空 Head 片段应零字节注入，实际 %q", got)
	}
	if got := buildHeadScripts("   \n\t "); got != "" {
		t.Fatalf("纯空白 Head 片段应零字节注入，实际 %q", got)
	}
	if got := buildBodyScripts(""); got != "" {
		t.Fatalf("空 Body 片段应零字节注入，实际 %q", got)
	}
	if got := buildBodyScripts("  "); got != "" {
		t.Fatalf("纯空白 Body 片段应零字节注入，实际 %q", got)
	}
	doc, err := RenderDocument(&CompiledPage{HTML: "<p>x</p>", CSS: "p{color:red}"})
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	if strings.Contains(doc, "cdn.example.com") {
		t.Fatalf("未配置注入代码，产物却含第三方脚本:\n%s", doc)
	}
}

// TestSiteScriptsInjectionIntoDocument 注入点受控：head 片段进 </head> 之前，body 片段进 </body> 之前。
func TestSiteScriptsInjectionIntoDocument(t *testing.T) {
	page := &CompiledPage{
		HTML:        "<p>x</p>",
		HeadScripts: buildHeadScripts(siteScriptHeadSample),
		BodyScripts: buildBodyScripts(siteScriptBodySample),
	}
	doc, err := RenderDocument(page)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	headEnd := strings.Index(doc, "</head>")
	bodyEnd := strings.Index(doc, "</body>")
	if headEnd < 0 || bodyEnd < 0 || bodyEnd < headEnd {
		t.Fatalf("产物骨架异常（缺 </head> / </body> 或顺序颠倒）:\n%s", doc)
	}
	head := doc[:headEnd]
	tail := doc[headEnd:bodyEnd]
	if !strings.Contains(head, siteScriptHeadMarker) {
		t.Fatalf("Head 片段未落在 </head> 之前:\n%s", head)
	}
	if strings.Contains(tail, siteScriptHeadMarker) {
		t.Fatalf("Head 片段漏到了 head 之外（脚本会在错误的上下文里解析）:\n%s", tail)
	}
	if !strings.Contains(tail, siteScriptBodyMarker) {
		t.Fatalf("Body 片段未落在 </body> 之前:\n%s", tail)
	}
	// 各恰好一次：出现两次说明装配被调了两遍（同一段代码装两遍，第二遍的行为无法预期）。
	if n := strings.Count(doc, siteScriptHeadMarker); n != 1 {
		t.Errorf("Head 片段应恰好出现 1 次，实际 %d 次", n)
	}
	if n := strings.Count(doc, siteScriptBodyMarker); n != 1 {
		t.Errorf("Body 片段应恰好出现 1 次，实际 %d 次", n)
	}
}

// TestSiteScriptsShape 形状判据：合法通过；结构性标签与超长一律拒绝且零字节注入。
func TestSiteScriptsShape(t *testing.T) {
	valid := []string{
		siteScriptHeadSample,
		"<meta name=\"google-site-verification\" content=\"abc\">", // 第三方给的整段标签也是合法脚本片段
		"<style>.a{color:red}</style>",
		// 组件级标签不得被误伤：判据是词边界（head/body/html 后面的字符决定了它是不是骨架标签）。
		"<header id=\"h\">菜单</header>",
		"<html5-embed src=\"x\"></html5-embed>",
		"<!-- 统计代码：等待市场部确认 -->",
	}
	for _, in := range valid {
		// 判据走生产同一入口（Normalize*）：合法 = ok，且归一化结果必须非空 ——
		// 「ok 但结果是空串」在保存路径上会写成空片段，等于把作者的内容丢掉。
		head, headOK := NormalizeHeadScripts(in)
		body, bodyOK := NormalizeBodyScripts(in)
		if !headOK || !bodyOK {
			t.Errorf("应判为合法: %q", in)
		}
		if head == "" || body == "" {
			t.Errorf("合法输入归一化后不得为空串: %q", in)
		}
	}
	invalid := []string{
		"", "   ",
		// 结构性标签：这些输入一定是「整页 HTML 误粘贴」，放过它就是产物出现第二套骨架。
		"<!DOCTYPE html>",
		"<!doctype html><html lang=\"en\"><head></head><body><p>x</p></body></html>",
		"</html>",
		"<head><meta charset=\"utf-8\"></head>",
		"<BODY class=\"x\">",
		"< body>",
		"</ head >",
		// 超长：projects.settings 每次读取都整列解析，入口必须拒绝。
		strings.Repeat("a", maxSiteScriptBytes+1),
	}
	for _, in := range invalid {
		if _, ok := NormalizeHeadScripts(in); ok {
			t.Errorf("应判为非法（Head）: %q", in)
		}
		if _, ok := NormalizeBodyScripts(in); ok {
			t.Errorf("应判为非法（Body）: %q", in)
		}
		if got := buildHeadScripts(in); got != "" {
			t.Errorf("非法 Head 片段必须零字节注入，输入 %q 得到 %q", in, got)
		}
		if got := buildBodyScripts(in); got != "" {
			t.Errorf("非法 Body 片段必须零字节注入，输入 %q 得到 %q", in, got)
		}
	}
	// 边界：恰好等于上限是合法的（上限是「不超过」，不是「小于」）。
	atLimit := "<script>" + strings.Repeat("a", maxSiteScriptBytes-len("<script>")-len("</script>")) + "</script>"
	if len(atLimit) != maxSiteScriptBytes {
		t.Fatalf("样本长度构造错误: %d", len(atLimit))
	}
	if _, ok := NormalizeHeadScripts(atLimit); !ok {
		t.Errorf("长度恰好等于上限 %d 应判为合法", maxSiteScriptBytes)
	}
}

// TestSiteScriptsInternalBytesPreserved 归一化只去两端空白，片段内部一个字节都不改。
//
// 改写的危害是隐性的：作者下次在设置页保存一次，就会把「被改写的那份」写回库 ——
// 他装的脚本会以他没写过的方式运行，而页面上看不出任何差别。
func TestSiteScriptsInternalBytesPreserved(t *testing.T) {
	raw := "\n\t  <script>\n  var a = 1; // 中文注释\n</script>\n\n"
	want := "<script>\n  var a = 1; // 中文注释\n</script>"
	got, ok := NormalizeHeadScripts(raw)
	if !ok {
		t.Fatalf("应判为合法: %q", raw)
	}
	if got != want {
		t.Fatalf("归一化改动了片段字节:\n输入 %q\n得到 %q\n期望 %q", raw, got, want)
	}
	if got, ok = NormalizeBodyScripts(raw); !ok || got != want {
		t.Fatalf("Body 侧口径应与 Head 相同，得到 %q（ok=%v）", got, ok)
	}
}

// TestSiteScriptsOptionEmptyAddsNoBytes 空值经编译选项进入构建，产物与完全不注入**逐字节相同**。
//
// 这条比「产物里不含某段文本」更强：多一个空行也是字节差异，而空行会让
// 「同一输入 → 同一字节」之外的所有基线比对照样失真。
func TestSiteScriptsOptionEmptyAddsNoBytes(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	plain, err := Compile(page, WithComponentSet(set))
	if err != nil {
		t.Fatalf("Compile(未注入): %v", err)
	}
	blank, err := Compile(page, WithComponentSet(set), WithHeadScripts("   "), WithBodyScripts("\n\t"))
	if err != nil {
		t.Fatalf("Compile(空白片段): %v", err)
	}
	if plain.HeadScripts != "" || plain.BodyScripts != "" {
		t.Fatalf("未注入时应零字节，实际 HeadScripts=%q BodyScripts=%q", plain.HeadScripts, plain.BodyScripts)
	}
	if blank.HeadScripts != "" || blank.BodyScripts != "" {
		t.Fatalf("空白片段应零字节，实际 HeadScripts=%q BodyScripts=%q", blank.HeadScripts, blank.BodyScripts)
	}
	docPlain, err := RenderDocument(plain)
	if err != nil {
		t.Fatalf("RenderDocument(未注入): %v", err)
	}
	docBlank, err := RenderDocument(blank)
	if err != nil {
		t.Fatalf("RenderDocument(空白片段): %v", err)
	}
	if docPlain != docBlank {
		t.Fatalf("空白片段改变了产物字节（空值必须是零字节注入）:\n%q\n%q", docPlain, docBlank)
	}
}

// TestCompileSiteScriptsInvalidNotInjected 非法片段经编译选项进入构建，产物零字节且无残留。
func TestCompileSiteScriptsInvalidNotInjected(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	evil := "<html><body>EVIL-MARK</body></html>"
	got, err := Compile(page, WithComponentSet(set), WithHeadScripts(evil), WithBodyScripts(evil))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got.HeadScripts != "" || got.BodyScripts != "" {
		t.Fatalf("非法片段必须零字节注入，实际 HeadScripts=%q BodyScripts=%q", got.HeadScripts, got.BodyScripts)
	}
	doc, err := RenderDocument(got)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	if strings.Contains(doc, "EVIL-MARK") {
		t.Fatalf("非法输入泄漏进产物:\n%s", doc)
	}
	// 骨架标签仍然只有文档自身那一份（没有第二套 <html>）。
	if n := strings.Count(doc, "<html"); n != 1 {
		t.Fatalf("产物出现 %d 个 <html>（应为 1）:\n%s", n, doc)
	}
}

// TestCompileSiteScriptsDeterminism 同一输入重复编译，注入片段与产物逐字节一致。
func TestCompileSiteScriptsDeterminism(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts := []CompileOption{
		WithComponentSet(set),
		WithHeadScripts(siteScriptHeadSample),
		WithBodyScripts(siteScriptBodySample),
		WithLanguage("zh-CN"),
	}
	first, err := Compile(page, opts...)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(first.HeadScripts, siteScriptHeadMarker) || !strings.Contains(first.BodyScripts, siteScriptBodyMarker) {
		t.Fatalf("配置了合法片段，编译结果却没有注入: Head=%q Body=%q", first.HeadScripts, first.BodyScripts)
	}
	// 片段内部不带尾随换行（换行由 document.jet 提供）—— 这是「空值零字节」能成立的前提：
	// 片段自带换行时，模板的 {{if}} 之外还会多出一个字节。
	if strings.HasSuffix(first.HeadScripts, "\n") || strings.HasSuffix(first.BodyScripts, "\n") {
		t.Fatalf("注入片段不应自带尾随换行: Head=%q Body=%q", first.HeadScripts, first.BodyScripts)
	}
	for i := 0; i < 5; i++ {
		got, cerr := Compile(page, opts...)
		if cerr != nil {
			t.Fatalf("第 %d 次 Compile: %v", i, cerr)
		}
		if got.HeadScripts != first.HeadScripts || got.BodyScripts != first.BodyScripts {
			t.Fatalf("第 %d 次编译注入片段字节不一致:\n%q\n%q", i, first.HeadScripts, got.HeadScripts)
		}
	}
}

// TestSiteScriptsEndToEndRender 走完整链路：Compile（注入）→ RenderDocument，两段都落在正确位置。
func TestSiteScriptsEndToEndRender(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	compiled, err := Compile(page,
		WithComponentSet(set),
		WithHeadScripts(siteScriptHeadSample),
		WithBodyScripts(siteScriptBodySample),
		WithLanguage("zh-CN"),
	)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	headEnd := strings.Index(doc, "</head>")
	bodyEnd := strings.Index(doc, "</body>")
	if headEnd < 0 || bodyEnd < 0 {
		t.Fatalf("产物骨架不完整:\n%s", doc)
	}
	if !strings.Contains(doc[:headEnd], siteScriptHeadMarker) {
		t.Fatalf("产物 head 缺少自定义代码:\n%s", doc[:headEnd])
	}
	if !strings.Contains(doc[headEnd:bodyEnd], siteScriptBodyMarker) {
		t.Fatalf("产物 body 缺少自定义代码:\n%s", doc[headEnd:bodyEnd])
	}
	// 整份文档仍需闭合（注入不能破坏骨架）。模板尾部带一个换行，故按 TrimSpace 判定。
	if !strings.HasSuffix(strings.TrimSpace(doc), "</html>") {
		t.Fatalf("产物未以 </html> 收尾（注入破坏了骨架）:\n%s", doc)
	}
	// 空值：同一页面不注入时产物零字节（对比同一构建路径）。
	compiled, err = Compile(page, WithComponentSet(set), WithLanguage("zh-CN"))
	if err != nil {
		t.Fatalf("Compile(空): %v", err)
	}
	doc, err = RenderDocument(compiled)
	if err != nil {
		t.Fatalf("RenderDocument(空): %v", err)
	}
	if strings.Contains(doc, "cdn.example.com") {
		t.Fatalf("未配置片段时产物不得含第三方脚本:\n%s", doc)
	}
}
