package templates

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	"go_wp/internal/uispec"
)

// renderSessionPage 渲染会话页（列表 + 可选的详情区）。
func renderSessionPage(t *testing.T, data map[string]any) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	if err := NewJetHTMLRender(".", true).Instance("admin/ai/sessions", data).Render(rec); err != nil {
		t.Fatalf("会话页渲染失败: %v", err)
	}
	return rec.Body.String()
}

// sessionPageBase 会话页的最小可渲染数据（列表与看板都空、会话标签可见）。
//
// 必须以 adminShellData() 为底：layout 要 lang / csrf / t，
// 少了 t 时模板里的 tr() 取到 nil —— 不报错，但整页文案全空。
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
	// 页面壳把这些键绑成局部变量后逐字段访问，缺一个就是运行时错误（Jet 不会容忍 nil 解引用）。
	// 给零值而不是让它们缺席 —— 零值渲染成空看板，正是「这一页没有数据」的样子。
	data["Buttons"] = map[string]bool{}
	data["Filter"] = aidto.SessionQuery{}
	data["Usage"] = aidto.SessionUsage{}
	data["Trend"] = aidto.SessionTrend{}
	data["FilterOptions"] = aidto.SessionFilterOptions{}
	return data
}

// detailSection 截出详情区那一段。
//
// 全局计数在这里会骗人：会话列表本身就是一张表（表头好几个），
// 「页面里有几个 th」根本回答不了「图表有没有渲染出来」。判据必须限定在详情区内。
func detailSection(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `id="ai-session-detail"`)
	if start < 0 {
		t.Fatalf("页面里没有详情区")
	}
	return body[start:]
}

// tableView 一份两列表格视图。
func tableView() uispec.View {
	return uispec.View{Type: uispec.TypeTable, Title: "热销商品", Table: &uispec.TableView{
		Columns: []uispec.Column{{Label: "商品"}, {Label: "销量", Align: "num"}},
		Rows:    []uispec.TableRow{{Cells: []uispec.TableCell{{Text: "甲"}, {Text: "3", Num: true}}}},
	}}
}

// sessionEventRow 一条事件行，形状与 handler 里 sessionEventRows 的产物一致。
//
// ViewsCtx 是给 ui_blocks.html 的：那个片段读的是上下文键 **Views**（它自己 range .Views），
// 所以 include 时传的必须是带这个键的上下文，而不是切片本身。
func sessionEventRow(seq int64, kind, tone, content, tool, phase string, views []uispec.View) map[string]any {
	return map[string]any{
		"Seq": seq, "Kind": kind, "Tone": tone, "Content": content,
		"Views": views, "HasViews": len(views) > 0,
		"TimeLabel": "10-05 01:30", "Tool": tool, "Phase": phase,
		"ViewsCtx": map[string]any{"Views": views},
	}
}

// TestSessionDetailRendersBlocks 详情区把事件里的图表真的渲染出来。
//
// 这是整条展示链路的终点：ui_render → 逐块取数 → meta.render → 详情页。
// 到这一步之前，模型产出的图表只是库里的一段 JSON，没人看得见。
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

	// 图表：两列表头 + 一行两格（用 countTag 计数而不是 Contains —— 容器标签前缀会骗人，
	// `<thead` 里就有 `<th`）。
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
	// 正文换行要保住（模型回复是多段文本）：模板只能是 pre-wrap 那条路径，
	// 不能把 \n 吞掉。这里断言的是「原始换行确实进了 HTML」。
	if !strings.Contains(section, "见下表\n第二行") {
		t.Error("事件正文的换行应原样保留")
	}
}

// TestSessionDetailOmitsBlocksWhenNoRender 没有 render 结构的事件不产生任何图表。
//
// 业务工具的调用与结果事件也在时间线上（它们是「模型做了什么」的证据），
// 但只有 ui_render 的结果才该带出图表 —— 判据是 meta.render 有没有，不是事件类型。
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

// TestSessionDetailHiddenWithoutID 不带 id 时不渲染详情区（列表页与详情页共用一个 URL）。
func TestSessionDetailHiddenWithoutID(t *testing.T) {
	body := renderSessionPage(t, sessionPageBase())
	if strings.Contains(body, `id="ai-session-detail"`) {
		t.Error("不带 ?id= 时不该出现详情区")
	}
}
