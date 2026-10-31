package feature

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

func rangeTime(day int) time.Time {
	return time.Date(2026, 9, day, 12, 0, 0, 0, time.UTC)
}

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

			if res.From != tt.from || res.To != tt.to {
				t.Errorf("生效窗口应回显 %s~%s，实得 %s~%s", tt.from, tt.to, res.From, res.To)
			}
		})
	}
}

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
