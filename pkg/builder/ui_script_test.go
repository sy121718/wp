package builder

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

func uiSrcForTest() map[string]string {
	return map[string]string{
		"_util.js":        "/* util */ window.WBUI=window.WBUI||{};",
		"site-catalog.js": "/* site-catalog */",
		"site-preview.js": "/* site-preview */",
		"site-actions.js": "/* site-actions */",
		"site-data.js":    "/* site-data */",
		"site-product.js": "/* site-product */",
		"requires_js.js":  "/* requires_js */",
		"write-base.js":   "/* write-base */",
		"htmx-bridge.js":  "/* htmx-bridge */",
		"htmx.min.js":     "/* htmx */ var htmx=function(){};",
		"select.js":       "/* select */ WBUI.register(function(){});",
		"modal.js":        "/* modal */ WBUI.register(function(){});",
		"index.js":        "/* index */ WBUI.scan(document);",
	}
}

func TestUIScriptSkippedWithoutFeature(t *testing.T) {
	got := uiScriptForTest(t, `<section><h1>纯内容</h1></section>`, uiSrcForTest())
	if got != "" {
		t.Errorf("没有 data-ui-* 特征时不该注入控件，got %q", got)
	}
}

func TestUIScriptInjectsSelectWithBase(t *testing.T) {
	got := uiScriptForTest(t, `<select data-ui-select name="city"></select>`, uiSrcForTest())
	for _, want := range []string{"/* util */", "/* select */", "/* index */"} {
		if !strings.Contains(got, want) {
			t.Errorf("拼装结果缺少 %q\n%s", want, got)
		}
	}

	if strings.Contains(got, "<script") || strings.Contains(got, "</script>") {
		t.Errorf("拼装结果不应包含 script 标签（外层模板已提供）:\n%s", got)
	}

	if strings.Index(got, "/* util */") > strings.Index(got, "/* select */") {
		t.Errorf("基座助手应在控件之前")
	}
	if strings.Index(got, "/* index */") < strings.Index(got, "/* select */") {
		t.Errorf("入口应在控件之后")
	}
}

func TestUIScriptInjectsModalOnTrigger(t *testing.T) {
	for _, html := range []string{
		`<button data-modal-open="f1">打开</button>`,
		`<dialog id="f1" data-modal><button type="button" data-modal-close>×</button></dialog>`,
	} {
		got := uiScriptForTest(t, html, uiSrcForTest())
		if !strings.Contains(got, "/* modal */") {
			t.Errorf("命中弹窗特征时应注入 modal.js；输入 %s；结果 %s", html, got)
		}
	}
}

func TestUIStyleFromClassOnly(t *testing.T) {
	css, script, err := uiAssetsForScan(collectHTMLScan(`<button class="btn btn-primary">提交</button>`), "/* css */", uiSrcForTest())
	if err != nil {
		t.Fatal(err)
	}
	if css != "/* css */" {
		t.Errorf("基座 class 应注入 ui.css，got css=%q", css)
	}
	if script != "" {
		t.Errorf("仅 class 不应注入控件脚本，got %q", script)
	}
}

func TestUIStyleClassWithoutSources(t *testing.T) {
	css, script, err := uiAssetsForScan(collectHTMLScan(`<input class="form-input" />`), "/* css */", nil)
	if err != nil || css != "/* css */" || script != "" {
		t.Fatalf("css=%q script=%q err=%v", css, script, err)
	}
}

func TestUIStyleFollowsControls(t *testing.T) {
	for _, tt := range []struct{ html, want string }{
		{`<button data-modal-open="f1">打开</button>`, "/* css */"},
		{`<h1>纯内容</h1>`, ""},
	} {
		css, _, err := uiAssetsFor(collectHTMLFeatures(tt.html), "/* css */", uiSrcForTest())
		if err != nil || css != tt.want {
			t.Fatalf("样式 = %q, err = %v", css, err)
		}
	}
}

func TestUIScriptIgnoresSelfReference(t *testing.T) {
	html := `<h1>纯内容</h1><script>var s = "data-ui-select";</script>`
	if got := uiScriptForTest(t, html, uiSrcForTest()); got != "" {
		t.Errorf("script 块内的字样不应触发注入，got %q", got)
	}
}

