package builder

// ui_script_test.go — 原始控件基座的按需拼装。
//
// 守住两条边界：① 没用到控件时一个字节都不注入；② 用到时基座助手与入口必须随行
// （否则控件脚本会因 WBUI 未定义而报错，或扫描逻辑缺席导致控件不生效）。

import (
	"strings"
	"testing"
)

// uiSrcForTest 构造件名 → 源码，模拟装配层从 embed 读出的结果。
func uiSrcForTest() map[string]string {
	return map[string]string{
		"_util.js":  "/* util */ window.WBUI=window.WBUI||{};",
		"select.js": "/* select */ WBUI.register(function(){});",
		"index.js":  "/* index */ WBUI.scan(document);",
	}
}

// TestUIScriptSkippedWithoutFeature 纯内容页：不注入任何控件脚本。
func TestUIScriptSkippedWithoutFeature(t *testing.T) {
	got := uiScriptFor(`<section><h1>纯内容</h1></section>`, uiSrcForTest())
	if got != "" {
		t.Errorf("没有 data-ui-* 特征时不该注入控件，got %q", got)
	}
}

// TestUIScriptInjectsSelectWithBase 用到下拉：控件 + 基座助手 + 入口都要在。
func TestUIScriptInjectsSelectWithBase(t *testing.T) {
	got := uiScriptFor(`<select data-ui-select name="city"></select>`, uiSrcForTest())
	for _, want := range []string{"/* util */", "/* select */", "/* index */"} {
		if !strings.Contains(got, want) {
			t.Errorf("拼装结果缺少 %q\n%s", want, got)
		}
	}
	// 不带 <script>：document.jet 已在外层套了，重复套会让整段脚本失效。
	if strings.Contains(got, "<script") || strings.Contains(got, "</script>") {
		t.Errorf("拼装结果不应包含 script 标签（外层模板已提供）:\n%s", got)
	}
	// 顺序：助手在前，入口在后（入口负责扫描，必须在控件定义之后）。
	if strings.Index(got, "/* util */") > strings.Index(got, "/* select */") {
		t.Errorf("基座助手应在控件之前")
	}
	if strings.Index(got, "/* index */") < strings.Index(got, "/* select */") {
		t.Errorf("入口应在控件之后")
	}
}

// TestUIScriptIgnoresSelfReference 特征扫描必须剥掉 script 块：
// 否则内联过的产物再次编译时，脚本源码里的 data-ui-select 字样会把自己"检测"出来。
func TestUIScriptIgnoresSelfReference(t *testing.T) {
	html := `<h1>纯内容</h1><script>var s = "data-ui-select";</script>`
	if got := uiScriptFor(html, uiSrcForTest()); got != "" {
		t.Errorf("script 块内的字样不应触发注入，got %q", got)
	}
}

// TestUIScriptMissingSourceIsSkipped 登记了控件但源码缺失：跳过并告警，不产出半截脚本。
func TestUIScriptMissingSourceIsSkipped(t *testing.T) {
	got := uiScriptFor(`<select data-ui-select></select>`, map[string]string{"_util.js": "x"})
	if got != "" {
		t.Errorf("控件源码缺失时不该拼出残缺脚本，got %q", got)
	}
}
