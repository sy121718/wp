package ordermcp

// order_tools_test.go — 工具元信息（权限、参数白名单）与调用路径。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	orderdto "go_wp/internal/module/order/dto"
	"go_wp/internal/permission"
)

// stubSummary 假 reader：记录入参、回放固定结果。
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
	// 装配期接线错误要当场报错：静默返回空工具集的话，表现是「AI 就是不知道有订单数据」，
	// 而这句话在日志里没有任何痕迹。
	if _, err := Tools(nil); err == nil {
		t.Fatal("依赖为 nil 应当报错")
	}
}

func TestOrdersSummaryMetadata(t *testing.T) {
	tool := mustTool(t, &stubSummary{})

	if tool.Name() != "orders_summary" {
		t.Fatalf("工具名应为 orders_summary，实得 %q", tool.Name())
	}
	if tool.Permission() != permission.OrderList {
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
	// 边界：schema 自己必须声明不接受额外字段 —— 校验函数会拒，但 schema 是
	// 交给模型看的「可填什么」，两侧口径不一致时模型会以为自己能传别的维度。
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
	// 正文要带上人读的数字：模型据此作答，不该让它自己去解析 Data。
	for _, want := range []string{"120", "118", "8800.00"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("正文应含 %q，实得 %q", want, res.Text)
		}
	}
	// Data 带上原始响应：渲染层直接用，避免正则解析中文。
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
	// 系统故障不能伪装成参数错误：否则模型会把「库挂了」当成「我参数写错了」，
	// 一遍遍重试同一个调用而不是告诉用户。
	var argsErr *mcp.ArgsError
	if errors.As(err, &argsErr) {
		t.Fatal("系统故障不应该是 *ArgsError")
	}
}
