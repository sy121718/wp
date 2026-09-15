package builder

// ui_script_baseclass_test.go — 基座注入的 class 判定（UIK-002）。
//
// 判定规则有三条边界，缺任何一条都会静默出问题：
//   ① 文档 §10.2 的控件外观类，写了就要有样式（漏项 = 类在、样式不在，产物看着还是完整的）；
//   ② 工具类不触发注入（工具类几乎每页都有，命中即注入整份 ui.css，产物字节整体膨胀）；
//   ③ 清单里的类必须真在 ui.css 里（拼错或改名后清单还在，命中却拿不到样式）。
//
// 断言分两层：函数级（uiAssetsForScan 的返回）+ 产物级（RenderDocument 出的文档）——
// 「判定放宽」最容易伤到的就是产物字节，只在函数级看不出这件事。

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// documentedControlClasses 文档 §10.2「类名清单（唯一来源：static/css/ui.css）」里的控件外观类。
// 这是**对照集**：它变了说明文档口径变了，基座清单要跟着动。
var documentedControlClasses = []string{
	// 表单
	"form-input", "form-select", "form-textarea", "form-group", "form-label",
	"form-hint", "form-error", "form-row", "checkbox",
	// 按钮
	"btn", "btn-primary", "btn-secondary", "btn-ghost", "btn-danger", "btn-sm", "btn-icon",
	// 卡片
	"card", "card-header", "card-title", "card-body", "card-footer",
	// 表格
	"data-table", "table-wrap",
	// 徽标与状态点
	"badge", "badge-success", "badge-warning", "badge-danger", "badge-mute",
	"dot", "dot-success", "dot-warning", "dot-danger", "dot-mute",
}

// utilityClasses 文档 §10.2 末尾单列的工具类：故意不进基座清单。
// 它们必须真实存在于 ui.css —— 否则本测试是空转，证明不了「不收工具类」这个取舍。
var utilityClasses = []string{
	"w-full", "text-sm", "text-xs", "text-mute", "text-right",
	"flex", "items-center", "justify-between", "gap-md", "mt-md", "mb-md",
}

// classSelectorRe 从 CSS 源码里取类选择器名（够用即可：目的是核对清单里的类确实被定义过）。
var classSelectorRe = regexp.MustCompile("\\.([a-zA-Z][a-zA-Z0-9_-]*)")

func uiBaseClassSet() map[string]bool {
	set := make(map[string]bool, len(uiBaseClasses))
	for _, c := range uiBaseClasses {
		set[c] = true
	}
	return set
}

// TestUIBaseClassDocumentedControlsInjectStyle 文档列出的控件外观类逐个命中注入，
// 且只带样式不带脚本：class 说「要外观」，属性才说「要行为」，两者解耦。
func TestUIBaseClassDocumentedControlsInjectStyle(t *testing.T) {
	for _, cls := range documentedControlClasses {
		t.Run(cls, func(t *testing.T) {
			html := "<div class=\"" + cls + "\">内容</div>"
			css, script, err := uiAssetsForScan(collectHTMLScan(html), "/* css */", nil)
			if err != nil {
				t.Fatal(err)
			}
			if css != "/* css */" {
				t.Errorf("控件外观类 %q 应注入 ui.css，got css=%q", cls, css)
			}
			if script != "" {
				t.Errorf("只写 class 不该带出控件脚本，got %q", script)
			}
		})
	}
}

// TestUIBaseClassListCoversDocumentedControls 清单必须覆盖文档对照集，漏一个就是一处静默无样式。
func TestUIBaseClassListCoversDocumentedControls(t *testing.T) {
	inList := uiBaseClassSet()
	for _, cls := range documentedControlClasses {
		if !inList[cls] {
			t.Errorf("基座清单缺 %q：作者按文档写这个类拿不到样式（UIK-002 的原始症状）", cls)
		}
	}
}

// TestUIBaseClassUtilityClassesDoNotInject 工具类一个字节都不注入：
// 它们散落在每一页的布局里，一旦命中就等于所有产物都带上整份 ui.css。
func TestUIBaseClassUtilityClassesDoNotInject(t *testing.T) {
	inList := uiBaseClassSet()
	for _, cls := range utilityClasses {
		if inList[cls] {
			t.Errorf("工具类 %q 不该进基座清单（会让每个用到它的产物整份注入 ui.css）", cls)
		}
		html := "<div class=\"" + cls + "\">内容</div>"
		css, script, err := uiAssetsForScan(collectHTMLScan(html), "/* css */", uiSrcForTest())
		if err != nil {
			t.Fatal(err)
		}
		if css != "" || script != "" {
			t.Errorf("工具类 %q 不该触发任何注入，got css=%q script=%q", cls, css, script)
		}
	}
}

// TestUIBaseClassListMatchesUICSS 清单与 ui.css 对表。
// 清单是硬编码的：ui.css 改名或删类之后它照样存在，症状是「类写了、注入也触发了、样式还是没有」。
func TestUIBaseClassListMatchesUICSS(t *testing.T) {
	src, err := os.ReadFile("../templates/static/css/ui.css")
	if err != nil {
		t.Fatalf("读取 ui.css 失败（基座清单的唯一来源）：%v", err)
	}
	defined := map[string]bool{}
	for _, m := range classSelectorRe.FindAllStringSubmatch(string(src), -1) {
		defined[m[1]] = true
	}
	for _, cls := range uiBaseClasses {
		if !defined[cls] {
			t.Errorf("清单里的 %q 在 ui.css 里没有定义：命中它只会注入一份用不上的样式", cls)
		}
	}
	for _, cls := range utilityClasses {
		if !defined[cls] {
			t.Errorf("工具类 %q 不在 ui.css 里，「不收工具类」这条取舍的对照就失效了", cls)
		}
	}
}

// TestUIBaseClassOnlyPageInjectsStyleAtProductLevel 产物级对照：
// 只写 class 的文档要拿到 ui.css；纯内容页一个字节都不多。
func TestUIBaseClassOnlyPageInjectsStyleAtProductLevel(t *testing.T) {
	sources, css := uiSrcForTest(), "/* ui css */"

	out, err := RenderDocument(&CompiledPage{
		HTML:      "<div class=\"card\"><span class=\"badge badge-success\">新品</span><button class=\"btn btn-primary\">提交</button></div>",
		UISources: sources,
		UIStyle:   css,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, css) {
		t.Error("只写 class 的文档没有内联 ui.css：类在、样式不在（UIK-002）")
	}
	for _, unwanted := range []string{"/* util */", "/* select */", "/* index */"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("只写 class 不该带出控件脚本 %q", unwanted)
		}
	}

	plain, err := RenderDocument(&CompiledPage{
		HTML:      "<h1>纯内容</h1><p>没有任何控件类</p>",
		UISources: sources,
		UIStyle:   css,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{css, "/* util */", "/* index */"} {
		if strings.Contains(plain, unwanted) {
			t.Errorf("纯内容页不该注入 %q（判定放宽不能变成「每页都注入」）", unwanted)
		}
	}
}
