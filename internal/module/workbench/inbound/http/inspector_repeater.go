package workbenchhttp

// inspector_repeater.go — 重复项面板的服务端骨架（折叠项 / 页签）。
//
// 面板 HTML 在这里生成，不再由浏览器拼 DOM：结构只有一处定义。客户端只做事件委托
// （改条目名、上移、下移、删除、添加）；需要读写文档 AST 的那一半（对齐语义 ——
// 增删条目时同步画布上对应的面板节点、分配节点 ID）仍留在客户端，服务端拿不到那份状态。
// 见 workbench/methods/controls/repeater.js 的 bindRepeaterPanel。
//
// 数据契约（客户端靠这些属性工作，改名要同步改绑定函数）：
//
//	data-wb-rep         根元素，值为组件类型
//	data-wb-rep-field   主字段键
//	data-wb-rep-input   主字段输入框，值为条目序号
//	data-wb-rep-extra   额外布尔字段键
//	data-wb-rep-index   条目序号
//	data-wb-rep-op      add / remove / move（move 另带 data-wb-rep-to）
//
// 与客户端渲染版的差别只在「结构由谁生成」：类名、按钮文案、提示语逐字一致，
// 否则同一次改动的产物与老路径会长得不一样，回归时看不出是哪种来源。
//
// **适用范围（别硬套）**：这里只覆盖「数组 ↔ 子节点一一对应」的对齐型面板 ——
// 折叠项数 = 面板数、页签数 = 面板数，增删条目必须同步增删画布节点（对齐语义在
// palette.js 的 alignMutation）。另两类面板不是这套模式：
//
//	普通数组（faq / social）  条目与子节点无关，增删只改 props；faq 的每行还挂一个
//	                        富文本答案字段（客户端增强控件），服务端只能给占位。
//	嵌套数组（nav）          条目带子项递归、两个字段、目标切换，行结构不是一维的。
//
// 支持范围由组件的 AlignedRepeaterProvider 声明，注册时核对真实 Props 类型；
// Go 面板与 generated-contracts.js 共用声明，不在工作台维护组件配置表。

import (
	"fmt"
	"html"
	"strings"

	"go_wp/internal/builder/core"
	workbenchenums "go_wp/internal/module/workbench/enums"
)

// repeaterRow 一条重复项的数据。
type repeaterRow struct {
	Value  string
	Extras map[string]bool
}

