package ordermcp

// order_overview_tools_test.go — 概览聚合三工具的元信息、参数白名单与正文可读性。
//
// 正文断言不是「测文案」：模型的回答完全来自 Text，正文里少了哪个数，用户就看不到
// 哪个数（Data 是给渲染层的，模型不解析它）。所以这里钉的是「关键数字必须出现在 Text 里」。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	orderdto "go_wp/internal/module/order/dto"
	"go_wp/internal/permission"
)

// stubOverview 假 reader：记录入参、回放固定结果。
type stubOverview struct {
	gotDaily  *orderdto.OrderDailySeriesReq
	gotTop    *orderdto.OrderTopProductsReq
	gotCounts *orderdto.OrderStatusCountsReq
	gotItems  *orderdto.OrderSoldQuantityReq

	dailyRes  *orderdto.OrderDailySeriesResp
	topRes    *orderdto.OrderTopProductsResp
	countsRes *orderdto.OrderStatusCountsResp
	itemsRes  *orderdto.OrderSoldQuantityResp
	err       error
}

func (s *stubOverview) DailySeries(_ context.Context, req *orderdto.OrderDailySeriesReq) (*orderdto.OrderDailySeriesResp, error) {
	s.gotDaily = req
	return s.dailyRes, s.err
}

func (s *stubOverview) TopProducts(_ context.Context, req *orderdto.OrderTopProductsReq) (*orderdto.OrderTopProductsResp, error) {
	s.gotTop = req
	return s.topRes, s.err
}

func (s *stubOverview) StatusCounts(_ context.Context, req *orderdto.OrderStatusCountsReq) (*orderdto.OrderStatusCountsResp, error) {
	s.gotCounts = req
	return s.countsRes, s.err
}

// SoldQuantityByRange 属于 ordercontract.OrderOverviewReader，但**没有**对应的 MCP 工具
// （件数是概览页 KPI 的一格，模型问「一共卖了多少件」走的是同一族的其它工具）。
// 它必须在这里实现，否则 *stubOverview 不满足那个接口，整个包编译不过。
func (s *stubOverview) SoldQuantityByRange(_ context.Context, req *orderdto.OrderSoldQuantityReq) (*orderdto.OrderSoldQuantityResp, error) {
	s.gotItems = req
	return s.itemsRes, s.err
}

func mustOverviewTools(t *testing.T, stub *stubOverview) map[string]mcp.Tool {
	t.Helper()
	tools, err := OverviewTools(stub)
	if err != nil {
		t.Fatalf("取概览工具集失败: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("概览聚合应暴露 3 个工具，实得 %d", len(tools))
	}
	byName := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name()] = tool
	}
	return byName
}

func TestOverviewToolsRejectsNilReader(t *testing.T) {
	if _, err := OverviewTools(nil); err == nil {
		t.Fatal("依赖为 nil 应当报错（装配期接线缺陷要在启动时炸掉）")
	}
}

func TestOverviewToolsMetadata(t *testing.T) {
	tools := mustOverviewTools(t, &stubOverview{})

	// 三个工具的权限点都必须复用订单列表：工具的可见性要与「这个人本来就看不看得到订单」一致。
	for _, name := range []string{"orders_daily", "orders_top_products", "orders_status_counts"} {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("缺少工具 %q", name)
		}
		if tool.Permission() != permission.OrderList {
			t.Errorf("%s 的权限 = %q，期望复用 %q", name, tool.Permission(), permission.OrderList)
		}
		if tool.Schema().Type != "object" {
			t.Errorf("%s 的顶层 schema 应是 object，实得 %q", name, tool.Schema().Type)
		}
		if tool.Schema().AdditionalProperties == nil || *tool.Schema().AdditionalProperties {
			t.Errorf("%s 必须声明 additionalProperties=false", name)
		}
	}

	daily := tools["orders_daily"].Schema()
	if len(daily.Required) != 3 || len(daily.Properties) != 3 {
		t.Errorf("趋势工具应恰好三个必填参数，实得 required=%v properties=%d", daily.Required, len(daily.Properties))
	}

	top := tools["orders_top_products"].Schema()
	if _, ok := top.Properties["limit"]; !ok {
		t.Error("榜单工具应有 limit 参数")
	}
	if len(top.Required) != 3 {
		t.Errorf("榜单工具的 limit 应可选（required 只有三项），实得 %v", top.Required)
	}

	counts := tools["orders_status_counts"].Schema()
	if len(counts.Required) != 1 || len(counts.Properties) != 1 {
		t.Errorf("状态计数工具只有 projectId 一个参数（没有时间窗），实得 required=%v properties=%d",
			counts.Required, len(counts.Properties))
	}
}

