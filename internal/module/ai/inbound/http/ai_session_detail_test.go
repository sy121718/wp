package aihttp

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	"go_wp/internal/uispec"
	"go_wp/pkg/utils"
)

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

func TestSessionEventRowsCarriesToolAndPhase(t *testing.T) {
	rows := sessionEventRows([]aidto.SessionEventItem{
		{Seq: 3, Kind: "tool", Content: "已接受", Meta: map[string]any{"tool": "ui_render", "phase": "call"}},
		{Seq: 2, Kind: "user", Content: "今天有多少单"},
		{Seq: 1, Kind: "tool", Content: "失败", Meta: map[string]any{"tool": 7}},
	})

	if len(rows) != 3 {
		t.Fatalf("应有三行，实得 %d", len(rows))
	}
	if rows[0].Tool != "ui_render" || rows[0].Phase != "call" {
		t.Errorf("工具名与阶段应从 meta 提出来：%+v", rows[0])
	}
	if rows[1].Tool != "" || rows[1].Phase != "" {
		t.Errorf("没有 meta 时留空：%+v", rows[1])
	}
	if rows[2].Tool != "" {
		t.Errorf("类型不对时留空而不是崩溃：%+v", rows[2])
	}

	if rows[1].HasViews != false {
		t.Errorf("无 meta 的事件不该有视图：%+v", rows[1])
	}
}

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

func TestSessionTimeLabel(t *testing.T) {
	if got := sessionTimeLabel(utils.JSONTime{}); got != "" {
		t.Errorf("零值应给空串，实得 %q", got)
	}
}

func TestAttachSessionDetailSkipsWithoutID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("GET", "/admin/ai/sessions", nil)

	view := &aidto.SessionListPageResp{}

	h := &SessionPageHandle{}
	h.attachSessionDetail(c, context.Background(), view)

	if view.Detail != nil || view.DetailID != 0 || view.DetailErr != "" ||
		len(view.Events) != 0 || view.EventsErr != "" || view.FoldPlan != nil || view.FoldPlanErr != "" {
		t.Fatalf("不带 id 时不该装配详情区块：%+v", view)
	}
}
