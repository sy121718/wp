package uispec

import (
	"context"
	"fmt"
	"strings"
)

// Resolver 按数据源名取一个块的 Data。
//
// 是接口而不是具体实现：本包不 import 任何模块（更不 import 工具注册表），
// 「已注册工具」由调用方适配成它。这样渲染这条链上没有任何业务依赖，
// 测试也只需要一个 stub。
type Resolver interface {
	Resolve(ctx context.Context, source string, params map[string]string, limit int) (any, error)
}

// TableRow 表格的一行。
//
// Cells 长度**恒等于**所属 TableView 的 Columns 长度 —— 由 buildTableView 保证。
// 这不是洁癖：模板层「表头 N 列、数据行写了 N-1 个 td」时浏览器不报错，而是把缺的那列
// 补在行尾，整张表**左移一列**（第一列显示第二列的值），模板不报错、计数断言也抓不到。
// 把补齐放在 Go 侧，这条约束就变成可测的。
type TableRow struct {
	Cells []TableCell
}

// TableCell 一个单元格。
type TableCell struct {
	Text string
	// Num 右对齐（金额 / 数量）。取值来自列定义，不由单元格内容猜 —— 猜列对齐
	// 会让「商品名恰好是数字」的行走偏。
	Num bool
}

// TableView 已就绪的表格，模板只做输出。
type TableView struct {
	Columns []Column
	Rows    []TableRow
	Empty   string
}

// ListView 已就绪的列表。
type ListView struct {
	Items []Item
	Empty string
}

// AccordionView 已就绪的手风琴。
type AccordionView struct {
	Groups []Group
	Empty  string
}

// StatView 已就绪的指标卡组。
type StatView struct {
	Items []Stat
	Empty string
}

// View 一个块的渲染上下文。
//
// 模板按 **Type 字符串**分派（`{{if eq .Type "stat"}}`），不按形状字段是否为 nil：
// Jet 的 `if` 要求 bool，nil 指针与 nil 接口会让渲染**在那一行中断**（整个响应 500、
// buffer 里的半截内容被丢弃；htmx 片段则因为 5xx 默认不 swap，用户看不到任何反应）。
// 用字符串比较就没有这个问题，`BuildView` 也保证「Type 与形状字段同进同出」。
type View struct {
	Type   string
	Title  string
	Stat   *StatView
	Table  *TableView
	List   *ListView
	Accord *AccordionView
}

// 单块执行结果的标签（进诊断事件；与校验的 Kind 一样是机器可读值）。
const (
	NoteOK       = "ok"        // 取数成功且形状匹配
	NoteNotFound = "not_found" // 数据源名查不到（模型编了个工具名）
	NoteShape    = "shape"     // 取到了数据，但形状与组件类型不匹配
	NoteFailed   = "failed"    // 取数本身失败
)

// Note 一个块的执行结果说明。
//
// 成功也记：诊断要能回答「模型这次用了哪些数据源、哪几个没用上」，
// 只记失败的话「它开始编工具名了」这种趋势要等到全错才看得出来。
type Note struct {
	Index  int
	Source string
	Kind   string
	// Msg 失败原因（成功时为空）。**只进日志与诊断事件，不进页面**。
	Msg string
}

// Execute 逐块取数并组装视图。
//
// 刻意不返回 error：单块失败不该让整条回答失败 —— 模型给了三张表，其中一张的数据源查不到，
// 另外两张仍然要显示。「有没有东西可渲染」由调用方看 views 是否为空来判断（空则降级为纯文本）。
//
// 逐块取数而不是并发：一次回答的块数上限是 MaxBlocks（8），并发带来的收益抵不上
// 「哪个块慢/失败」的排查成本；真要快，该快的是数据源本身。
func Execute(ctx context.Context, s *Spec, r Resolver) ([]View, []Note) {
	if s == nil || len(s.Blocks) == 0 {
		return nil, nil
	}
	views := make([]View, 0, len(s.Blocks))
	notes := make([]Note, 0, len(s.Blocks))
	for i, b := range s.Blocks {
		if r == nil {
			notes = append(notes, Note{Index: i, Source: b.Source, Kind: NoteFailed, Msg: "没有可用的数据源解析器"})
			continue
		}
		data, err := r.Resolve(ctx, b.Source, b.Params, b.Limit)
		if err != nil {
			notes = append(notes, Note{Index: i, Source: b.Source, Kind: NoteFailed, Msg: err.Error()})
			continue
		}
		v, ok := BuildView(b, data)
		if !ok {
			notes = append(notes, Note{
				Index: i, Source: b.Source, Kind: NoteShape,
				Msg: fmt.Sprintf("数据形状 %T 与组件 %s 不匹配", data, b.Type),
			})
			continue
		}
		views = append(views, v)
		notes = append(notes, Note{Index: i, Source: b.Source, Kind: NoteOK})
	}
	return views, notes
}

