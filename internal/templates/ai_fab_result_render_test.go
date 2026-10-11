package templates

// ai_fab_result_render_test.go — 悬浮球回答片段（P5 渲染数据通道的消费侧）。
//
// 这个片段**单独渲染**（c.HTML，不经 adminui.Prepare），所以取词函数靠数据注入 ——
// 装配处是 adminui.PrepareBare（见 ai_page.go 的 renderFab）。本文件钉两件事：
//
//	① 槽位收的是**事实**（key + 兜底），文案在渲染期由 tf 取 —— 不是 Go 侧塞成品串；
//	② 漏注入 tf 时是**静默空串**（不是 500、不是报错），所以「注入」这件事只能靠
//	   渲染入口统一，别在调用点各写一份。

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/uispec"
)

// fabFact 与 adminui.Msg 同形（key + 中文兜底 + `%s` 参数）。
//
// 不复用 adminui.Msg：adminui 反向依赖本包（它的 Prepare 注入 templates.TranslateFunc），
// 本包的测试再引 adminui 就是 import cycle。模板按字段名取值，同形即可。
type fabFact struct {
	Key      string
	Fallback string
	Args     []string
}

// fabTF 模拟 PrepareBare 注入的 tf：缺词条回落中文兜底，有参数时按 `%s` 填充。
//
// 真链路的取词与语言解析在 adminui.TranslateArgs（按请求语言查 sys_i18n），
// 本文件只验模板形状，所以这里只把「key 查不到 → 用兜底」这一条简化实现。
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

// renderFabResult 渲染一次片段（带上 PrepareBare 会注入的 tf）。
func renderFabResult(t *testing.T, data map[string]any) string {
	t.Helper()
	if data == nil {
		data = map[string]any{}
	}
	data["tf"] = fabTF
	return renderFabResultRaw(t, data)
}

// renderFabResultRaw 不带 tf 的原样渲染（只给「漏注入」那条用例用）。
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

// TestFabResultEmptyIsExplained 既无回答也无工具事件时要有解释，不能留空白。
//
// 空白与「功能不存在」在页面上长得一样，而这两件事需要用户做的事完全不同。
func TestFabResultEmptyIsExplained(t *testing.T) {
	body := renderFabResult(t, map[string]any{
		"FabEmptyText": fabFact{Key: "ai.fab.empty", Fallback: "没有拿到回答。"},
	})
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
		// Go 侧包好的 include 上下文（见 ai_page.go 的 FabViewsCtx 注释）。
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
//
// 工具次数那句是**两段词条 + 一个数字**：前缀 + 计数 + 后缀，逐字等于原先 Go 侧拼串的产物。
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

// TestFabResultTranslatesAtRenderTime 文案由注入的 tf 在**渲染期**取，不是 Go 侧塞成品串。
//
// 判据是「换掉 tf 的返回值，页面跟着变」：如果模板又去直接输出槽位（`{{.FabError}}`），
// 这条会红 —— 那条路径的后果是 `ai.fab.*` 的词条永远不生效（永远显示中文兜底）。
func TestFabResultTranslatesAtRenderTime(t *testing.T) {
	body := renderFabResultRaw(t, map[string]any{
		"FabError": fabFact{Key: "ai.fab.failed", Fallback: "中文兜底"},
		// 假装当前语言是 en-US：key 查得到，就不该出现中文兜底。
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

// TestFabResultWithoutTFIsSilent 漏注入 tf 时是**静默空串**，不是 500。
//
// 这条把「片段自己取词」的代价写成会变红的检查：渲染不报错、结构完整、日志干净，
// 只有文案整片消失 —— 所以装配必须统一走 adminui.PrepareBare，别在调用点手填。
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

// 空缓冲区变量的存在只为让编译器把 bytes 留住（Render 的签名将来若变会用上）。
var _ = bytes.MinRead