func TestOrdersDailyInvokePassesArgsAndExplainsText(t *testing.T) {
	stub := &stubOverview{dailyRes: &orderdto.OrderDailySeriesResp{
		ProjectID: "p1", From: "2026-09-01", To: "2026-09-03",
		Points: []orderdto.OrderDailyPointDTO{
			{Day: "2026-09-01", OrderCount: 1, PaidOrderCount: 1, NetSales: 1000, NetSalesLabel: "10.00"},
			{Day: "2026-09-02", OrderCount: 3, PaidOrderCount: 3, NetSales: 5000, NetSalesLabel: "50.00"},
			{Day: "2026-09-03", OrderCount: 0, PaidOrderCount: 0, NetSales: 0, NetSalesLabel: "0.00"},
		},
	}}
	tool := mustOverviewTools(t, stub)["orders_daily"]

	res, err := tool.Invoke(context.Background(),
		json.RawMessage(`{"projectId":"p1","from":"2026-09-01","to":"2026-09-03"}`))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.gotDaily == nil || stub.gotDaily.ProjectID != "p1" {
		t.Fatalf("入参没有传到 reader: %+v", stub.gotDaily)
	}
	for _, want := range []string{"共 3 天", "订单 4 单", "6000 分", "2026-09-02", "逐日"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("正文应含 %q，实得 %q", want, res.Text)
		}
	}
	if _, ok := res.Data.(*orderdto.OrderDailySeriesResp); !ok {
		t.Fatalf("Data 应是 *OrderDailySeriesResp，实得 %T", res.Data)
	}
}

func TestOrdersDailyTextSkipsLongDetail(t *testing.T) {
	// 31 天（超过 dailyDetailDays）：正文给汇总与峰值，但不逐日铺开 —— 否则上下文被明细吃掉。
	points := make([]orderdto.OrderDailyPointDTO, 0, 31)
	for i := 0; i < 31; i++ {
		points = append(points, orderdto.OrderDailyPointDTO{
			Day: fmt.Sprintf("2026-09-%02d", i+1), OrderCount: 1, PaidOrderCount: 1, NetSales: 100,
		})
	}
	text := dailyText(&orderdto.OrderDailySeriesResp{
		From: "2026-09-01", To: "2026-09-30", Points: points,
	})
	if !strings.Contains(text, "共 31 天") || !strings.Contains(text, "订单 31 单") {
		t.Errorf("长区间也应给出汇总，实得 %q", text)
	}
	if strings.Contains(text, "逐日：") {
		t.Errorf("超过 %d 天不该逐日铺开，实得 %q", dailyDetailDays, text)
	}
	if !strings.Contains(text, "峰值日") {
		t.Errorf("长区间更该给出峰值日，实得 %q", text)
	}
}

