package builder

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

const (
	siteScriptHeadSample = "<script async src=\"https://cdn.example.com/tag.js\"></script>\n" +
		"<script>window.demoTag=1;</script>"
	siteScriptHeadMarker = "window.demoTag=1"
	siteScriptBodySample = "<script src=\"https://cdn.example.com/chat.js\" async></script>"
	siteScriptBodyMarker = "cdn.example.com/chat.js"
)

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

	if n := strings.Count(doc, siteScriptHeadMarker); n != 1 {
		t.Errorf("Head 片段应恰好出现 1 次，实际 %d 次", n)
	}
	if n := strings.Count(doc, siteScriptBodyMarker); n != 1 {
		t.Errorf("Body 片段应恰好出现 1 次，实际 %d 次", n)
	}
}

func TestSiteScriptsShape(t *testing.T) {
	valid := []string{
		siteScriptHeadSample,
		"<meta name=\"google-site-verification\" content=\"abc\">",
		"<style>.a{color:red}</style>",

		"<header id=\"h\">菜单</header>",
		"<html5-embed src=\"x\"></html5-embed>",
		"<!-- 统计代码：等待市场部确认 -->",
	}
	for _, in := range valid {

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

		"<!DOCTYPE html>",
		"<!doctype html><html lang=\"en\"><head></head><body><p>x</p></body></html>",
		"</html>",
		"<head><meta charset=\"utf-8\"></head>",
		"<BODY class=\"x\">",
		"< body>",
		"</ head >",

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

	atLimit := "<script>" + strings.Repeat("a", maxSiteScriptBytes-len("<script>")-len("</script>")) + "</script>"
	if len(atLimit) != maxSiteScriptBytes {
		t.Fatalf("样本长度构造错误: %d", len(atLimit))
	}
	if _, ok := NormalizeHeadScripts(atLimit); !ok {
		t.Errorf("长度恰好等于上限 %d 应判为合法", maxSiteScriptBytes)
	}
}

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

func TestSiteScriptsOptionEmptyAddsNoBytes(t *testing.T) {
	set, err := templates.NewComponentSet("../../internal/templates/components")
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

func TestCompileSiteScriptsInvalidNotInjected(t *testing.T) {
	set, err := templates.NewComponentSet("../../internal/templates/components")
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

	if n := strings.Count(doc, "<html"); n != 1 {
		t.Fatalf("产物出现 %d 个 <html>（应为 1）:\n%s", n, doc)
	}
}

func TestCompileSiteScriptsDeterminism(t *testing.T) {
	set, err := templates.NewComponentSet("../../internal/templates/components")
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

func TestSiteScriptsEndToEndRender(t *testing.T) {
	set, err := templates.NewComponentSet("../../internal/templates/components")
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

	if !strings.HasSuffix(strings.TrimSpace(doc), "</html>") {
		t.Fatalf("产物未以 </html> 收尾（注入破坏了骨架）:\n%s", doc)
	}

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
