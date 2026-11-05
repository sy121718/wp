package tabs

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

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

func TestTabsRadioStaysFocusable(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Tabs: []Tab{{Label: "一"}}}, &b)
	out := b.String()

	if !strings.Contains(out, ".sky-c-t .sky-tabs-radio:focus-visible + .sky-tabs-tab {") {
		t.Errorf("焦点环规则缺失（键盘用户看不到当前页签）：\n%s", out)
	}
	if strings.Contains(out, ".sky-tabs-radio {\n  display: none") {
		t.Errorf("radio 被 display:none 隐藏（键盘无法切换页签）：\n%s", out)
	}

	if !strings.Contains(out, ".sky-tabs-radio:focus-visible + .sky-tabs-tab {") {
		t.Errorf("标签缺少 focus-visible 焦点环：\n%s", out)
	}
}
