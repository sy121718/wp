package feature

import (
	"context"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
)

func growthUser(n uint64) *uint64 { return &n }

func mkGrowthOrder(t *testing.T, m *ordermodel.OrderModel, projectID, orderNo, status string, total int64, createdAt time.Time, userID *uint64) {
	t.Helper()
	e := &ordermodel.OrderEntity{
		ProjectID:   projectID,
		OrderNo:     orderNo,
		Status:      status,
		UserID:      userID,
		Total:       total,
		Currency:    "CNY",
		CreateTime:  createdAt,
		UpdateTime:  createdAt,
		Attribution: []byte("{}"),
	}
	if err := m.Create(context.Background(), e); err != nil {
		t.Fatalf("落单 %s 失败: %v", orderNo, err)
	}
}

func TestCustomerGrowthSplitsNewAndReturning(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}

	mkGrowthOrder(t, m, summaryProjectA, "G-A0", ordermodel.OrderStatusPaid, 1000, rangeTime(-32), growthUser(9101))
	mkGrowthOrder(t, m, summaryProjectA, "G-A1", ordermodel.OrderStatusPaid, 1000, rangeTime(2), growthUser(9101))
	mkGrowthOrder(t, m, summaryProjectA, "G-A2", ordermodel.OrderStatusCompleted, 2000, rangeTime(3), growthUser(9101))

	mkGrowthOrder(t, m, summaryProjectA, "G-B1", ordermodel.OrderStatusPaid, 3000, rangeTime(1), growthUser(9102))
	mkGrowthOrder(t, m, summaryProjectA, "G-B2", ordermodel.OrderStatusShipped, 3000, rangeTime(2), growthUser(9102))
	mkGrowthOrder(t, m, summaryProjectA, "G-B3", ordermodel.OrderStatusPaid, 3000, rangeTime(3), growthUser(9102))

	mkGrowthOrder(t, m, summaryProjectA, "G-C1", ordermodel.OrderStatusPaid, 4000, rangeTime(4), growthUser(9103))
	mkGrowthOrder(t, m, summaryProjectA, "G-D0", ordermodel.OrderStatusPaid, 5000, rangeTime(-40), growthUser(9104))
	mkGrowthOrder(t, m, summaryProjectA, "G-D1", ordermodel.OrderStatusPaid, 5000, rangeTime(5), growthUser(9104))

	mkGrowthOrder(t, m, summaryProjectA, "G-E1", ordermodel.OrderStatusCancelled, 9000, rangeTime(2), growthUser(9105))
	mkGrowthOrder(t, m, summaryProjectA, "G-F1", ordermodel.OrderStatusPaid, 9000, rangeTime(2), nil)
	mkGrowthOrder(t, m, "22222222-2222-2222-2222-222222222222", "G-X1", ordermodel.OrderStatusPaid, 9000, rangeTime(2), growthUser(9106))

	res, err := svc.CustomerGrowthByRange(context.Background(), &orderdto.CustomerGrowthReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取客户增长失败: %v", err)
	}
	if res.OrderingCustomers != 4 {
		t.Errorf("下单客户数 = %d，期望 4（A/B/C/D；取消单与游客单不计）", res.OrderingCustomers)
	}
	if res.NewCustomers != 2 {
		t.Errorf("新客 = %d，期望 2（B/C 首单在区间内；A/D 首单在 8 月）", res.NewCustomers)
	}
	if res.ReturningCustomers != 2 {
		t.Errorf("回头客 = %d，期望 2（A/D 首单在区间之前）", res.ReturningCustomers)
	}

	if res.NewCustomers+res.ReturningCustomers != res.OrderingCustomers {
		t.Errorf("新客 %d + 回头客 %d != 下单客户数 %d —— 两个切分不是同一批人",
			res.NewCustomers, res.ReturningCustomers, res.OrderingCustomers)
	}
	if res.Repurchasers != 2 {
		t.Errorf("复购客户 = %d，期望 2（A 两单、B 三单；C/D 各一单）", res.Repurchasers)
	}
	if res.NewRepurchasers != 1 {
		t.Errorf("新客复购 = %d，期望 1（B；C 只下一单）", res.NewRepurchasers)
	}

	if res.RepurchaseRatePct != 75.0 {
		t.Errorf("复购率 = %v%%，期望 75.0（（1+2）/4）", res.RepurchaseRatePct)
	}
	if res.RepurchaseRateLabel != "75.0%" {
		t.Errorf("复购率展示串 = %q，期望 75.0%%", res.RepurchaseRateLabel)
	}
	if res.From != "2026-09-01" || res.To != "2026-09-05" {
		t.Errorf("窗口回显 = %s ~ %s，期望 2026-09-01 ~ 2026-09-05", res.From, res.To)
	}
}

func TestCustomerGrowthEmptyRangeHasZeroRate(t *testing.T) {
	db, _, svc := newRangeFixture(t)
	if db == nil {
		return
	}

	res, err := svc.CustomerGrowthByRange(context.Background(), &orderdto.CustomerGrowthReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取客户增长失败: %v", err)
	}
	if res.OrderingCustomers != 0 || res.NewCustomers != 0 || res.ReturningCustomers != 0 ||
		res.Repurchasers != 0 || res.NewRepurchasers != 0 {
		t.Errorf("空区间的计数应全为 0，实得 %+v", res)
	}
	if res.RepurchaseRatePct != 0 || res.RepurchaseRateLabel != "0.0%" {
		t.Errorf("空区间的复购率应为 0.0%%，实得 %v / %q", res.RepurchaseRatePct, res.RepurchaseRateLabel)
	}
}

func TestCustomerGrowthNeedsExplicitRange(t *testing.T) {
	_, _, svc := newRangeFixture(t)

	for _, req := range []*orderdto.CustomerGrowthReq{
		nil,
		{ProjectID: summaryProjectA},
		{ProjectID: summaryProjectA, From: "2026-09-01"},
		{ProjectID: summaryProjectA, From: "bad", To: "2026-09-05"},
		{From: "2026-09-01", To: "2026-09-05"},
	} {
		if _, err := svc.CustomerGrowthByRange(context.Background(), req); err == nil {
			t.Errorf("期望报错，实得 nil：%+v", req)
		}
	}
}