// BuildView 把块与它的数据配成视图；形状不匹配返回 false（调用方记一条形状不匹配的诊断）。
//
// 导出是给「只有一个块」的调用方用的（会话里的即时渲染），也方便测试直接钉形状匹配规则。
func BuildView(b Block, data any) (View, bool) {
	v := View{Type: b.Type, Title: strings.TrimSpace(b.Title)}
	switch b.Type {
	case TypeStat:
		d, ok := asStatData(data)
		if !ok {
			return v, false
		}
		v.Stat = &StatView{Items: d.Items, Empty: d.Empty}
	case TypeTable:
		d, ok := asTableData(data)
		if !ok {
			return v, false
		}
		// 没有列定义的表渲染不出表头，也无法保证「单元格数 == 列数」——
		// 与其渲染一张错位的表，不如让这个块降级（数据源写错了，该修的是它）。
		if len(d.Columns) == 0 {
			return v, false
		}
		tv := buildTableView(d)
		v.Table = &tv
	case TypeList:
		d, ok := asListData(data)
		if !ok {
			return v, false
		}
		v.List = &ListView{Items: d.Items, Empty: d.Empty}
	case TypeAccordion:
		d, ok := asAccordionData(data)
		if !ok {
			return v, false
		}
		v.Accord = &AccordionView{Groups: d.Groups, Empty: d.Empty}
	default:
		// 到不了这里：Validate 已经把类型限制在白名单内。真到了说明调用方绕过了校验，
		// 此时**宁可渲染不出来**（返回 false）也不要猜。
		return v, false
	}
	return v, true
}

// buildTableView 把原始表格补成「每行单元格数 == 列数」的渲染就绪形态。
//
// 多出来的单元格**截断**、少的**补空串**：数据源给了错位的行时，宁可显示成有空缺的表，
// 也不要让整张表左移一列（那会把金额显示到商品名那一列，看着像数据全错）。
func buildTableView(d TableData) TableView {
	cols := d.Columns
	num := make([]bool, len(cols))
	for i, c := range cols {
		num[i] = c.Align == "right"
	}
	rows := make([]TableRow, 0, len(d.Rows))
	for _, r := range d.Rows {
		cells := make([]TableCell, len(cols))
		for i := range cols {
			text := ""
			if i < len(r) {
				text = r[i]
			}
			cells[i] = TableCell{Text: text, Num: num[i]}
		}
		rows = append(rows, TableRow{Cells: cells})
	}
	return TableView{Columns: cols, Rows: rows, Empty: d.Empty}
}

// 下面四个适配函数都接受值或指针：数据源用哪种形式返回是它的自由，
// 渲染器不该因为「返回了 *T 而不是 T」拒掉一份正常数据。

// asStatData 取指标卡数据。
func asStatData(data any) (StatData, bool) {
	switch d := data.(type) {
	case StatData:
		return d, true
	case *StatData:
		if d == nil {
			return StatData{}, false
		}
		return *d, true
	default:
		return StatData{}, false
	}
}

// asTableData 取表格数据。
func asTableData(data any) (TableData, bool) {
	switch d := data.(type) {
	case TableData:
		return d, true
	case *TableData:
		if d == nil {
			return TableData{}, false
		}
		return *d, true
	default:
		return TableData{}, false
	}
}

// asListData 取列表数据。
func asListData(data any) (ListData, bool) {
	switch d := data.(type) {
	case ListData:
		return d, true
	case *ListData:
		if d == nil {
			return ListData{}, false
		}
		return *d, true
	default:
		return ListData{}, false
	}
}

// asAccordionData 取手风琴数据。
func asAccordionData(data any) (AccordionData, bool) {
	switch d := data.(type) {
	case AccordionData:
		return d, true
	case *AccordionData:
		if d == nil {
			return AccordionData{}, false
		}
		return *d, true
	default:
		return AccordionData{}, false
	}
}
