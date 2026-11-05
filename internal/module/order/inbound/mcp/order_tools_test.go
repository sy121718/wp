package ordermcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/pkg/mcp"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
)

type stubSummary struct {
	got *orderdto.OrderRangeSummaryReq
	res *orderdto.OrderRangeSummaryResp
	err error
}

func (s *stubSummary) SummaryByRange(_ context.Context, req *orderdto.OrderRangeSummaryReq) (*orderdto.OrderRangeSummaryResp, error) {
	s.got = req
	return s.res, s.err
}

func mustTool(t *testing.T, summary *stubSummary) mcp.Tool {
	t.Helper()
	tools, err := Tools(summary)
	if err != nil {
		t.Fatalf("取工具集失败: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("订单模块应暴露 1 个工具，实得 %d", len(tools))
	}
	return tools[0]
}

func TestToolsRejectsNilReader(t *testing.T) {

	if _, err := Tools(nil); err == nil {
		t.Fatal("依赖为 nil 应当报错")
	}
}

func TestOrdersSummaryMetadata(t *testing.T) {
	tool := mustTool(t, &stubSummary{})

	if tool.Name() != "orders_summary" {
		t.Fatalf("工具名应为 orders_summary，实得 %q", tool.Name())
	}
	if tool.Permission() != orderenums.OrderList {
		t.Fatalf("工具权限应复用订单列表权限点，实得 %q", tool.Permission())
	}

	s := tool.Schema()
	if s.Type != "object" {
		t.Fatalf("顶层 schema 应是 object，实得 %q", s.Type)
	}
	for _, key := range []string{"projectId", "from", "to"} {
		if _, ok := s.Properties[key]; !ok {
			t.Fatalf("schema 缺参数 %q", key)
		}
	}
	if len(s.Required) != 3 {
		t.Fatalf("三个参数都必填，实得 required=%v", s.Required)
	}
	if len(s.Properties) != 3 {
		t.Fatalf("参数白名单应恰好三项，实得多余项: %v", s.Properties)
	}

	if s.AdditionalProperties == nil || *s.AdditionalProperties {
		t.Fatal("schema 必须声明 additionalProperties=false")
	}
}

func TestOrdersSummaryInvokePassesArgsThrough(t *testing.T) {
	stub := &stubSummary{res: &orderdto.OrderRangeSummaryResp{
		ProjectID:      "p1",
		From:           "2026-09-01",
		To:             "2026-09-30",
		OrderCount:     120,
		PaidOrderCount: 118,
		NetSales:       880000,
		NetSalesLabel:  "8800.00",
	}}
	tool := mustTool(t, stub)

	res, err := tool.Invoke(context.Background(),
		json.RawMessage(`{"projectId":"p1","from":"2026-09-01","to":"2026-09-30"}`))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}

	if stub.got == nil {
		t.Fatal("入参没有传到 reader")
	}
	if stub.got.ProjectID != "p1" || stub.got.From != "2026-09-01" || stub.got.To != "2026-09-30" {
		t.Fatalf("入参透传错误: %+v", stub.got)
	}

	for _, want := range []string{"120", "118", "8800.00"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("正文应含 %q，实得 %q", want, res.Text)
		}
	}

	if _, ok := res.Data.(*orderdto.OrderRangeSummaryResp); !ok {
		t.Fatalf("Data 应是 *OrderRangeSummaryResp，实得 %T", res.Data)
	}
}

func TestOrdersSummaryInvokeRejectsBadArgs(t *testing.T) {
	tool := mustTool(t, &stubSummary{})

	tests := []struct {
		name string
		args string
	}{
		{"缺工程 id", `{"from":"2026-09-01","to":"2026-09-30"}`},
		{"缺结束日", `{"projectId":"p1","from":"2026-09-01"}`},
		{"多传维度", `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","status":"paid"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Invoke(context.Background(), json.RawMessage(tt.args))
			var argsErr *mcp.ArgsError
			if !errors.As(err, &argsErr) {
				t.Fatalf("应是 *ArgsError（模型可据此改参重试），实得 %T（%v）", err, err)
			}
		})
	}
}

func TestOrdersSummaryInvokePropagatesServiceError(t *testing.T) {
	wantErr := errors.New("db down")
	tool := mustTool(t, &stubSummary{err: wantErr})

	_, err := tool.Invoke(context.Background(),
		json.RawMessage(`{"projectId":"p1","from":"2026-09-01","to":"2026-09-30"}`))
	if !errors.Is(err, wantErr) {
		t.Fatalf("service 错误应原样上抛，实得 %v", err)
	}

	var argsErr *mcp.ArgsError
	if errors.As(err, &argsErr) {
		t.Fatal("系统故障不应该是 *ArgsError")
	}
}
