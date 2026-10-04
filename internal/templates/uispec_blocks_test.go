package templates

import (
	"context"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/uispec"
)

// renderBlocks 渲染 spec 片段（调用方只需给一个 Views 键 —— 片段刻意不依赖 i18n 上下文）。
func renderBlocks(t *testing.T, views []uispec.View) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	data := gin.H{"Views": views}
	if err := NewJetHTMLRender(".", true).Instance("admin/ai/ui_blocks", data).Render(rec); err != nil {
		t.Fatalf("spec 片段渲染失败: %v", err)
	}
	return rec.Body.String()
}

// countTag 数某个标签出现次数（表头列数 / 每行 td 数要靠计数，不能只看「有没有」）。
//
// 用正则而不是 strings.Count(body, "<"+tag)：后者会把**前缀相同的容器标签**一起数进去
// （`<thead` 含 `<th`、`<tbody` 不含 `<td` 纯属运气），第一版就因此把 2 列表头数成 3。
// 判据必须锚定「标签名到此为止」。
func countTag(body, tag string) int {
	re := regexp.MustCompile(`<` + tag + `[ >]`)
	return len(re.FindAllString(body, -1))
}

// TestRenderSpecBlocksAllTypes 四种组件都能渲染出预期的既有类。
func TestRenderSpecBlocksAllTypes(t *testing.T) {
	sp := &uispec.Spec{Blocks: []uispec.Block{
		{Type: uispec.TypeStat, Source: "s1"},
		{Type: uispec.TypeTable, Title: "热销商品", Source: "s2"},
		{Type: uispec.TypeList, Source: "s3"},
		{Type: uispec.TypeAccordion, Source: "s4"},
	}}
	r := &fixedResolver{data: map[string]any{
		"s1": uispec.StatData{Items: []uispec.Stat{{Label: "今日订单", Value: "12", Delta: "+3"}}},
		"s2": uispec.TableData{
			Columns: []uispec.Column{{Label: "商品"}, {Label: "销量", Align: "right"}},
			Rows:    [][]string{{"手工皂", "12"}, {"精油", "3"}},
		},
		"s3": uispec.ListData{Items: []uispec.Item{{Label: "待付款", Value: "3"}}},
		"s4": uispec.AccordionData{Groups: []uispec.Group{{Title: "逐日", Items: []uispec.Item{{Label: "10-01", Value: "5 单"}}}}},
	}}
	views, notes := uispec.Execute(context.Background(), sp, r)
	for _, n := range notes {
		if n.Kind != uispec.NoteOK {
			t.Fatalf("取数应全部成功: %+v", n)
		}
	}
	body := renderBlocks(t, views)

	for _, want := range []string{
		`class="stat-grid"`, `class="card stat-card"`, `class="stat-label"`, `class="stat-value"`, `class="stat-note"`,
		`class="data-table"`, `class="table-wrap table-scroll"`, `tabindex="0"`, `role="region"`, `aria-label="热销商品"`,
		`class="col-num"`, `<details class="section-fold card">`, `class="fold-title"`, `class="fold-body"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("片段缺少 %q", want)
		}
	}
	if strings.Contains(body, "{{") || strings.Contains(body, "}}") {
		t.Errorf("有未渲染的模板标记（说明某处语法让 Jet 跳过了）")
	}

	// 列数必须与单元格数一致（表头 2 列 → 每行 2 个 td → 共 4 个）。
	thead := body[strings.Index(body, "<thead>"):strings.Index(body, "</thead>")]
	if got := countTag(thead, "th"); got != 2 {
		t.Fatalf("表头列数不对: %d", got)
	}
	tbody := body[strings.Index(body, "<tbody>"):strings.Index(body, "</tbody>")]
	if got := countTag(tbody, "td"); got != 4 {
		t.Fatalf("数据格数不对（应为 2 行 × 2 列）：%d", got)
	}
	// 右对齐只出现在第二列：第一列是商品名，不该是 col-num。
	rows := regexp.MustCompile(`<tr>(.*?)</tr>`).FindAllString(tbody, -1)
	if len(rows) != 2 {
		t.Fatalf("行数不对: %d", len(rows))
	}
	for _, row := range rows {
		cells := regexp.MustCompile(`<td[^>]*>`).FindAllString(row, -1)
		if len(cells) != 2 || strings.Contains(cells[0], "col-num") || !strings.Contains(cells[1], "col-num") {
			t.Fatalf("列对齐落错位置: %v", cells)
		}
	}
}

// TestRenderSpecBlocksEmpty 空数据时按 Empty 决定是否出提示，且不硬编码文案。
func TestRenderSpecBlocksEmpty(t *testing.T) {
	views := []uispec.View{
		{Type: uispec.TypeTable, Table: &uispec.TableView{
			Columns: []uispec.Column{{Label: "商品"}},
			Rows:    nil,
			Empty:   "区间内没有任何已付款的订单。",
		}},
		{Type: uispec.TypeStat, Stat: &uispec.StatView{Items: nil}},
	}
	body := renderBlocks(t, views)
	if !strings.Contains(body, "区间内没有任何已付款的订单。") {
		t.Errorf("Empty 文案没有渲染出来")
	}
	if strings.Contains(body, "暂无数据") || strings.Contains(body, "无数据") {
		t.Errorf("模板不该硬编码空态文案: %s", body)
	}
	// 没有 Empty 的块渲染出空，而不是一张没有行的空表。
	if strings.Contains(body, "<table") {
		t.Errorf("没有数据也没有提示时不该出现表格: %s", body)
	}
}

// TestRenderSpecBlocksSkipsUnknownType 类型不在白名单的 View 不渲染任何东西（而不是渲染成裸文本）。
func TestRenderSpecBlocksSkipsUnknownType(t *testing.T) {
	body := renderBlocks(t, []uispec.View{{Type: "chart"}})
	if strings.TrimSpace(body) != "" {
		t.Fatalf("未知类型不该渲染出内容: %q", body)
	}
}