func TestOrdersTopProductsInvokeExplainsText(t *testing.T) {
	stub := &stubOverview{topRes: &orderdto.OrderTopProductsResp{
		ProjectID: "p1", From: "2026-09-01", To: "2026-09-30", Limit: 2,
		Items: []orderdto.OrderTopProductItemDTO{
			{Rank: 1, ProductID: "g1", ProductName: "TEO 香水 50ml", SKU: "TEO-50-01", Quantity: 5, Amount: 40000, AmountLabel: "400.00"},
			{Rank: 2, ProductID: "g2", ProductName: "NEAFF Eau de Parfum", SKU: "NEA-100-02", Quantity: 3, Amount: 21000, AmountLabel: "210.00"},
		},
	}}
	tool := mustOverviewTools(t, stub)["orders_top_products"]

	res, err := tool.Invoke(context.Background(),
		json.RawMessage(`{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","limit":2}`))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.gotTop == nil || stub.gotTop.Limit != 2 {
		t.Fatalf("limit 没有传到 reader: %+v", stub.gotTop)
	}
	for _, want := range []string{"1) TEO 香水 50ml", "SKU TEO-50-01", "5 件", "400.00", "2) NEAFF"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("正文应含 %q，实得 %q", want, res.Text)
		}
	}
}

func TestOrdersTopProductsTextHandlesEmpty(t *testing.T) {
	// 空榜单要明确说「没有已付款的订单」，而不是回一句「前 5 名：。」——
	// 后者会让模型以为数据拉取失败。
	text := topProductsText(&orderdto.OrderTopProductsResp{From: "2026-09-01", To: "2026-09-30", Limit: 5})
	if !strings.Contains(text, "没有任何已付款的订单") {
		t.Errorf("空榜单的正文应说明原因，实得 %q", text)
	}
}

func TestOrdersStatusCountsInvokeExplainsText(t *testing.T) {
	stub := &stubOverview{countsRes: &orderdto.OrderStatusCountsResp{
		ProjectID: "p1", PendingCount: 2, ShipPendingCount: 1, TotalCount: 5,
		Counts: map[string]int64{"pending": 2, "paid": 1, "completed": 1, "cancelled": 1},
	}}
	tool := mustOverviewTools(t, stub)["orders_status_counts"]

	res, err := tool.Invoke(context.Background(), json.RawMessage(`{"projectId":"p1"}`))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.gotCounts == nil || stub.gotCounts.ProjectID != "p1" {
		t.Fatalf("入参没有传到 reader: %+v", stub.gotCounts)
	}
	for _, want := range []string{"待付款 2 笔", "待发货 1 笔", "共 5 笔"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("正文应含 %q，实得 %q", want, res.Text)
		}
	}
}

func TestOverviewToolsRejectBadArgs(t *testing.T) {
	tools := mustOverviewTools(t, &stubOverview{})
	tests := []struct {
		tool string
		args string
	}{
		{"orders_daily", `{"from":"2026-09-01","to":"2026-09-30"}`},
		{"orders_daily", `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","day":"2026-09-02"}`},
		{"orders_top_products", `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","limit":"3"}`},
		{"orders_status_counts", `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.tool+"/"+tt.args, func(t *testing.T) {
			_, err := tools[tt.tool].Invoke(context.Background(), json.RawMessage(tt.args))
			var argsErr *mcp.ArgsError
			if !errors.As(err, &argsErr) {
				t.Fatalf("应是 *ArgsError（模型可据此改参重试），实得 %T（%v）", err, err)
			}
		})
	}
}

func TestOverviewToolsPropagateServiceError(t *testing.T) {
	wantErr := errors.New("db down")
	tools := mustOverviewTools(t, &stubOverview{err: wantErr})

	for name, args := range map[string]string{
		"orders_daily":         `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30"}`,
		"orders_top_products":  `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30"}`,
		"orders_status_counts": `{"projectId":"p1"}`,
	} {
		_, err := tools[name].Invoke(context.Background(), json.RawMessage(args))
		if !errors.Is(err, wantErr) {
			t.Errorf("%s: service 错误应原样上抛，实得 %v", name, err)
		}
		var argsErr *mcp.ArgsError
		if errors.As(err, &argsErr) {
			t.Errorf("%s: 系统故障不该是 *ArgsError", name)
		}
	}
}
