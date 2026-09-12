package dashboardhttp

import (
	"strings"
	"testing"
)

// TestRenderAccordionRepeaterHTML 折叠项面板的服务端骨架。
//
// 钉住的是**数据契约**：客户端绑定函数（repeater.js 的 bindRepeaterPanel）靠这些
// data-* 属性工作，改名而不同步改绑定函数，面板就会「看着正常但点了没反应」。
func TestRenderAccordionRepeaterHTML(t *testing.T) {
	spec := repeaterSpecFor("core.accordion")
	if spec == nil {
		t.Fatal("accordion 应有重复项面板配置")
	}
	rows := []repeaterRow{
		{Value: "第一项", Extras: map[string]bool{"open": true}},
		{Value: "第二项", Extras: map[string]bool{}},
		{Value: "第三项", Extras: map[string]bool{}},
	}
	out := renderRepeaterHTML(spec, rows, 3)

	for _, want := range []string{
		`data-wb-rep="core.accordion"`,
		`data-wb-rep-field="title"`,
		`data-wb-rep-input="0"`,
		`data-wb-rep-input="2"`,
		`value="第一项"`,
		`placeholder="折叠项1 标题"`,
		`placeholder="折叠项3 标题"`,
		`data-wb-rep-extra="open" data-wb-rep-index="0" checked`,
		`data-wb-rep-extra="open" data-wb-rep-index="1">`, // 第二项未勾选
		`data-wb-rep-to="-1"`,
		`data-wb-rep-to="1"`,
		`data-wb-rep-op="remove" data-wb-rep-index="1"`,
		`data-wb-rep-op="add"`,
		`折叠项与面板数量一致（3）`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("骨架缺少 %s\n实际: %s", want, out)
		}
	}
	// 首行没有上移、末行没有下移：越界的移动按钮不该出现（点了也没有意义）。
	first := out[:strings.Index(out, `data-wb-rep-index="1"`)]
	if strings.Contains(first, `data-wb-rep-to="-1"`) {
		t.Error("第一行不该有上移按钮")
	}
	if strings.Contains(out, `data-wb-rep-op="move" data-wb-rep-index="2" data-wb-rep-to="1"`) {
		t.Error("最后一行不该有下移按钮")
	}
}

// TestRenderRepeaterHTMLCountMismatch 条目数与画布面板数不一致时给红色提示。
//
// 不一致是真实会发生的：历史脏数据、或者用户直接在画布上增删了面板。
// 提示要让用户保存前就知道会被校验拦下，而不是保存后才发现。
func TestRenderRepeaterHTMLCountMismatch(t *testing.T) {
	spec := repeaterSpecFor("core.tabs")
	rows := []repeaterRow{{Value: "A"}, {Value: "B"}}
	out := renderRepeaterHTML(spec, rows, 3)
	if !strings.Contains(out, "数量不一致（页签 2 个 / 面板 3 个）") {
		t.Errorf("应有数量不一致提示: %s", out)
	}
	if !strings.Contains(out, "var(--c-danger") {
		t.Error("不一致时应标红")
	}
	// 一致时不该有红色。
	ok := renderRepeaterHTML(spec, rows, 2)
	if strings.Contains(ok, "var(--c-danger") {
		t.Error("数量一致时不该标红")
	}
}

// TestRepeaterRowsOf props 里取不到数据时给空列表（与客户端同口径，不报错）。
func TestRepeaterRowsOf(t *testing.T) {
	spec := repeaterSpecFor("core.accordion")
	props := map[string]any{
		"items": []any{
			map[string]any{"title": "甲", "open": true},
			map[string]any{"title": "乙"},
			map[string]any{"open": false}, // 缺 title → 空串，不 panic
		},
	}
	rows := repeaterRowsOf(props, spec)
	if len(rows) != 3 {
		t.Fatalf("应解析出 3 行，实际 %d", len(rows))
	}
	if rows[0].Value != "甲" || !rows[0].Extras["open"] {
		t.Errorf("首行解析错误: %+v", rows[0])
	}
	if rows[2].Value != "" || rows[2].Extras["open"] {
		t.Errorf("缺字段的行应回落空值: %+v", rows[2])
	}
	if got := repeaterRowsOf(map[string]any{}, spec); len(got) != 0 {
		t.Errorf("无数据应为空列表，实际 %d 行", len(got))
	}
}

// TestAppendRepeaterPanel 只挂进内容分组；样式页签与非重复项组件都不挂。
func TestAppendRepeaterPanel(t *testing.T) {
	node := &docNode{
		ID: "acc1", Type: "core.accordion",
		Children: []docNode{{ID: "c1"}, {ID: "c2"}},
	}
	props := map[string]any{"items": []any{map[string]any{"title": "一"}, map[string]any{"title": "二"}}}
	// 每次取新切片：append 会复用底层数组，共用一份会让后续断言看到前面的结果。
	sections := func() []inspectorSection {
		return []inspectorSection{{Key: "content", Title: "内容"}, {Key: "style", Title: "样式"}}
	}
	contentSection := func(list []inspectorSection) inspectorSection { return list[0] }

	got := appendRepeaterPanel(sections(), node, props, "content")
	var html string
	for _, s := range got {
		for _, f := range s.Fields {
			if f.HTML != "" {
				html = f.HTML
			}
		}
	}
	if html == "" {
		t.Fatal("content 分组应追加重复项面板骨架")
	}
	if !strings.Contains(html, `data-wb-rep="core.accordion"`) {
		t.Errorf("骨架内容不对: %s", html)
	}
	// 样式页签不挂（重复项编辑属于内容）。
	if f := contentSection(appendRepeaterPanel(sections(), node, props, "style")).Fields; len(f) != 0 {
		t.Error("样式页签不该挂重复项面板")
	}
	// 不是重复项组件就不挂。
	other := &docNode{ID: "h1", Type: "core.heading"}
	if f := contentSection(appendRepeaterPanel(sections(), other, props, "content")).Fields; len(f) != 0 {
		t.Error("非重复项组件不该挂")
	}
}
