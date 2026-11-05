package builder

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var documentedControlClasses = []string{

	"form-input", "form-select", "form-textarea", "form-group", "form-label",
	"form-hint", "form-error", "form-row", "checkbox",

	"btn", "btn-primary", "btn-secondary", "btn-ghost", "btn-danger", "btn-sm", "btn-icon",

	"card", "card-header", "card-title", "card-body",

	"data-table", "table-wrap",

	"badge", "badge-success", "badge-warning", "badge-danger", "badge-mute",
	"dot",
}

var utilityClasses = []string{
	"text-sm", "text-xs", "text-mute", "flex",
	"gap-sm", "mt-lg", "mb-lg", "col-actions", "sr-only",
}

var classSelectorRe = regexp.MustCompile("\\.([a-zA-Z][a-zA-Z0-9_-]*)")

func uiBaseClassSet() map[string]bool {
	set := make(map[string]bool, len(uiBaseClasses))
	for _, c := range uiBaseClasses {
		set[c] = true
	}
	return set
}

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

func TestUIBaseClassListCoversDocumentedControls(t *testing.T) {
	inList := uiBaseClassSet()
	for _, cls := range documentedControlClasses {
		if !inList[cls] {
			t.Errorf("基座清单缺 %q：作者按文档写这个类拿不到样式（UIK-002 的原始症状）", cls)
		}
	}
}

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

func TestUIBaseClassListMatchesUICSS(t *testing.T) {
	src, err := os.ReadFile("../view/ui/assets/css/ui.css")
	if err != nil {
		t.Fatalf("读取 ui.css 失败（基座清单的唯一来源，属主 pkg/view/ui）：%v", err)
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
