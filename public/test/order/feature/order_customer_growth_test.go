package feature

// order_customer_growth_test.go — 区间客户增长（新客 / 复购 / 回头客 / 复购率）。
//
// 钉住四件事：
//
//	1. 新客按**首次下单**归属区间（不看注册时间，本用例里根本没建账号）；
//	2. 复购按**区间内**单数 ≥2 判定，不是历史累计 —— 老客户区间内只下 1 单不算复购；
//	3. 五个计数满足恒等式 NewCustomers + ReturningCustomers == OrderingCustomers；
//	4. 复购率 =（新客复购数 + 老客下单数）/ 区间下单客户数，且分母为 0 时不出现 NaN。

import (
	"context"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
)

// growthUser 一个可区分的客户 id（本用例只需要 id 不同，不建账号）。
func growthUser(n uint64) *uint64 { return &n }

// mkGrowthOrder 落一单并挂到指定客户名下（mkSummaryOrder 的 UserID 固定 9001，不够用）。
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
	// 区间是 2026-09-01 ~ 2026-09-05（rangeTime(n) 落在 9-0n）。
	//
	// A：首单在**区间之前**（8 月）→ 老客；区间内下 2 单 → 也计入复购。
	// B：首单在区间内、区间内 3 单 → 新客 + 复购。
	// C：首单在区间内、区间内 1 单 → 新客，不复购。
	// D：首单在区间之前、区间内 1 单 → 老客，不复购（老客只要下单就算「回来」）。
	mkGrowthOrder(t, m, summaryProjectA, "G-A0", ordermodel.OrderStatusPaid, 1000, rangeTime(-32), growthUser(9101))
	mkGrowthOrder(t, m, summaryProjectA, "G-A1", ordermodel.OrderStatusPaid, 1000, rangeTime(2), growthUser(9101))
	mkGrowthOrder(t, m, summaryProjectA, "G-A2", ordermodel.OrderStatusCompleted, 2000, rangeTime(3), growthUser(9101))

	mkGrowthOrder(t, m, summaryProjectA, "G-B1", ordermodel.OrderStatusPaid, 3000, rangeTime(1), growthUser(9102))
	mkGrowthOrder(t, m, summaryProjectA, "G-B2", ordermodel.OrderStatusShipped, 3000, rangeTime(2), growthUser(9102))
	mkGrowthOrder(t, m, summaryProjectA, "G-B3", ordermodel.OrderStatusPaid, 3000, rangeTime(3), growthUser(9102))

	mkGrowthOrder(t, m, summaryProjectA, "G-C1", ordermodel.OrderStatusPaid, 4000, rangeTime(4), growthUser(9103))
	mkGrowthOrder(t, m, summaryProjectA, "G-D0", ordermodel.OrderStatusPaid, 5000, rangeTime(-40), growthUser(9104))
	mkGrowthOrder(t, m, summaryProjectA, "G-D1", ordermodel.OrderStatusPaid, 5000, rangeTime(5), growthUser(9104))

	// 干扰项：区间内的取消单（不该把这个人算成客户）、游客单（user_id 为空）、别的工程。
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
	// 恒等式：每个人要么首单在区间内、要么在区间之前。
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
	// 复购率 =（新客复购 1 + 老客下单 2）/ 4 = 75.0%
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
	// 一单都没有的区间：五个计数全 0，复购率 0.0% 而**不是** NaN
	//（NaN 会让模板渲染出「NaN%」而且没有任何报错）。
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
	// 缺区间（或工程）一律当场拒 —— 漏传 from 会静默变成「不限起点」，
	// 页面于是显示全站累计的客户数而看起来完全合理。
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
