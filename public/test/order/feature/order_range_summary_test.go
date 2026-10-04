package feature

// order_range_summary_test.go — 区间订单摘要（概览页 KPI 与只读聚合的取数口）。
//
// 钉住三件事：
//
//	1. 时间窗是**含当天**的半开区间 —— 结束日当天的单必须算进来，次日的不算；
//	2. 销售额只算「钱进来的单」（paidStatuses），且按**净额**（扣掉已实际收货的退款）——
//	   这条与客户页订单摘要的累计消费必须逐字一致（同一个 SQL 表达式常量）；
//	3. 别的工程的单不掺进来（工程作用域）。

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"

	"go_wp/public/test/support"
)

// rangeTime 区间用例的建单时刻：**UTC 正午**。
//
// 不用 summaryTime 那样按 time.Local 建：窗口是按 UTC 日界算的，本地时间的单在
// 靠近日界的时段会落到相邻的 UTC 日，「含当天」这条断言就会变成一条随时区漂移的测试。
func rangeTime(day int) time.Time {
	return time.Date(2026, 9, day, 12, 0, 0, 0, time.UTC)
}

// newRangeFixture 只装配订单三张表用得到的 model 与 service
//（与 order_customer_summary_window_test.go 同一形态：product / stock 等端口传 nil）。
func newRangeFixture(t *testing.T) (*gorm.DB, *ordermodel.OrderModel, *orderservice.Service) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil, nil
	}
	m := ordermodel.NewOrderModel(db)
	svc := orderservice.NewService(
		m,
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),
		ordermodel.NewCouponModel(db),
		ordermodel.NewReturnModel(db),
		nil, nil, nil, nil,
	)
	return db, m, svc
}

// TestOrderRangeSummaryFiltersByWindowAndStatus 区间边界 + 状态口径 + 工程作用域。
func TestOrderRangeSummaryFiltersByWindowAndStatus(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	const otherProject = "22222222-2222-2222-2222-222222222222"
	mkSummaryOrder(t, m, summaryProjectA, "R-PAID", ordermodel.OrderStatusPaid, 3001, 10000, rangeTime(2))
	mkSummaryOrder(t, m, summaryProjectA, "R-SHIPPED", ordermodel.OrderStatusShipped, 3002, 2000, rangeTime(3))
	mkSummaryOrder(t, m, summaryProjectA, "R-CANCELLED", ordermodel.OrderStatusCancelled, 3003, 20000, rangeTime(4))
	mkSummaryOrder(t, m, summaryProjectA, "R-REFUNDED", ordermodel.OrderStatusRefunded, 3004, 30000, rangeTime(5))
	// 干扰项：区间开始之前的一单、以及同期别的工程里的一单。
	mkSummaryOrder(t, m, summaryProjectA, "R-EARLY", ordermodel.OrderStatusPaid, 3005, 5000, rangeTime(1))
	mkSummaryOrder(t, m, otherProject, "R-OTHER", ordermodel.OrderStatusPaid, 3006, 70000, rangeTime(3))

	tests := []struct {
		name           string
		from, to       string
		wantOrders     int64
		wantPaidOrders int64
		wantNetSales   int64
		wantLabel      string
	}{
		{
			name: "整整五天：四单全在窗口内，只有已付款与已发货计入销售额",
			// 取消与退款单算「下了几单」，不算钱进来。
			from: "2026-09-02", to: "2026-09-05",
			wantOrders: 4, wantPaidOrders: 2, wantNetSales: 12000, wantLabel: "120.00",
		},
		{
			name: "单日区间：只算结束日当天那一单（含当天）",
			from: "2026-09-05", to: "2026-09-05",
			wantOrders: 1, wantPaidOrders: 0, wantNetSales: 0, wantLabel: "0.00",
		},
		{
			name: "起始日当天也算进来（含当天的另一半）",
			from: "2026-09-01", to: "2026-09-01",
			wantOrders: 1, wantPaidOrders: 1, wantNetSales: 5000, wantLabel: "50.00",
		},
		{
			name: "窗口内没有单：三个数全零，不是错误",
			from: "2026-09-20", to: "2026-09-25",
			wantOrders: 0, wantPaidOrders: 0, wantNetSales: 0, wantLabel: "0.00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := svc.SummaryByRange(context.Background(), &orderdto.OrderRangeSummaryReq{
				ProjectID: summaryProjectA, From: tt.from, To: tt.to,
			})
			if err != nil {
				t.Fatalf("取区间摘要失败: %v", err)
			}
			if res.OrderCount != tt.wantOrders {
				t.Errorf("订单数应为 %d，实得 %d", tt.wantOrders, res.OrderCount)
			}
			if res.PaidOrderCount != tt.wantPaidOrders {
				t.Errorf("计入消费的订单数应为 %d，实得 %d", tt.wantPaidOrders, res.PaidOrderCount)
			}
			if res.NetSales != tt.wantNetSales {
				t.Errorf("净销售额应为 %d 分，实得 %d", tt.wantNetSales, res.NetSales)
			}
			if res.NetSalesLabel != tt.wantLabel {
				t.Errorf("净销售额展示值应为 %s，实得 %s", tt.wantLabel, res.NetSalesLabel)
			}
			// 回显的是生效窗口（含当天），与请求值在这几条里相同。
			if res.From != tt.from || res.To != tt.to {
				t.Errorf("生效窗口应回显 %s~%s，实得 %s~%s", tt.from, tt.to, res.From, res.To)
			}
		})
	}
}

