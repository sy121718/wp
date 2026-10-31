package aiservice_test

import (
	"cmp"
	"context"
	json "go_wp/pkg/jsonx"
	"slices"
	"strings"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/uispec"
)

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

func uiRenderSpec() aidto.ToolSpec {
	return aidto.ToolSpec{
		Name:        "ui_render",
		Description: "展示指令",
		Parameters:  []byte(`{"type":"object","properties":{"spec":{"type":"string"}},"required":["spec"]}`),
	}
}

func uiRenderArgs(t *testing.T, specJSON string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"spec": specJSON})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func ordersSpec() aidto.ToolSpec {
	return aidto.ToolSpec{
		Name:        "orders_top_products",
		Description: "热销商品",
		Parameters:  []byte(`{"type":"object","properties":{"limit":{"type":"integer"}}}`),
	}
}

func renderMetaOf(t *testing.T, events []aidto.SessionEventItem) string {
	t.Helper()
	slices.SortFunc(events, func(a, b aidto.SessionEventItem) int { return cmp.Compare(a.Seq, b.Seq) })
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

	var tookData bool
	for _, r := range tools.runs {
		if strings.HasPrefix(r, "orders_top_products ") && strings.Contains(r, `"limit":5`) {
			tookData = true
		}
	}
	if !tookData {
		t.Fatalf("应按积木逐块取数且带 limit，实得调用：%v", tools.runs)
	}

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

func TestUIRenderToolSkipsOversizeRender(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)

	const specJSON = `{"blocks":[{"type":"table","source":"orders_top_products"}]}`
	spec, err := uispec.Parse([]byte(specJSON))
	if err != nil {
		t.Fatalf("spec 应能解析：%v", err)
	}

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
