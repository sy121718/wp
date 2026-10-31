package templates

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/uispec"
)

type fabFact struct {
	Key      string
	Fallback string
	Args     []string
}

func fabTF(_ string, fallback string, args []string) string {
	if len(args) == 0 {
		return fallback
	}
	vals := make([]any, len(args))
	for i, a := range args {
		vals[i] = a
	}
	return fmt.Sprintf(fallback, vals...)
}

func renderFabResult(t *testing.T, data map[string]any) string {
	t.Helper()
	if data == nil {
		data = map[string]any{}
	}
	data["tf"] = fabTF
	return renderFabResultRaw(t, data)
}

func renderFabResultRaw(t *testing.T, data map[string]any) string {
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

func TestFabResultRendersError(t *testing.T) {
	body := renderFabResult(t, map[string]any{
		"FabError": fabFact{Key: "ai.fab.noModel", Fallback: "还没有可用的模型。"},
	})
	if !strings.Contains(body, "还没有可用的模型。") {
		t.Error("错误句没渲染出来")
	}
	if strings.Contains(body, "ai-fab-answer") {
		t.Error("错误态不该渲染回答容器 —— 两个容器并存时读的人分不清哪个是真的")
	}
}

func TestFabResultEmptyIsExplained(t *testing.T) {
	body := renderFabResult(t, map[string]any{
		"FabEmptyText": fabFact{Key: "ai.fab.empty", Fallback: "没有拿到回答。"},
	})
	if !strings.Contains(body, "没有拿到回答。") {
		t.Error("空态要有解释")
	}
}

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

		"FabViewsCtx": map[string]any{"Views": views},
	})
	if !strings.Contains(body, "热销商品") || !strings.Contains(body, "A") {
		t.Fatalf("视图没渲染出来：%s", body)
	}

	if !strings.Contains(body, "ai-fab-views") {
		t.Error("视图块没落进容器")
	}
}

func TestFabResultNotesAreRendered(t *testing.T) {
	body := renderFabResult(t, map[string]any{
		"FabAnswer":        "答案",
		"FabTruncatedText": fabFact{Key: "ai.fab.truncated", Fallback: "问题太长，已按前 4000 字截断。"},
		"FabToolsPrefix":   fabFact{Key: "ai.fab.tools", Fallback: "这轮查了数据"},
		"FabToolsCount":    "2",
		"FabToolsSuffix":   fabFact{Key: "ai.fab.toolsUnit", Fallback: " 次。"},
	})
	for _, want := range []string{"已按前 4000 字截断", "这轮查了数据2 次"} {
		if !strings.Contains(body, want) {
			t.Errorf("缺少提示 %q", want)
		}
	}
}

func TestFabResultTranslatesAtRenderTime(t *testing.T) {
	body := renderFabResultRaw(t, map[string]any{
		"FabError": fabFact{Key: "ai.fab.failed", Fallback: "中文兜底"},

		"tf": func(key, fallback string, _ []string) string {
			if key == "ai.fab.failed" {
				return "This question did not go through."
			}
			return fallback
		},
	})
	if !strings.Contains(body, "This question did not go through.") {
		t.Errorf("文案没走注入的 tf（取词没回到渲染期）\n%s", body)
	}
	if strings.Contains(body, "中文兜底") {
		t.Errorf("出现了兜底而不是译词\n%s", body)
	}
}

func TestFabResultWithoutTFIsSilent(t *testing.T) {
	body := renderFabResultRaw(t, map[string]any{
		"FabError": fabFact{Key: "ai.fab.failed", Fallback: "这句应该消失"},
	})
	if strings.Contains(body, "这句应该消失") {
		t.Error("没有 tf 也渲染出了文案 —— 说明模板在直接输出槽位")
	}
	if !strings.Contains(body, "ai-fab-error") {
		t.Errorf("结构仍在（这正是它难查的原因）\n%s", body)
	}
}

var _ = bytes.MinRead
