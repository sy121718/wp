package uispec

import (
	"context"
	"errors"
	"testing"
)

// resolveCall 一次调用的实参。
//
// 记**每次**调用而不是「最后一次」：第一版只留了最后一组的 params/limit，结果断言永远
// 对着最后一个块（它本来就没传参数）—— 用例红了，红的原因却是判据写错了，不是代码错了。
type resolveCall struct {
	source string
	params map[string]string
	limit  int
}

// stubResolver 记录调用并返回预置结果。
type stubResolver struct {
	data  map[string]any
	errs  map[string]error
	calls []resolveCall
}

func (s *stubResolver) Resolve(_ context.Context, source string, params map[string]string, limit int) (any, error) {
	s.calls = append(s.calls, resolveCall{source: source, params: params, limit: limit})
	if err, ok := s.errs[source]; ok {
		return nil, err
	}
	if d, ok := s.data[source]; ok {
		return d, nil
	}
	return nil, errors.New("数据源不存在")
}

// TestExecuteBuildsViewsForEachType 四种组件各配一次，形状都应对上。
func TestExecuteBuildsViewsForEachType(t *testing.T) {
	r := &stubResolver{data: map[string]any{
		"orders_summary":       StatData{Items: []Stat{{Label: "今日订单", Value: "12"}}},
		"orders_top_products":  TableData{Columns: []Column{{Label: "商品"}, {Label: "金额", Align: "right"}}, Rows: [][]string{{"手工皂", "¥128.00"}}},
		"orders_status_counts": ListData{Items: []Item{{Label: "待付款", Value: "3"}}},
		"orders_daily":         AccordionData{Groups: []Group{{Title: "逐日", Items: []Item{{Label: "10-01", Value: "5 单"}}}}},
	}}
	spec := &Spec{Blocks: []Block{
		{Type: TypeStat, Title: "概览", Source: "orders_summary"},
		{Type: TypeTable, Source: "orders_top_products", Params: map[string]string{"from": "2026-10-01"}, Limit: 5},
		{Type: TypeList, Source: "orders_status_counts"},
		{Type: TypeAccordion, Source: "orders_daily"},
	}}
	views, notes := Execute(context.Background(), spec, r)
	if len(views) != 4 {
		t.Fatalf("视图数不对: %d（notes=%+v）", len(views), notes)
	}
	for _, n := range notes {
		if n.Kind != NoteOK {
			t.Errorf("块 %d 应为 ok: %+v", n.Index, n)
		}
	}
	if views[0].Stat == nil || views[1].Table == nil || views[2].List == nil || views[3].Accord == nil {
		t.Fatalf("形状没落到对应字段: %+v", views)
	}
	if views[0].Title != "概览" {
		t.Errorf("标题没带过来: %q", views[0].Title)
	}
	// 参数与 limit 必须原样透传给数据源：它们最终会进查询，改一格就是另一个数。
	// 索引 1 = 第二个块（带 params 与 limit 的那个）。
	if r.calls[1].limit != 5 {
		t.Errorf("limit 没透传: %d", r.calls[1].limit)
	}
	if r.calls[1].params["from"] != "2026-10-01" {
		t.Errorf("params 没透传: %+v", r.calls[1].params)
	}
}

// TestExecuteOneBlockFailureDoesNotKillTheRest 单块失败时其余块照常渲染。
//
// 这是 Execute 刻意不返回 error 的原因：三张表里一张的数据源查不到，
// 另外两张仍然要对用户可见。
func TestExecuteOneBlockFailureDoesNotKillTheRest(t *testing.T) {
	r := &stubResolver{
		data: map[string]any{"orders_summary": StatData{Items: []Stat{{Label: "a", Value: "1"}}}},
		errs: map[string]error{"orders_daily": errors.New("数据库超时")},
	}
	spec := &Spec{Blocks: []Block{
		{Type: TypeStat, Source: "orders_summary"},
		{Type: TypeTable, Source: "orders_daily"},     // 取数失败
		{Type: TypeTable, Source: "made_up_by_model"}, // 数据源不存在
	}}
	views, notes := Execute(context.Background(), spec, r)
	if len(views) != 1 {
		t.Fatalf("成功块数不对: %d", len(views))
	}
	if len(notes) != 3 {
		t.Fatalf("每条块都应有一条说明（成功也要记）: %d", len(notes))
	}
	if notes[1].Kind != NoteFailed || notes[1].Msg == "" {
		t.Errorf("失败块的原因没记: %+v", notes[1])
	}
	if notes[2].Kind != NoteFailed {
		t.Errorf("不存在的数据源应记 failed: %+v", notes[2])
	}
}