func uiScriptForTest(t *testing.T, content string, sources map[string]string) string {
	t.Helper()
	_, script, err := uiAssetsFor(collectHTMLFeatures(content), "/* css */", sources)
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func TestUIAssetRegistryAndSelection(t *testing.T) {
	want := []string{"_util.js", "htmx.min.js", "site-preview.js", "site-actions.js", "site-data.js", "site-catalog.js", "site-product.js", "write-base.js", "htmx-bridge.js", "requires_js.js", "select.js", "modal.js", "drawer.js", "confirm.js", "colorfield.js", "iconfield.js", "themetoggle.js", "index.js"}
	got := UIAssetFiles()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("资源清单 = %v", got)
	}
	got[0] = "mutated"
	if !reflect.DeepEqual(UIAssetFiles(), want) {
		t.Fatal("调用方改变了资源注册表")
	}
	_, script, err := uiAssetsFor(collectHTMLFeatures(`<dialog data-modal></dialog><SELECT DATA-UI-SELECT></SELECT><select data-ui-select></select>`), "/* css */", uiSrcForTest())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(script, "/* select */") != 1 || strings.Count(script, "/* modal */") != 1 || strings.Index(script, "/* select */") > strings.Index(script, "/* modal */") {
		t.Fatal("控件必须去重且按注册顺序输出")
	}

	sources := uiSrcForTest()
	delete(sources, "modal.js")
	_, script, err = uiAssetsFor(collectHTMLFeatures(`<select data-ui-select></select>`), "/* css */", sources)
	if err != nil || strings.Contains(script, "/* modal */") {
		t.Fatalf("不应要求无关控件：%v", err)
	}
}

func TestUIAssetsExplicitNoScriptMode(t *testing.T) {
	css, script, err := uiAssetsFor(collectHTMLFeatures(`<select data-ui-select></select>`), "/* css */", nil)
	if err != nil || script != "" || css != "/* css */" {
		t.Fatalf("无脚本模式失效：css=%q js=%q err=%v", css, script, err)
	}
	_, _, err = uiAssetsFor(collectHTMLFeatures(`<select data-ui-select></select>`), "/* css */", map[string]string{})
	if err == nil {
		t.Fatal("已启用增强却缺全部资源时必须报错")
	}
	css, script, err = uiAssetsFor(collectHTMLFeatures(`<p>内容</p>`), "", map[string]string{})
	if err != nil || css != "" || script != "" {
		t.Fatal("纯内容页不应依赖控件资源")
	}
}

func TestHtmxInjectedOnlyWhenUsed(t *testing.T) {
	for _, html := range []string{
		`<div hx-get="/_fragments/ordersList" hx-trigger="load"></div>`,
		`<button HX-POST="/_fragments/returnRequest">加购</button>`,
		`<form hx-swap="outerHTML"><input name="q"></form>`,
	} {
		if got := uiScriptForTest(t, html, uiSrcForTest()); !strings.Contains(got, "/* htmx */") {
			t.Errorf("命中 hx-* 属性时必须注入 htmx；输入 %s；结果 %q", html, got)
		}
	}
	for _, html := range []string{
		`<section><h1>纯内容</h1></section>`,
		`<div data-hx-get="/x"></div>`,
		`<script>var s = 'hx-get';</script>`,
		`<p title="hx-get">示例</p>`,
	} {
		if got := uiScriptForTest(t, html, uiSrcForTest()); strings.Contains(got, "/* htmx */") {
			t.Errorf("非 hx-* 属性不应触发 htmx 注入；输入 %s；结果 %q", html, got)
		}
	}
}

func TestHtmxDoesNotDragControlBase(t *testing.T) {
	got := uiScriptForTest(t, `<div hx-get="/x" hx-trigger="load"></div>`, uiSrcForTest())
	for _, unwanted := range []string{"/* util */", "/* index */", "/* select */"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("纯 htmx 页面不该注入 %q\n%s", unwanted, got)
		}
	}

	mixed := uiScriptForTest(t, `<div hx-get="/x" hx-trigger="load"></div><select data-ui-select></select>`, uiSrcForTest())
	for _, want := range []string{"/* util */", "/* htmx */", "/* select */", "/* index */"} {
		if !strings.Contains(mixed, want) {
			t.Fatalf("混用页面缺少 %q\n%s", want, mixed)
		}
	}
	if strings.Index(mixed, "/* htmx */") > strings.Index(mixed, "/* index */") {
		t.Error("htmx 应在控件扫描入口之前就位")
	}
}

func TestHtmxOnlyPageNeedsNoUIStyle(t *testing.T) {
	css, script, err := uiAssetsFor(collectHTMLFeatures(`<div hx-get="/x"></div>`), "", uiSrcForTest())
	if err != nil {
		t.Fatalf("htmx 页面不该因缺少控件样式而失败：%v", err)
	}
	if css != "" || !strings.Contains(script, "/* htmx */") {
		t.Fatalf("css=%q script=%q", css, script)
	}

	if _, _, err := uiAssetsFor(collectHTMLFeatures(`<select data-ui-select></select>`), "", uiSrcForTest()); err == nil {
		t.Fatal("控件页面缺 ui.css 必须报错")
	}
}

