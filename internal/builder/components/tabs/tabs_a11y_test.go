package tabs

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// tabs_a11y_test.go — UI-009：页签标签的可访问名。
//
// 零 JS 方案是 sr-only radio + label[for]，radio 的可访问名由关联 label 的文本提供。
// 原模板给 label 加了 aria-hidden="true"，等于把控件名的来源藏起来（读屏只会念
// 「单选按钮」而没有标签文本）。正解是不加 aria-hidden —— 关联 label 的文本本就
// 作为控件名被引用，不会被读两遍。

// TestTabsLabelProvidesAccessibleName 标签文本必须对无障碍树可见。
func TestTabsLabelProvidesAccessibleName(t *testing.T) {
	if strings.Contains(tabsTemplate, "aria-hidden") {
		t.Errorf("标签模板里仍有 aria-hidden（radio 的可访问名来源被隐藏）：\n%s", tabsTemplate)
	}
	if !strings.Contains(tabsTemplate, `for="{{ t.ID }}"`) {
		t.Errorf("标签未与 radio 关联（label[for] 缺失，控件没有可访问名）：\n%s", tabsTemplate)
	}
	if !strings.Contains(tabsTemplate, `{{ t.Label }}`) {
		t.Errorf("标签文本未输出：\n%s", tabsTemplate)
	}
}

// TestTabsRadioStaysFocusable 键盘可切换的前提：radio 用 sr-only 而不是 display:none。
func TestTabsRadioStaysFocusable(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Tabs: []Tab{{Label: "一"}}}, &b)
	out := b.String()
	if !strings.Contains(out, "clip-path: inset(50%)") {
		t.Errorf("radio 不是 sr-only 隐藏（clip-path 缺失，可能被 display:none 移出键盘序列）：\n%s", out)
	}
	if strings.Contains(out, ".sky-tabs-radio {\n  display: none") {
		t.Errorf("radio 被 display:none 隐藏（键盘无法切换页签）：\n%s", out)
	}
	// 聚焦可见：焦点环画在对应标签上。
	if !strings.Contains(out, ".sky-tabs-radio:focus-visible + .sky-tabs-tab {") {
		t.Errorf("标签缺少 focus-visible 焦点环：\n%s", out)
	}
}
