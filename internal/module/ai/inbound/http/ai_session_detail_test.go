package aihttp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	"go_wp/internal/uispec"
	"go_wp/pkg/utils"
)

// 这一组只测详情装配里那三个纯函数（renderViewsOf / sessionEventRows / sessionEventTone）：
// 它们决定了「库里那段 JSON 会不会变成用户看得见的图表」，而这段判断不依赖库、不依赖模板，
// 用不着起一条完整请求链。

// TestRenderViewsOfRejectsGarbage 坏结构一律返回 nil，绝不 panic。
//
// 库里可能躺着旧版本写下的结构、被手工改过的行、或者将来某个版本换了序列化格式。
// 详情页读到它们只能「这块不渲染」—— 一个坏结构不该让整页 500，
// 也不该让同一轮的其它块跟着消失（每块各自解各自的）。
func TestRenderViewsOfRejectsGarbage(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
	}{
		{"没有 meta", nil},
		{"空 meta", map[string]any{}},
		{"render 不是字符串", map[string]any{"render": 42}},
		{"render 是空串", map[string]any{"render": ""}},
		{"render 不是 JSON", map[string]any{"render": "{"}},
		{"render 是 JSON 但不是数组", map[string]any{"render": `{"type":"table"}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if views := renderViewsOf(tc.meta); views != nil {
				t.Fatalf("坏结构应返回 nil，实得 %+v", views)
			}
		})
	}
}

// TestRenderViewsOfParsesViews render 结构能解回视图（这是页面上图表的唯一来源）。
func TestRenderViewsOfParsesViews(t *testing.T) {
	views := []uispec.View{{Type: uispec.TypeTable, Table: &uispec.TableView{
		Columns: []uispec.Column{{Label: "商品"}},
		Rows:    []uispec.TableRow{{Cells: []uispec.TableCell{{Text: "甲"}}}},
	}}}
	raw, err := json.Marshal(views)
	if err != nil {
		t.Fatal(err)
	}
	got := renderViewsOf(map[string]any{"render": string(raw)})
	if len(got) != 1 || got[0].Type != uispec.TypeTable {
		t.Fatalf("应解出一张表格：%+v", got)
	}
	if got[0].Table == nil || len(got[0].Table.Rows) != 1 {
		t.Fatalf("表格内容不对：%+v", got[0].Table)
	}
}

// TestSessionEventRowsCarriesToolAndPhase 工具名与阶段从 meta 提出来（模板据此显示「哪个工具」）。
//
// 正文里可能只有「工具执行失败」几个字，是哪个工具失败了只有结构化字段说得清；
// 而「调用」与「结果」是同一个 kind 的两条事件，靠 meta.phase 区分。
func TestSessionEventRowsCarriesToolAndPhase(t *testing.T) {
	rows := sessionEventRows([]aidto.SessionEventItem{
		{Seq: 3, Kind: "tool", Content: "已接受", Meta: map[string]any{"tool": "ui_render", "phase": "call"}},
		{Seq: 2, Kind: "user", Content: "今天有多少单"},
		{Seq: 1, Kind: "tool", Content: "失败", Meta: map[string]any{"tool": 7}}, // 类型不对：不崩，留空
	})

	if len(rows) != 3 {
		t.Fatalf("应有三行，实得 %d", len(rows))
	}
	if rows[0]["Tool"] != "ui_render" || rows[0]["Phase"] != "call" {
		t.Errorf("工具名与阶段应从 meta 提出来：%+v", rows[0])
	}
	if rows[1]["Tool"] != "" || rows[1]["Phase"] != "" {
		t.Errorf("没有 meta 时留空：%+v", rows[1])
	}
	if rows[2]["Tool"] != "" {
		t.Errorf("类型不对时留空而不是崩溃：%+v", rows[2])
	}
	// 无 meta 的事件不该带出视图（也不该有 ViewsCtx 里的东西）。
	if rows[1]["HasViews"] != false {
		t.Errorf("无 meta 的事件不该有视图：%+v", rows[1])
	}
}

// TestSessionEventTone 三类事件各有一档配色，未知类型留空（不猜）。
func TestSessionEventTone(t *testing.T) {
	cases := map[string]string{
		"user":            "info",
		"assistant":       "ok",
		"tool":            "warn",
		"compact_summary": "",
		"":                "",
	}
	for kind, want := range cases {
		if got := sessionEventTone(kind); got != want {
			t.Errorf("kind=%q 应得 %q，实得 %q", kind, want, got)
		}
	}
}

// TestSessionTimeLabel 时间格式化在 Go 侧做（Jet 没有日期函数），零值给空串。
func TestSessionTimeLabel(t *testing.T) {
	if got := sessionTimeLabel(utils.JSONTime{}); got != "" {
		t.Errorf("零值应给空串，实得 %q", got)
	}
}

// TestAttachSessionDetailSkipsWithoutID 不带 ?id= 时不查库、不装配（列表页与详情页共用一个 URL）。
func TestAttachSessionDetailSkipsWithoutID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("GET", "/admin/ai/sessions", nil)

	data := gin.H{}
	// svc 为 nil：一旦真去查库就会 panic —— 这条用例正是要钉住「不带 id 时一次库都不碰」。
	h := &SessionPageHandle{}
	h.attachSessionDetail(c, context.Background(), data)

	if len(data) != 0 {
		t.Fatalf("不带 id 时不该往页面数据里塞任何键：%+v", data)
	}
}
