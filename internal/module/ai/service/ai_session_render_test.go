package aiservice_test

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/uispec"
)

// renderStubProvider 假工具端口：**按工具名**回不同的 Data。
//
// 与 stubToolProvider 的差别就在 Data：展示链路只有「工具交回结构」才走得通，
// 而结构是按工具名区分的（ui_render 交 spec，业务工具交表/列表）。
type renderStubProvider struct {
	specs  []aidto.ToolSpec
	byTool map[string]aiservice.ToolRunResult
	runs   []string
}

func (s *renderStubProvider) Specs() []aidto.ToolSpec { return s.specs }

func (s *renderStubProvider) Run(_ context.Context, _ int64, name, arguments string) (aiservice.ToolRunResult, error) {
	s.runs = append(s.runs, name+" "+arguments)
	if r, ok := s.byTool[name]; ok {
		return r, nil
	}
	return aiservice.ToolRunResult{Text: "没有这个工具", Status: aienums.ToolCallStatusFailed}, nil
}

// uiRenderSpec 一条工具声明（参数形状与真实工具一致：spec 是**一段 JSON 字符串**）。
func uiRenderSpec() aidto.ToolSpec {
	return aidto.ToolSpec{
		Name:        "ui_render",
		Description: "展示指令",
		Parameters:  []byte(`{"type":"object","properties":{"spec":{"type":"string"}},"required":["spec"]}`),
	}
}

// uiRenderArgs 把 spec 原文包成 ui_render 的工具入参。
func uiRenderArgs(t *testing.T, specJSON string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"spec": specJSON})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// ordersSpec 一条业务工具声明（取数用）。
func ordersSpec() aidto.ToolSpec {
	return aidto.ToolSpec{
		Name:        "orders_top_products",
		Description: "热销商品",
		Parameters:  []byte(`{"type":"object","properties":{"limit":{"type":"integer"}}}`),
	}
}

// renderMetaOf 取出结果事件里的 meta.render（没有就回空串）。
func renderMetaOf(t *testing.T, events []aidto.SessionEventItem) string {
	t.Helper()
	sort.Slice(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
	for _, e := range events {
		if e.Meta == nil {
			continue
		}
		if v, ok := e.Meta["render"]; ok {
			s, _ := v.(string)
			return s
		}
	}
	return ""
}

// TestUIRenderToolRendersBlocksIntoMeta 展示链路走通：模型给出 spec → 会话层逐块取数 →
// 视图落进结果事件的 meta.render（页面靠这个键渲染，不进模型上下文）。
func TestUIRenderToolRendersBlocksIntoMeta(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)

	const specJSON = `{"text":"见下表","blocks":[{"type":"table","source":"orders_top_products","limit":5}]}`
	spec, err := uispec.Parse([]byte(specJSON))
	if err != nil {
		t.Fatalf("spec 应能解析：%v", err)
	}

	tools := &renderStubProvider{
		specs: []aidto.ToolSpec{uiRenderSpec(), ordersSpec()},
		byTool: map[string]aiservice.ToolRunResult{
			"ui_render": {
				Text:   "已接受",
				Status: aienums.ToolCallStatusOK,
				Data:   spec,
			},
			"orders_top_products": {
				Text:   "取到 2 行",
				Status: aienums.ToolCallStatusOK,
				Data: uispec.TableData{
					Columns: []uispec.Column{{Label: "商品"}, {Label: "销量", Align: "num"}},
					Rows:    [][]string{{"甲", "3"}, {"乙", "2"}},
				},
			},
		},
	}
	sess.SetToolProvider(tools)

	bodies := sessionChatUpstreamSeq(t, svc,
		responsesFunctionCall("call_1", "ui_render", uiRenderArgs(t, specJSON)),
		`{"object":"response","status":"completed","output_text":"见下表"}`,
	)

	res := sendWithTools(t, sess, "render-key-1")
	_ = waitBody(t, bodies)
	_ = waitBody(t, bodies)

	// ① 逐块取数：ui_render 之后**又调了**业务工具，且把 limit 带下去了。
	var tookData bool
	for _, r := range tools.runs {
		if strings.HasPrefix(r, "orders_top_products ") && strings.Contains(r, `"limit":5`) {
			tookData = true
		}
	}
	if !tookData {
		t.Fatalf("应按积木逐块取数且带 limit，实得调用：%v", tools.runs)
	}

	// ② 视图落进 meta.render。
	render := renderMetaOf(t, listSessionEvents(t, sess, res.Session.ID))
	if render == "" {
		t.Fatal("结果事件应带 meta.render")
	}
	var views []uispec.View
	if err := json.Unmarshal([]byte(render), &views); err != nil {
		t.Fatalf("meta.render 应是视图数组：%v（%s）", err, render)
	}
	if len(views) != 1 || views[0].Type != uispec.TypeTable {
		t.Fatalf("应是一张表格视图：%+v", views)
	}
	if len(views[0].Table.Rows) != 2 || views[0].Table.Rows[0].Cells[0].Text != "甲" {
		t.Fatalf("表格内容不对：%+v", views[0].Table)
	}
}