// repeaterRowsOf 从 props 里取出重复项数据（取不到就是空列表，与客户端同口径）。
func repeaterRowsOf(props map[string]any, spec *core.AlignedRepeaterSpec) []repeaterRow {
	raw, _ := props[spec.AlignKey].([]any)
	rows := make([]repeaterRow, 0, len(raw))
	for _, it := range raw {
		entry, _ := it.(map[string]any)
		row := repeaterRow{Extras: map[string]bool{}}
		if v, ok := entry[spec.Field].(string); ok {
			row.Value = v
		}
		for _, ex := range spec.Extra {
			if b, ok := entry[ex.Key].(bool); ok {
				row.Extras[ex.Key] = b
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// renderRepeaterHTML 生成重复项面板骨架。
//
// panelCount 是画布上对应的面板节点数：两者不一致时给红色提示（历史脏数据或直接在
// 画布上增删面板都可能造成不一致），让用户保存前就知道会被校验拦下。
//
// tr 是「key → 当前语言文案」的取词函数（workbenchTrFunc）：本文件的 HTML 由 Go 拼串产出，
// 模板只做 `|unsafe` 插槽，所以按钮提示与数量说明必须在这里取词。
// spec.Noun（项名，如「折叠项」）来自组件声明（builder/core），不属于本模块的文案，
// 作为占位符 {noun} 填进已翻译的句子 —— 译文语序与中文不同也不会错位。
func renderRepeaterHTML(spec *core.AlignedRepeaterSpec, rows []repeaterRow, panelCount int, tr func(key string) string) string {
	var b strings.Builder
	b.WriteString(`<div class="wb-repeater" data-wb-rep="` + html.EscapeString(spec.Type) +
		`" data-wb-rep-field="` + html.EscapeString(spec.Field) + `">`)
	for i, row := range rows {
		b.WriteString(`<div class="wb-repeater-row" data-wb-rep-index="` + fmt.Sprint(i) + `">`)
		b.WriteString(`<div class="wb-repeater-mid">`)
		placeholder := spec.Noun + fmt.Sprint(i+1) + " " + spec.Label
		b.WriteString(`<input type="text" data-wb-rep-input="` + fmt.Sprint(i) +
			`" value="` + html.EscapeString(row.Value) + `" placeholder="` + html.EscapeString(placeholder) + `">`)
		for _, ex := range spec.Extra {
			checked := ""
			if row.Extras[ex.Key] {
				checked = " checked"
			}
			b.WriteString(`<label class="wb-check-field"><input type="checkbox" data-wb-rep-extra="` +
				html.EscapeString(ex.Key) + `" data-wb-rep-index="` + fmt.Sprint(i) + `"` + checked + `> ` + html.EscapeString(ex.Label) + `</label>`)
		}
		b.WriteString(`</div><div class="wb-repeater-acts">`)
		if i > 0 {
			b.WriteString(repeaterButton("↑", tr(workbenchenums.InspectorRepeaterMoveUp), "move", i, "-1", "wb-btn wb-btn-sm wb-btn-ghost"))
		}
		if i < len(rows)-1 {
			b.WriteString(repeaterButton("↓", tr(workbenchenums.InspectorRepeaterMoveDown), "move", i, "1", "wb-btn wb-btn-sm wb-btn-ghost"))
		}
		b.WriteString(repeaterButton("✕",
			strings.ReplaceAll(tr(workbenchenums.InspectorRepeaterRemove), "{noun}", spec.Noun),
			"remove", i, "", "wb-icon-btn"))
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`<button type="button" class="wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add" data-wb-rep-op="add">` +
		html.EscapeString(spec.AddText) + `</button>`)
	b.WriteString(`<p class="wb-empty"`)
	if len(rows) != panelCount {
		b.WriteString(` style="color: var(--c-danger, #d93425)"`)
	}
	b.WriteString(`>`)
	if len(rows) == panelCount {
		b.WriteString(html.EscapeString(fillRepeaterText(tr(workbenchenums.InspectorRepeaterMatched), map[string]string{
			"{noun}": spec.Noun, "{count}": fmt.Sprint(len(rows)),
		})))
	} else {
		b.WriteString(html.EscapeString(fillRepeaterText(tr(workbenchenums.InspectorRepeaterMismatch), map[string]string{
			"{noun}": spec.Noun, "{rows}": fmt.Sprint(len(rows)), "{panels}": fmt.Sprint(panelCount),
		})))
	}
	b.WriteString(`</p></div>`)
	return b.String()
}

// fillRepeaterText 用占位符值填充已翻译的句子（占位符约定同 sys_i18n 的 {name}）。
//
// 键之间互不为子串，所以 map 的遍历顺序不影响结果；用它而不是 fmt.Sprintf 是为了让
// 译文重新排序占位符时不必改 Go 代码。
func fillRepeaterText(text string, values map[string]string) string {
	for k, v := range values {
		text = strings.ReplaceAll(text, k, v)
	}
	return text
}

// repeaterButton 行内操作按钮（to 为相对位移，仅 move 用）。
func repeaterButton(text, title, op string, index int, to, cls string) string {
	s := `<button type="button" class="` + cls + `" data-wb-rep-op="` + op +
		`" data-wb-rep-index="` + fmt.Sprint(index) + `"`
	if to != "" {
		s += ` data-wb-rep-to="` + to + `"`
	}
	return s + ` title="` + html.EscapeString(title) + `">` + html.EscapeString(text) + `</button>`
}

// appendRepeaterPanel 把重复项面板的服务端骨架挂进「内容」分组末尾。
//
// 只出结构 —— 行为留在客户端（repeater.js 的 bindRepeaterPanel）。
func appendRepeaterPanel(sections []inspectorSection, node *docNode, props map[string]any, tab string, tr func(key string) string) []inspectorSection {
	if tab == "style" || tab == "motion" {
		return sections
	}
	spec := core.AlignedRepeaterFor(node.Type)
	if spec == nil {
		return sections
	}
	field := inspectorField{
		Key:  "__repeater",
		HTML: renderRepeaterHTML(spec, repeaterRowsOf(props, spec), len(node.Children), tr),
	}
	for i := range sections {
		if sections[i].Key == "content" {
			sections[i].Fields = append(sections[i].Fields, field)
			return sections
		}
	}
	// 组件没有内容分组时补一个：tabs 的字段全在样式里（竖向 / 对齐 / 配色），
	// 但「页签列表」本身是内容 —— 挂在样式分组里位置不对。
	// content 是分组顺序表 inspectorSectionOrder 的第一项，前置插入即可。
	return append([]inspectorSection{{Key: "content", Title: tr(workbenchenums.InspectorSectionContent), Open: true, Fields: []inspectorField{field}}}, sections...)
}
