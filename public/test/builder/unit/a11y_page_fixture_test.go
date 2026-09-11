package unit

// a11y_page_fixture_test.go — 生成「含交互组件的完整页面」供浏览器人工/自动走查。
//
// 组件级断言只能证明标记结构对，证明不了键盘真的走得通（原生 radio group 的方向键、
// sr-only checkbox 的空格键、label[for] 的点击关联都要真实浏览器才能验）。
// 产物写到 /tmp/a11y-page.html，配合 CDP 发真实按键事件做走查。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
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
    {"id": "info1", "type": "core.infobox", "props": {"title": "信息框", "text": "说明文字", "mediaImage": "/a.jpg", "mediaAlt": "示例图"}},
    {"id": "form1", "type": "core.form", "props": {"submitLabel": "提交", "method": "post", "fields": [
      {"type": "text", "label": "姓名", "name": "name"},
      {"type": "textarea", "label": "留言", "name": "msg"},
      {"type": "select", "label": "城市", "name": "city", "options": ["北京", "上海"]},
      {"type": "checkbox", "label": "同意条款", "name": "agree"}
    ]}}
  ]
}`

// TestA11yPageFixture 生成走查页并做结构性断言（交互行为在浏览器里用真实按键验证）。
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
	// tabs：radio 在 tablist 内、可聚焦（不是 display:none），panel 与 tab 互指。
	for _, want := range []string{
		// tabs：radio 是原生单选组（键盘方向键原生可用），aria-controls 指向对应面板。
		`class="sky-tabs-radio" id="sky-tabs-tabs1-0" name="sky-tabs-tabs1" checked`,
		`aria-controls="sky-tabs-panel-tabs1-0"`,
		`id="sky-tabs-panel-tabs1-0"`,
		`for="sky-tabs-tabs1-0"`,
		// form：label[for] 与控件 id 关联。
		`for="sky-form-form1-name"`,
		`id="sky-form-form1-name"`,
		`id="sky-form-form1-msg"`,
		`id="sky-form-form1-city"`,
		// 前台产物也要带原始控件基座：form 的 <select> 打出 data-ui-select 特征，
		// 构建期据此内联下拉替身（访客页面同样不再依赖原生弹层）。
		`<select data-ui-select`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("走查页缺少 %q", want)
		}
	}
	// 不套 ARIA tab 模式：role=tab 要求 aria-selected 跟着切换走，而零 JS 方案更新不了它
	// —— 静态写死的 aria-selected 会在用户切换后变成假状态，比不写更糟。
	// 原生 radio group 的语义（单选一组）与「选一个面板」本来就一致，读屏播报准确。
	// 用 ARIA tab 专属的词做判据（aria-selected 在原始控件基座里是合法用法，不能当违规信号）。
	for _, unwanted := range []string{`role="tab"`, `role="tablist"`, `role="tabpanel"`} {
		if strings.Contains(doc, unwanted) {
			t.Errorf("tabs 不应输出 %q（零 JS 下会变成假 ARIA 状态）", unwanted)
		}
	}
	// 原始控件基座必须真的内联进产物（不是只有特征标记）：基座助手 + 控件 + 入口。
	// 只认基座自己的标识：aria-selected 之类的属性控件里也会合法出现，不能拿来当判据。
	for _, want := range []string{"WBUI.controls", "wbs-trigger", "WBUI.register"} {
		if !strings.Contains(doc, want) {
			t.Errorf("产物缺少原始控件基座内容 %q（前台下拉仍未受控）", want)
		}
	}
	// tabs 的 radio 不能用 display:none 隐藏（会让键盘序列里没有它）。
	if strings.Contains(compiled.CSS, ".sky-tabs-radio {\n  display: none") {
		t.Errorf("tabs 的 radio 仍被 display:none 隐藏（键盘无法切换）")
	}
	if !strings.Contains(compiled.CSS, "clip-path: inset(50%)") {
		t.Errorf("tabs 的 radio 缺少 sr-only 隐藏方式")
	}
	// nav 的折叠开关不能是 hidden。
	if strings.Contains(doc, `class="sky-nav-toggle" id="sky-nav-toggle-nav1" hidden`) {
		t.Errorf("导航折叠开关仍是 hidden（键盘无法展开菜单）")
	}

	out := filepath.Join(os.TempDir(), "a11y-page.html")
	if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
		t.Fatalf("写走查页失败: %v", err)
	}
	t.Logf("走查页已生成：%s（%.1f KB）", out, float64(len(doc))/1024)
}