// TestOrderRangeSummaryNetSalesMatchesCustomerSummary 净额口径与客户页订单摘要**同源**。
//
// 两处算的是同一件事的两种视图（这里按区间、那里按客户）。各写一份表达式的失败模式是
// 「某次顺手改了净额口径之后，概览页的销售额变了、客户页的累计消费没变」——
// 两边都不报错，只在有人对账时才发现。所以这条断言用**一次退货**把两边同时拉动。
func TestOrderRangeSummaryNetSalesMatchesCustomerSummary(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	const userID = uint64(3101)
	mkSummaryOrder(t, m, summaryProjectA, "N-1", ordermodel.OrderStatusPaid, userID, 10000, rangeTime(3))
	head, err := m.GetByNo(context.Background(), summaryProjectA, "N-1")
	if err != nil || head == nil {
		t.Fatalf("取回建好的单失败: %v", err)
	}
	// 一张「已实际收货」的退款 3000：净额应降到 7000（口径见 order_net_total 的注释）。
	if err := db.Exec(
		"INSERT INTO order_returns (project_id, order_id, order_no, return_no, status, refund_amount, user_id, create_time, update_time) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, now(), now())",
		summaryProjectA, head.ID, head.OrderNo, "RT-N-1", ordermodel.ReturnStatusReceived, 3000, userID,
	).Error; err != nil {
		t.Fatalf("落退货单失败: %v", err)
	}

	rangeRes, err := svc.SummaryByRange(context.Background(), &orderdto.OrderRangeSummaryReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-30",
	})
	if err != nil {
		t.Fatalf("取区间摘要失败: %v", err)
	}
	customerRes, err := svc.CustomerOrderSummaryOf(context.Background(), &orderdto.CustomerOrderSummaryReq{
		ProjectID: summaryProjectA, UserID: userID,
	})
	if err != nil {
		t.Fatalf("取客户摘要失败: %v", err)
	}
	if rangeRes.NetSales != 7000 {
		t.Fatalf("净销售额应为 7000 分（10000 − 已收货退款 3000），实得 %d", rangeRes.NetSales)
	}
	if rangeRes.NetSales != customerRes.TotalAmount {
		t.Fatalf("区间净销售额与客户累计消费必须同源：区间 %d / 客户 %d",
			rangeRes.NetSales, customerRes.TotalAmount)
	}
	if rangeRes.PaidOrderCount != 1 {
		t.Fatalf("计入消费的订单数应为 1（退款不改变订单数），实得 %d", rangeRes.PaidOrderCount)
	}
}

// TestOrderRangeSummaryRejectsBadRequest 缺工程 / 非法窗口一律报错，不静默给数字。
func TestOrderRangeSummaryRejectsBadRequest(t *testing.T) {
	db, _, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	tests := []struct {
		name string
		req  *orderdto.OrderRangeSummaryReq
		want string
	}{
		{
			name: "缺工程",
			req:  &orderdto.OrderRangeSummaryReq{From: "2026-09-01", To: "2026-09-30"},
			want: "order.err.projectRequired",
		},
		{
			name: "窗口缺起始",
			req:  &orderdto.OrderRangeSummaryReq{ProjectID: summaryProjectA, From: "", To: "2026-09-30"},
			want: "order.err.invalidParam",
		},
		{
			name: "窗口反向",
			req:  &orderdto.OrderRangeSummaryReq{ProjectID: summaryProjectA, From: "2026-09-30", To: "2026-09-01"},
			want: "order.err.invalidParam",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := svc.SummaryByRange(context.Background(), tt.req)
			if err == nil {
				t.Fatalf("应当报错，实得 %+v", res)
			}
			if err.Error() != tt.want {
				t.Fatalf("错误文案应为 %s，实得 %q", tt.want, err.Error())
			}
		})
	}
}
