package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/pkg/builder"
)

const a11yPageJSON = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "无障碍走查页", "description": "tabs / nav / form 的键盘与读屏走查样本"}},
  "root": [
    {"id": "hd", "type": "core.container", "props": {"tag": "header", "layout": {"engine": "flex", "flex": {"direction": "row", "justify": "between", "align": "center", "wrap": false, "gap": "12px"}}}, "children": [
      {"id": "nav1", "type": "core.nav", "props": {"items": [{"label": "首页", "url": "/"}, {"label": "关于", "url": "/about"}], "mobileCollapse": true, "toggleLabel": "Menu"}}
    ]},
    {"id": "tabs1", "type": "core.tabs", "props": {"tabs": [{"label": "标签一"}, {"label": "标签二"}]}, "children": [
      {"id": "p1", "type": "core.heading", "props": {"text": "面板一", "tag": "h3"}},
      {"id": "p2", "type": "core.heading", "props": {"text": "面板二", "tag": "h3"}}
    ]},
    {"id": "card1", "type": "core.card", "props": {"title": "二级标题卡片", "text": "正文", "titleTag": "h2"}},
    {"id": "info1", "type": "core.infobox", "props": {"title": "信息框", "text": "说明文字", "mediaImage": "/storage/a.jpg", "mediaAlt": "示例图"}},
    {"id": "form1", "type": "core.form", "props": {"submitLabel": "提交", "method": "post", "fields": [
      {"type": "text", "label": "姓名", "name": "name"},
      {"type": "textarea", "label": "留言", "name": "msg"},
      {"type": "select", "label": "城市", "name": "city", "options": ["北京", "上海"]},
      {"type": "checkbox", "label": "同意条款", "name": "agree"}
    ]}}
  ]
}`

func TestA11yPageFixture(t *testing.T) {
	page, err := builder.ParsePage([]byte(a11yPageJSON))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	compiled, err := compile(t, page)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}

	for _, want := range []string{

		`class="sky-tabs-radio sky-sr-only" id="sky-tabs-tabs1-0" name="sky-tabs-tabs1" checked`,
		`aria-controls="sky-tabs-panel-tabs1-0"`,
		`id="sky-tabs-panel-tabs1-0"`,
		`for="sky-tabs-tabs1-0"`,

		`for="sky-form-form1-name"`,
		`id="sky-form-form1-name"`,
		`id="sky-form-form1-msg"`,
		`id="sky-form-form1-city"`,

		`<select data-ui-select`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("走查页缺少 %q", want)
		}
	}

	for _, unwanted := range []string{`role="tab"`, `role="tablist"`, `role="tabpanel"`} {
		if strings.Contains(doc, unwanted) {
			t.Errorf("tabs 不应输出 %q（零 JS 下会变成假 ARIA 状态）", unwanted)
		}
	}

	for _, want := range []string{"WBUI.controls", "wbs-trigger", "WBUI.register"} {
		if !strings.Contains(doc, want) {
			t.Errorf("产物缺少原始控件基座内容 %q（前台下拉仍未受控）", want)
		}
	}

	if strings.Contains(compiled.CSS, ".sky-tabs-radio {\n  display: none") {
		t.Errorf("tabs 的 radio 仍被 display:none 隐藏（键盘无法切换）")
	}
	if !strings.Contains(compiled.CSS, "clip-path: inset(50%)") {
		t.Errorf("tabs 的 radio 缺少 sr-only 隐藏方式")
	}

	if strings.Contains(doc, `class="sky-nav-toggle" id="sky-nav-toggle-nav1" hidden`) {
		t.Errorf("导航折叠开关仍是 hidden（键盘无法展开菜单）")
	}

	out := filepath.Join(os.TempDir(), "a11y-page.html")
	if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
		t.Fatalf("写走查页失败: %v", err)
	}
	t.Logf("走查页已生成：%s（%.1f KB）", out, float64(len(doc))/1024)
}