// TestExecuteShapeMismatch 形状与组件类型不匹配时该块单独降级（其余块不受影响）。
func TestExecuteShapeMismatch(t *testing.T) {
	r := &stubResolver{data: map[string]any{
		// 数据源返回的是表格，模型却要 stat。
		"orders_top_products": TableData{Columns: []Column{{Label: "商品"}}, Rows: [][]string{{"皂"}}},
		"orders_summary":      StatData{Items: []Stat{{Label: "a", Value: "1"}}},
	}}
	spec := &Spec{Blocks: []Block{
		{Type: TypeTable, Source: "orders_top_products"},
		{Type: TypeStat, Source: "orders_top_products"}, // 形状不匹配
		{Type: TypeStat, Source: "orders_summary"},
	}}
	views, notes := Execute(context.Background(), spec, r)
	if len(views) != 2 {
		t.Fatalf("成功块数不对: %d", len(views))
	}
	if notes[1].Kind != NoteShape {
		t.Errorf("形状不匹配应记 shape: %+v", notes[1])
	}
}

// TestExecuteEdgeCases 空 spec / 没有解析器 / 指针数据。
func TestExecuteEdgeCases(t *testing.T) {
	views, notes := Execute(context.Background(), nil, nil)
	if views != nil || notes != nil {
		t.Fatalf("空 spec 应返回 nil: %v / %v", views, notes)
	}
	views, notes = Execute(context.Background(), &Spec{Text: "只有文字"}, nil)
	if views != nil || notes != nil {
		t.Fatalf("没有块时应返回 nil: %v / %v", views, notes)
	}

	// 有块但没有解析器：该块记 failed，而不是 panic。
	spec := &Spec{Blocks: []Block{{Type: TypeStat, Source: "orders_summary"}}}
	views, notes = Execute(context.Background(), spec, nil)
	if len(views) != 0 || len(notes) != 1 || notes[0].Kind != NoteFailed {
		t.Fatalf("没有解析器时应记一条 failed: %v / %+v", views, notes)
	}
}

// TestBuildViewAcceptsPointer 值型与指针型都接受。
func TestBuildViewAcceptsPointer(t *testing.T) {
	ptr := &TableData{Columns: []Column{{Label: "c"}}, Rows: [][]string{{"v"}}}
	v, ok := BuildView(Block{Type: TypeTable}, ptr)
	if !ok || v.Table == nil || v.Table.Rows[0].Cells[0].Text != "v" {
		t.Fatalf("指针型数据应被接受: %+v ok=%v", v, ok)
	}
	// nil 指针不能当成「空表」，否则会渲染出一张没有列的空表格。
	if _, ok := BuildView(Block{Type: TypeTable}, (*TableData)(nil)); ok {
		t.Fatalf("nil 指针不应被接受")
	}
	// 未知类型（绕过校验时）不猜，直接拒绝。
	if _, ok := BuildView(Block{Type: "chart"}, StatData{}); ok {
		t.Fatalf("白名单外的类型不应渲染")
	}
}

// TestBuildViewPadsRowsToColumnCount 行单元格数必须等于列数（多截断、少补空）。
//
// 这条不是洁癖：模板里「表头 N 列、数据行 N-1 个 td」时浏览器不报错，而是把缺的那列补在
// 行尾，整张表**左移一列**（第一列显示第二列的值）。在 Go 侧补齐 + 在这里钉住，
// 这类缺陷才变成可测的。
func TestBuildViewPadsRowsToColumnCount(t *testing.T) {
	d := TableData{
		Columns: []Column{{Label: "商品"}, {Label: "销量", Align: "right"}, {Label: "金额", Align: "right"}},
		Rows: [][]string{
			{"手工皂", "12", "¥1,536.00"},    // 齐
			{"精油", "3"},                   // 少一格
			{"蜡烛", "5", "¥200.00", "多余的"}, // 多一格
		},
	}
	v, ok := BuildView(Block{Type: TypeTable}, d)
	if !ok {
		t.Fatalf("应通过")
	}
	for i, row := range v.Table.Rows {
		if len(row.Cells) != len(v.Table.Columns) {
			t.Fatalf("第 %d 行单元格数 %d != 列数 %d", i, len(row.Cells), len(v.Table.Columns))
		}
	}
	if v.Table.Rows[1].Cells[2].Text != "" {
		t.Errorf("缺的格子应补空串: %q", v.Table.Rows[1].Cells[2].Text)
	}
	if v.Table.Rows[2].Cells[2].Text != "¥200.00" {
		t.Errorf("多余的格子应被截断，不能挤掉正常列: %q", v.Table.Rows[2].Cells[2].Text)
	}
	// 对齐来自列定义，不由单元格内容猜。
	if !v.Table.Rows[0].Cells[1].Num || v.Table.Rows[0].Cells[0].Num {
		t.Errorf("右对齐列判定不对: %+v", v.Table.Rows[0].Cells)
	}
}

// TestBuildViewRejectsTableWithoutColumns 没有列定义的表不渲染（渲染出来没有表头且无法保证列数）。
func TestBuildViewRejectsTableWithoutColumns(t *testing.T) {
	if _, ok := BuildView(Block{Type: TypeTable}, TableData{Rows: [][]string{{"a"}}}); ok {
		t.Fatalf("没有列定义的表应降级")
	}
}
