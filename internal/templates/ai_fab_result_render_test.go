package templates

// ai_fab_result_render_test.go — 悬浮球回答片段的三种状态（P5 渲染数据通道的消费侧）。
//
// 这个片段是**单独渲染**的（c.HTML，不经 shell.Prepare），所以它不能有任何
// 依赖 i18n 注入的写法 —— 这也是本文件存在的另一半理由：把「片段里不许用 t」
// 从注释变成会变红的检查。

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/uispec"
)

// renderFabResult 渲染一次片段。
func renderFabResult(t *testing.T, data map[string]any) string {
	t.Helper()
	rec := httptest.NewRecorder()
	err := NewJetHTMLRender(".", true).
		Instance("partials/ai_fab_result", data).
		Render(rec)
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if rec.Code != 200 {
		t.Fatalf("状态 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestFabResultRendersAnswer 有回答时渲染回答，且保留换行。
func TestFabResultRendersAnswer(t *testing.T) {
	body := renderFabResult(t, map[string]any{
		"FabAnswer": "这周卖了 12 单。\n净销售额 ¥3,400。",
	})
	if !strings.Contains(body, "这周卖了 12 单。") {
		t.Error("回答正文没渲染出来")
	}
	if !strings.Contains(body, "ai-fab-answer") {
		t.Error("回答要用 pre-wrap 容器（不带容器的话模型给的编号列表会挤成一行）")
	}
}

// TestFabResultRendersError 错误态只渲染错误句，不渲染空回答。
func TestFabResultRendersError(t *testing.T) {
	body := renderFabResult(t, map[string]any{"FabError": "还没有可用的模型。"})
	if !strings.Contains(body, "还没有可用的模型。") {
		t.Error("错误句没渲染出来")
	}
	if strings.Contains(body, "ai-fab-answer") {
		t.Error("错误态不该渲染回答容器 —— 两个容器并存时读的人分不清哪个是真的")
	}
}

// TestFabResultEmptyIsExplained 既无回答也无工具事件时要有解释，不能留空白。
//
// 空白与「功能不存在」在页面上长得一样，而这两件事需要用户做的事完全不同。
func TestFabResultEmptyIsExplained(t *testing.T) {
	body := renderFabResult(t, map[string]any{"FabEmptyText": "没有拿到回答。"})
	if !strings.Contains(body, "没有拿到回答。") {
		t.Error("空态要有解释")
	}
}

// TestFabResultRendersViews 带视图时渲染表格（P5 的 spec → 片段链路）。
func TestFabResultRendersViews(t *testing.T) {
	views := []uispec.View{{
		Type:  uispec.TypeTable,
		Title: "热销商品",
		Table: &uispec.TableView{
			Columns: []uispec.Column{{Label: "商品"}, {Label: "销量"}},
			Rows:    []uispec.TableRow{{Cells: []uispec.TableCell{{Text: "A"}, {Text: "12", Num: true}}}},
		},
	}}
	body := renderFabResult(t, map[string]any{
		"FabAnswer": "见下表",
		// Go 侧包好的 include 上下文（见 ai_fab_handle.go 的 FabViewsCtx 注释）。
		"FabViewsCtx": map[string]any{"Views": views},
	})
	if !strings.Contains(body, "热销商品") || !strings.Contains(body, "A") {
		t.Fatalf("视图没渲染出来：%s", body)
	}
	// 视图片段的 include 契约：它自己 range .Views，所以要传带 Views 键的上下文。
	// 传错的表现是「一片空白但状态 200」—— 这条断言就是钉它的。
	if !strings.Contains(body, "ai-fab-views") {
		t.Error("视图块没落进容器")
	}
}

// TestFabResultNotesAreRendered 截断与工具次数两句提示都要能渲染出来。
func TestFabResultNotesAreRendered(t *testing.T) {
	body := renderFabResult(t, map[string]any{
		"FabAnswer":        "答案",
		"FabTruncatedText": "问题太长，已按前 4000 字截断。",
		"FabToolsText":     "这轮查了数据2 次。",
	})
	for _, want := range []string{"已按前 4000 字截断", "这轮查了数据2 次"} {
		if !strings.Contains(body, want) {
			t.Errorf("缺少提示 %q", want)
		}
	}
}

// 空缓冲区变量的存在只为让编译器把 bytes 留住（Render 的签名将来若变会用上）。
var _ = bytes.MinRead