// TestUIRenderToolSkipsOversizeRender 视图超过落库上限时**整份不写**（不截断）。
//
// 半个 JSON 反序列化必然失败，页面会得到一张永远渲不出来的表；
// 宁可不渲（页面退化成一条普通消息），也不要写一份坏数据。
func TestUIRenderToolSkipsOversizeRender(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)

	const specJSON = `{"blocks":[{"type":"table","source":"orders_top_products"}]}`
	spec, err := uispec.Parse([]byte(specJSON))
	if err != nil {
		t.Fatalf("spec 应能解析：%v", err)
	}

	// 造一份远超 64KB 的表（2000 行 × 80 字符 ≈ 160KB）。
	rows := make([][]string, 0, 2000)
	for i := 0; i < 2000; i++ {
		rows = append(rows, []string{strings.Repeat("x", 80)})
	}

	tools := &renderStubProvider{
		specs: []aidto.ToolSpec{uiRenderSpec(), ordersSpec()},
		byTool: map[string]aiservice.ToolRunResult{
			"ui_render":           {Text: "已接受", Status: aienums.ToolCallStatusOK, Data: spec},
			"orders_top_products": {Text: "取到 2000 行", Status: aienums.ToolCallStatusOK, Data: uispec.TableData{Columns: []uispec.Column{{Label: "商品"}}, Rows: rows}},
		},
	}
	sess.SetToolProvider(tools)

	bodies := sessionChatUpstreamSeq(t, svc,
		responsesFunctionCall("call_1", "ui_render", uiRenderArgs(t, specJSON)),
		`{"object":"response","status":"completed","output_text":"见下表"}`,
	)

	res := sendWithTools(t, sess, "render-key-2")
	_ = waitBody(t, bodies)
	_ = waitBody(t, bodies)

	if render := renderMetaOf(t, listSessionEvents(t, sess, res.Session.ID)); render != "" {
		t.Fatalf("超限时不该写 meta.render（实得 %d 字符）", len(render))
	}
}

// TestUIRenderToolWithoutDataSkipsRender 工具交回的 Data 不是 spec 时**不渲染**。
//
// 这道判据挡的是「哪个工具都能顺手往 meta.render 里塞东西」：只有展示指令才该产生视图，
// 业务工具的结构是给渲染器吃的原料，不是给页面看的成品。
func TestUIRenderToolWithoutDataSkipsRender(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)

	tools := &renderStubProvider{
		specs: []aidto.ToolSpec{ordersSpec()},
		byTool: map[string]aiservice.ToolRunResult{
			"orders_top_products": {
				Text:   "取到 2 行",
				Status: aienums.ToolCallStatusOK,
				Data:   uispec.TableData{Columns: []uispec.Column{{Label: "商品"}}, Rows: [][]string{{"甲"}}},
			},
		},
	}
	sess.SetToolProvider(tools)

	bodies := sessionChatUpstreamSeq(t, svc,
		responsesFunctionCall("call_1", "orders_top_products", `{"limit":5}`),
		`{"object":"response","status":"completed","output_text":"有 2 个商品"}`,
	)

	res := sendWithTools(t, sess, "render-key-3")
	_ = waitBody(t, bodies)
	_ = waitBody(t, bodies)

	if render := renderMetaOf(t, listSessionEvents(t, sess, res.Session.ID)); render != "" {
		t.Fatalf("业务工具的结果不该变成视图：%s", render)
	}
}
