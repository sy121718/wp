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

func countTag(body, tag string) int {
	re := regexp.MustCompile(`<` + tag + `[ >]`)
	return len(re.FindAllString(body, -1))
}

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

	thead := body[strings.Index(body, "<thead>"):strings.Index(body, "</thead>")]
	if got := countTag(thead, "th"); got != 2 {
		t.Fatalf("表头列数不对: %d", got)
	}
	tbody := body[strings.Index(body, "<tbody>"):strings.Index(body, "</tbody>")]
	if got := countTag(tbody, "td"); got != 4 {
		t.Fatalf("数据格数不对（应为 2 行 × 2 列）：%d", got)
	}

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

	if strings.Contains(body, "<table") {
		t.Errorf("没有数据也没有提示时不该出现表格: %s", body)
	}
}

func TestRenderSpecBlocksSkipsUnknownType(t *testing.T) {
	body := renderBlocks(t, []uispec.View{{Type: "chart"}})
	if strings.TrimSpace(body) != "" {
		t.Fatalf("未知类型不该渲染出内容: %q", body)
	}
}
