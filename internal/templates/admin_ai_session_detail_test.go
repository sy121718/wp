package templates

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	"go_wp/internal/uispec"
)

func renderSessionPage(t *testing.T, data map[string]any) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	if err := NewJetHTMLRender(".", true).Instance("admin/ai/sessions", data).Render(rec); err != nil {
		t.Fatalf("会话页渲染失败: %v", err)
	}
	return rec.Body.String()
}

func sessionPageBase() map[string]any {
	data := adminShellData()
	data["Rows"] = []any{}
	data["Total"] = int64(0)
	data["Page"] = 1
	data["PageSize"] = 20
	data["TotalPages"] = 1
	data["HasPrev"] = false
	data["HasNext"] = false
	data["Tab"] = "sessions"
	data["IsModels"] = false
	data["CanViewModels"] = false
	data["CanViewSessions"] = true

	data["Buttons"] = map[string]bool{}
	data["Filter"] = aidto.SessionQuery{}
	data["Usage"] = aidto.SessionUsage{}
	data["Trend"] = aidto.SessionTrend{}
	data["FilterOptions"] = aidto.SessionFilterOptions{}
	return data
}

func detailSection(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `id="ai-session-detail"`)
	if start < 0 {
		t.Fatalf("页面里没有详情区")
	}
	return body[start:]
}

func tableView() uispec.View {
	return uispec.View{Type: uispec.TypeTable, Title: "热销商品", Table: &uispec.TableView{
		Columns: []uispec.Column{{Label: "商品"}, {Label: "销量", Align: "num"}},
		Rows:    []uispec.TableRow{{Cells: []uispec.TableCell{{Text: "甲"}, {Text: "3", Num: true}}}},
	}}
}

func sessionEventRow(seq int64, kind, tone, content, tool, phase string, views []uispec.View) map[string]any {
	return map[string]any{
		"Seq": seq, "Kind": kind, "Tone": tone, "Content": content,
		"Views": views, "HasViews": len(views) > 0,
		"TimeLabel": "10-05 01:30", "Tool": tool, "Phase": phase,
		"ViewsCtx": map[string]any{"Views": views},
	}
}

func TestSessionDetailRendersBlocks(t *testing.T) {
	data := sessionPageBase()
	detail := aidto.SessionDetail{}
	detail.ID = 7
	detail.ProviderKey = "sess-tools"
	detail.ModelID = "muse-spark-1.3"
	detail.EventCount = 4
	detail.VisibleTokens = 1234
	detail.CompactCount = 1
	data["DetailID"] = int64(7)
	data["Detail"] = detail
	data["Events"] = []any{
		sessionEventRow(4, "assistant", "ok", "见下表\n第二行", "", "", []uispec.View{tableView()}),
		sessionEventRow(3, "tool", "warn", "已接受", "ui_render", "result", []uispec.View{}),
	}

	body := renderSessionPage(t, data)

	for _, want := range []string{`id="ai-session-detail"`, "#7", "sess-tools", "见下表", "ui_render"} {
		if !strings.Contains(body, want) {
			t.Errorf("详情区应含 %q", want)
		}
	}

	section := detailSection(t, body)
	if got := countTag(section, "th"); got != 2 {
		t.Errorf("表格应有 2 个表头，实际 %d", got)
	}
	if got := countTag(section, "td"); got != 2 {
		t.Errorf("表格应有 2 个单元格，实际 %d", got)
	}
	if !strings.Contains(section, "热销商品") {
		t.Error("表格标题应渲染出来")
	}

	if !strings.Contains(section, "见下表\n第二行") {
		t.Error("事件正文的换行应原样保留")
	}
}

func TestSessionDetailOmitsBlocksWhenNoRender(t *testing.T) {
	data := sessionPageBase()
	detail := aidto.SessionDetail{}
	detail.ID = 9
	data["DetailID"] = int64(9)
	data["Detail"] = detail
	data["Events"] = []any{
		sessionEventRow(2, "tool", "warn", "取到 2 行", "orders_top_products", "result", []uispec.View{}),
	}

	section := detailSection(t, renderSessionPage(t, data))

	if got := countTag(section, "th"); got != 0 {
		t.Errorf("没有 render 结构时不该有表格，实际 %d 个表头", got)
	}
	if !strings.Contains(section, "orders_top_products") {
		t.Error("工具名仍应显示（它是「模型做了什么」的证据）")
	}
}

func TestSessionDetailHiddenWithoutID(t *testing.T) {
	body := renderSessionPage(t, sessionPageBase())
	if strings.Contains(body, `id="ai-session-detail"`) {
		t.Error("不带 ?id= 时不该出现详情区")
	}
}