func TestRenderDocumentRejectsIncompleteUIAssets(t *testing.T) {
	for _, missing := range []string{"_util.js", "select.js", "modal.js", "index.js", "ui.css"} {
		t.Run(missing, func(t *testing.T) {
			sources, css := uiSrcForTest(), "/* ui css */"
			if missing == "ui.css" {
				css = ""
			} else {
				delete(sources, missing)
			}
			out, err := RenderDocument(&CompiledPage{HTML: `<select data-ui-select></select><dialog data-modal></dialog>`, UISources: sources, UIStyle: css})
			if err == nil || !strings.Contains(err.Error(), missing) || out != "" {
				t.Fatalf("缺失 %s 应阻断文档产出并给出资源名，got html=%q err=%v", missing, out, err)
			}
		})
	}
}

func TestUIAssetsRejectBlankSource(t *testing.T) {
	sources := uiSrcForTest()
	sources["select.js"] = " \n\t"
	_, _, err := uiAssetsFor(collectHTMLFeatures(`<select data-ui-select></select>`), "/* css */", sources)
	if err == nil || !strings.Contains(err.Error(), "select.js") {
		t.Fatalf("空白源码必须视作缺失：%v", err)
	}
}

func TestRenderDocumentUIUsesAttributesOnly(t *testing.T) {
	for _, content := range []string{
		`<p>data-ui-select / data-modal 示例</p>`,
		`<!-- <select data-ui-select></select> -->`,
		`<script>var sample = '<dialog data-modal></dialog>';</script>`,
		`<div title="data-modal" data-modal-example=""></div>`,
		`<style>.example::before { content: 'data-ui-select' }</style>`,
	} {
		out, err := RenderDocument(&CompiledPage{HTML: content, UISources: uiSrcForTest(), UIStyle: "/* ui css */"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "/* util */") || strings.Contains(out, "/* ui css */") {
			t.Errorf("正文、脚本、样式、注释或属性值不应触发控件注入：%s", content)
		}
	}
}

func TestWriteAssetsRequireHTMLBaseAndHtmxInOrder(t *testing.T) {
	content := `<form data-write hx-post="/products/create" hx-target="#result" hx-swap="innerHTML"></form>`
	script := uiScriptForTest(t, content, uiSrcForTest())
	previous := -1
	for _, marker := range []string{"/* htmx */", "/* write-base */", "/* htmx-bridge */"} {
		position := strings.Index(script, marker)
		if position <= previous || strings.Count(script, marker) != 1 {
			t.Fatalf("写依赖必须唯一且顺序正确：%s", marker)
		}
		previous = position
	}
	for _, missing := range []string{"htmx.min.js", "write-base.js", "htmx-bridge.js"} {
		sources := uiSrcForTest()
		delete(sources, missing)
		_, _, err := uiAssetsFor(collectHTMLFeatures(content), "", sources)
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Fatalf("缺少写依赖 %s 应阻止构建：%v", missing, err)
		}
	}
}

func TestUIAssetExportsShareEmbeddedSources(t *testing.T) {
	// 公共 UI 属主在 pkg/view/ui；应用侧不再保留副本。
	entry, err := os.ReadFile("../../pkg/view/ui/templates/partials/ui_scripts.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range UIAssetFiles() {
		if strings.Contains(name, "form-json") || strings.Contains(name, "json_swap") {
			t.Fatalf("构建出口仍登记旧 JSON UI 执行器：%s", name)
		}
		source, err := templates.StaticJS("ui/" + name)
		if err != nil || strings.TrimSpace(source) == "" {
			t.Fatalf("构建资产缺少同源运行时嵌入：%s，%v", name, err)
		}
		for _, retired := range []string{"registerResultHandler", "WPJsonWrite"} {
			if strings.Contains(source, retired) {
				t.Fatalf("构建出口不得登记依赖已退役 JSON UI 接口的脚本：%s (%s)", name, retired)
			}
		}
		if strings.Count(string(entry), `/static/js/ui/`+name+`"`) != 1 {
			t.Fatalf("双出口必须共享同一份源且运行时只加载一次：%s", name)
		}
	}
}

func TestActionAndPreviewDependencyOrder(t *testing.T) {
	_, script, err := uiAssetsFor(collectHTMLFeatures(`<div data-site-read="/api/site/user/account" data-component-instance="one"><button data-component-action="go" data-write></button></div>`), "", uiSrcForTest())
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"/* site-preview */", "/* site-actions */", "/* site-data */", "/* write-base */"} {
		if !strings.Contains(script, marker) {
			t.Fatalf("缺少唯一依赖%s", marker)
		}
	}
	if strings.Index(script, "/* site-preview */") > strings.Index(script, "/* site-data */") || strings.Index(script, "/* site-preview */") > strings.Index(script, "/* write-base */") {
		t.Fatal("预览事实必须先于真实读写基座")
	}
}
