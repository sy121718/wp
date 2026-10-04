package feature

// order_overview_test.go — 概览页三块只读聚合（按天趋势 / 热销榜 / 状态计数）。
//
// 钉住四件事：
//
//	1. 按天趋势是**逐日连续**的（没有订单的那天补 0），且日期升序；
//	2. 逐日相加必须等于同区间的汇总（DailySeries 与 SummaryByRange 共用一个口径 ——
//	   两处一旦分叉，柱图加起来对不上 KPI，而两边都不报错）；
//	3. 榜单只算「钱进来的单」，排序与名次由同一条 ORDER BY 决定，未付款/取消的商品不上榜；
//	4. 「待处理 / 待发货」的口径由 service 解释一次（页面与 AI 不各自拼状态名）。

import (
	"context"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
)

// mkOverviewOrder 落一单并返回它的自增 id（mkSummaryOrder 不回 id，而订单项要挂上去）。
//
// 不改 mkSummaryOrder 的签名去加返回值：它已被区间摘要与客户摘要两处调用，
// 改签名会波及那些测试文件（同包私有函数的签名变更从来不是「只改一个文件」）。
func mkOverviewOrder(t *testing.T, m *ordermodel.OrderModel, projectID, orderNo, status string, total int64, createdAt time.Time) uint64 {
	t.Helper()
	uid := uint64(9001)
	e := &ordermodel.OrderEntity{
		ProjectID:   projectID,
		OrderNo:     orderNo,
		Status:      status,
		UserID:      &uid,
		Total:       total,
		Currency:    "CNY",
		CreateTime:  createdAt,
		UpdateTime:  createdAt,
		Attribution: []byte("{}"),
	}
	if err := m.Create(context.Background(), e); err != nil {
		t.Fatalf("落单 %s 失败: %v", orderNo, err)
	}
	return e.ID
}

// mkOverviewItem 落一条订单项（商品名与 SKU 是下单时刻的快照，与生产写入一致）。
func mkOverviewItem(t *testing.T, im *ordermodel.OrderItemModel, orderID uint64, productID, name, sku string, qty int, lineTotal int64) {
	t.Helper()
	items := []*ordermodel.OrderItemEntity{{
		OrderID:      orderID,
		ProductID:    productID,
		VariantID:    "66666666-6666-6666-6666-666666666666",
		ProductName:  name,
		VariantLabel: "默认",
		SKU:          sku,
		UnitPrice:    lineTotal / int64(qty),
		Quantity:     qty,
		LineSubtotal: lineTotal,
		LineTotal:    lineTotal,
		CreateTime:   time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
	}}
	if err := im.CreateBatch(context.Background(), items); err != nil {
		t.Fatalf("落订单项 %s 失败: %v", name, err)
	}
}

// TestOrderDailySeriesFillsMissingDays 逐日连续 + 补零 + 工程作用域。
func TestOrderDailySeriesFillsMissingDays(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	const otherProject = "22222222-2222-2222-2222-222222222222"
	// 9-02 两单（一单已付款、一单取消），9-04 一单已发货；9-01 / 9-03 / 9-05 没有订单。
	mkSummaryOrder(t, m, summaryProjectA, "D-PAID", ordermodel.OrderStatusPaid, 9101, 1000, rangeTime(2))
	mkSummaryOrder(t, m, summaryProjectA, "D-CANCEL", ordermodel.OrderStatusCancelled, 9102, 500, rangeTime(2))
	mkSummaryOrder(t, m, summaryProjectA, "D-SHIP", ordermodel.OrderStatusShipped, 9103, 2000, rangeTime(4))
	// 干扰项：别的工程同期的单。
	mkSummaryOrder(t, m, otherProject, "D-OTHER", ordermodel.OrderStatusPaid, 9104, 9999, rangeTime(3))

	res, err := svc.DailySeries(context.Background(), &orderdto.OrderDailySeriesReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取按天趋势失败: %v", err)
	}
	if len(res.Points) != 5 {
		t.Fatalf("点数应为 5（含首尾），实得 %d: %+v", len(res.Points), res.Points)
	}
	want := []struct {
		day        string
		orders     int64
		paidOrders int64
		netSales   int64
	}{
		{"2026-09-01", 0, 0, 0},
		{"2026-09-02", 2, 1, 1000},
		{"2026-09-03", 0, 0, 0},
		{"2026-09-04", 1, 1, 2000},
		{"2026-09-05", 0, 0, 0},
	}
	for i, w := range want {
		got := res.Points[i]
		if got.Day != w.day || got.OrderCount != w.orders || got.PaidOrderCount != w.paidOrders || got.NetSales != w.netSales {
			t.Errorf("第 %d 点 = %+v，期望 day=%s orders=%d paid=%d net=%d",
				i, got, w.day, w.orders, w.paidOrders, w.netSales)
		}
	}
	if res.From != "2026-09-01" || res.To != "2026-09-05" {
		t.Errorf("回显窗口 = %s..%s，期望 2026-09-01..2026-09-05", res.From, res.To)
	}
}

// TestOrderDailySeriesSumsToOneRangeSummary 逐日相加 == 区间汇总（两个接口同口径）。
func TestOrderDailySeriesSumsToOneRangeSummary(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	mkSummaryOrder(t, m, summaryProjectA, "S-PAID", ordermodel.OrderStatusPaid, 9201, 3000, rangeTime(2))
	mkSummaryOrder(t, m, summaryProjectA, "S-COMPLETED", ordermodel.OrderStatusCompleted, 9202, 4000, rangeTime(3))
	mkSummaryOrder(t, m, summaryProjectA, "S-REFUNDED", ordermodel.OrderStatusRefunded, 9203, 5000, rangeTime(4))
	// 同一时刻的分界外干扰项：结束日次日。
	mkSummaryOrder(t, m, summaryProjectA, "S-AFTER", ordermodel.OrderStatusPaid, 9204, 8888, rangeTime(6))

	ctx := context.Background()
	series, err := svc.DailySeries(ctx, &orderdto.OrderDailySeriesReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取按天趋势失败: %v", err)
	}
	summary, err := svc.SummaryByRange(ctx, &orderdto.OrderRangeSummaryReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取区间摘要失败: %v", err)
	}
	var orders, paidOrders, netSales int64
	for _, p := range series.Points {
		orders += p.OrderCount
		paidOrders += p.PaidOrderCount
		netSales += p.NetSales
	}
	if orders != summary.OrderCount || paidOrders != summary.PaidOrderCount || netSales != summary.NetSales {
		t.Errorf("逐日相加 = (%d, %d, %d)，区间汇总 = (%d, %d, %d) —— 两个接口的口径分叉了",
			orders, paidOrders, netSales, summary.OrderCount, summary.PaidOrderCount, summary.NetSales)
	}
}

// TestOrderTopProductsRanksAndIgnoresUnpaidOrders 排序、名次、只算付款单、limit。
func TestOrderTopProductsRanksAndIgnoresUnpaidOrders(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	im := ordermodel.NewOrderItemModel(db)
	const productA = "11111111-1111-1111-1111-111111111111"
	const productB = "22222222-2222-2222-2222-222222222222"
	const productC = "33333333-3333-3333-3333-333333333333"
	const productD = "44444444-4444-4444-4444-444444444444"

	// A：两单共 5 件；B：一单 3 件；C：取消单 10 件（不该上榜）；D：待付款 20 件（不该上榜）。
	o1 := mkOverviewOrder(t, m, summaryProjectA, "T-A1", ordermodel.OrderStatusPaid, 3000, rangeTime(2))
	mkOverviewItem(t, im, o1, productA, "TEO 香水 50ml", "TEO-50-01", 3, 24000)
	o2 := mkOverviewOrder(t, m, summaryProjectA, "T-A2", ordermodel.OrderStatusCompleted, 4000, rangeTime(3))
	mkOverviewItem(t, im, o2, productA, "TEO 香水 50ml", "TEO-50-01", 2, 16000)
	o3 := mkOverviewOrder(t, m, summaryProjectA, "T-B1", ordermodel.OrderStatusShipped, 5000, rangeTime(3))
	mkOverviewItem(t, im, o3, productB, "NEAFF Eau de Parfum", "NEA-100-02", 3, 21000)
	o4 := mkOverviewOrder(t, m, summaryProjectA, "T-C1", ordermodel.OrderStatusCancelled, 9000, rangeTime(4))
	mkOverviewItem(t, im, o4, productC, "取消的商品", "CAN-01", 10, 100000)
	o5 := mkOverviewOrder(t, m, summaryProjectA, "T-D1", ordermodel.OrderStatusPending, 8000, rangeTime(4))
	mkOverviewItem(t, im, o5, productD, "还没付款的商品", "PEN-01", 20, 200000)

	res, err := svc.TopProducts(context.Background(), &orderdto.OrderTopProductsReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Limit: 5,
	})
	if err != nil {
		t.Fatalf("取热销榜失败: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("上榜商品应为 2 个（取消与待付款不上榜），实得 %d: %+v", len(res.Items), res.Items)
	}
	first, second := res.Items[0], res.Items[1]
	if first.Rank != 1 || first.ProductID != productA || first.Quantity != 5 || first.Amount != 40000 {
		t.Errorf("第一名 = %+v，期望 A / 5 件 / 40000 分", first)
	}
	if second.Rank != 2 || second.ProductID != productB || second.Quantity != 3 {
		t.Errorf("第二名 = %+v，期望 B / 3 件", second)
	}
	if first.ProductName != "TEO 香水 50ml" || first.SKU != "TEO-50-01" {
		t.Errorf("商品名与 SKU 应取订单行上的快照，实得 %q / %q", first.ProductName, first.SKU)
	}

	// limit 生效：只要 1 条时只回第一名，且名次仍是 1。
	limited, err := svc.TopProducts(context.Background(), &orderdto.OrderTopProductsReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Limit: 1,
	})
	if err != nil {
		t.Fatalf("取热销榜(limit=1)失败: %v", err)
	}
	if len(limited.Items) != 1 || limited.Items[0].ProductID != productA || limited.Limit != 1 {
		t.Errorf("limit=1 时 = %+v（Limit 字段=%d），期望只回 A", limited.Items, limited.Limit)
	}
}

// TestOrderStatusCountsExplainsPendingAndShipPending 状态计数与「待处理 / 待发货」口径。
func TestOrderStatusCountsExplainsPendingAndShipPending(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	mkSummaryOrder(t, m, summaryProjectA, "C-P1", ordermodel.OrderStatusPending, 9301, 1000, rangeTime(2))
	mkSummaryOrder(t, m, summaryProjectA, "C-P2", ordermodel.OrderStatusPending, 9302, 1000, rangeTime(2))
	mkSummaryOrder(t, m, summaryProjectA, "C-PAID", ordermodel.OrderStatusPaid, 9303, 1000, rangeTime(3))
	mkSummaryOrder(t, m, summaryProjectA, "C-DONE", ordermodel.OrderStatusCompleted, 9304, 1000, rangeTime(3))
	mkSummaryOrder(t, m, summaryProjectA, "C-CANCEL", ordermodel.OrderStatusCancelled, 9305, 1000, rangeTime(4))

	res, err := svc.StatusCounts(context.Background(), &orderdto.OrderStatusCountsReq{ProjectID: summaryProjectA})
	if err != nil {
		t.Fatalf("取状态计数失败: %v", err)
	}
	if res.PendingCount != 2 {
		t.Errorf("待付款 = %d，期望 2", res.PendingCount)
	}
	if res.ShipPendingCount != 1 {
		t.Errorf("待发货 = %d，期望 1", res.ShipPendingCount)
	}
	if res.TotalCount != 5 {
		t.Errorf("总数 = %d，期望 5", res.TotalCount)
	}
	if res.Counts[ordermodel.OrderStatusCancelled] != 1 {
		t.Errorf("取消单计数 = %d，期望 1（原始状态要原样给出）", res.Counts[ordermodel.OrderStatusCancelled])
	}
}

// TestOrderOverviewRejectsBadRequest 三个只读聚合的参数校验（缺工程 / 缺区间）。
func TestOrderOverviewRejectsBadRequest(t *testing.T) {
	db, _, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "趋势缺工程",
			call: func() error {
				_, err := svc.DailySeries(ctx, &orderdto.OrderDailySeriesReq{From: "2026-09-01", To: "2026-09-05"})
				return err
			},
			want: "order.err.projectRequired",
		},
		{
			name: "趋势缺起始日",
			call: func() error {
				_, err := svc.DailySeries(ctx, &orderdto.OrderDailySeriesReq{ProjectID: summaryProjectA, To: "2026-09-05"})
				return err
			},
			want: "order.err.invalidParam",
		},
		{
			name: "榜单缺工程",
			call: func() error {
				_, err := svc.TopProducts(ctx, &orderdto.OrderTopProductsReq{From: "2026-09-01", To: "2026-09-05"})
				return err
			},
			want: "order.err.projectRequired",
		},
		{
			name: "状态计数缺工程",
			call: func() error {
				_, err := svc.StatusCounts(ctx, &orderdto.OrderStatusCountsReq{})
				return err
			},
			want: "order.err.projectRequired",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatalf("期望报错 %s，实得 nil", tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("错误 = %q，期望 %q", err.Error(), tc.want)
			}
		})
	}
}

// TestOrderSoldQuantitySharesScopeWithTopProducts 商品销售总量与热销榜**同口径**。
//
// 这一条是这一批的核心判据：件数（KPI 卡）与榜单（排行榜）摆在同一个页面上，
// 两张卡的数字必须自洽 —— 榜单里几个商品的销量加起来就该是总量。两条 SQL 各写一份
// WHERE 的失败模式是「榜单排除了取消单、总量忘了排除」，两个数字互相矛盾而每一处单独看都对。
func TestOrderSoldQuantitySharesScopeWithTopProducts(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	im := ordermodel.NewOrderItemModel(db)
	const productA = "11111111-1111-1111-1111-111111111111"
	const productB = "22222222-2222-2222-2222-222222222222"
	const productC = "33333333-3333-3333-3333-333333333333"
	const productD = "44444444-4444-4444-4444-444444444444"

	// 与热销榜用例同一组数据：A 两单共 5 件、B 一单 3 件，
	// C 取消单 10 件、D 待付款 20 件（两者都不该计入）。
	o1 := mkOverviewOrder(t, m, summaryProjectA, "Q-A1", ordermodel.OrderStatusPaid, 3100, rangeTime(2))
	mkOverviewItem(t, im, o1, productA, "TEO 香水 50ml", "TEO-50-01", 3, 24000)
	o2 := mkOverviewOrder(t, m, summaryProjectA, "Q-A2", ordermodel.OrderStatusCompleted, 3200, rangeTime(3))
	mkOverviewItem(t, im, o2, productA, "TEO 香水 50ml", "TEO-50-01", 2, 16000)
	o3 := mkOverviewOrder(t, m, summaryProjectA, "Q-B1", ordermodel.OrderStatusShipped, 3300, rangeTime(3))
	mkOverviewItem(t, im, o3, productB, "NEAFF Eau de Parfum", "NEA-100-02", 3, 21000)
	o4 := mkOverviewOrder(t, m, summaryProjectA, "Q-C1", ordermodel.OrderStatusCancelled, 3400, rangeTime(4))
	mkOverviewItem(t, im, o4, productC, "取消的商品", "CAN-01", 10, 100000)
	o5 := mkOverviewOrder(t, m, summaryProjectA, "Q-D1", ordermodel.OrderStatusPending, 3500, rangeTime(4))
	mkOverviewItem(t, im, o5, productD, "还没付款的商品", "PEN-01", 20, 200000)
	// 干扰项：同工程但区间之外（次日）的单；别的工程的单。
	o6 := mkOverviewOrder(t, m, summaryProjectA, "Q-AFTER", ordermodel.OrderStatusPaid, 3600, rangeTime(6))
	mkOverviewItem(t, im, o6, productA, "TEO 香水 50ml", "TEO-50-01", 7, 56000)
	const otherProject = "22222222-2222-2222-2222-222222222222"
	o7 := mkOverviewOrder(t, m, otherProject, "Q-OTHER", ordermodel.OrderStatusPaid, 3700, rangeTime(3))
	mkOverviewItem(t, im, o7, productA, "TEO 香水 50ml", "TEO-50-01", 9, 72000)

	res, err := svc.SoldQuantityByRange(context.Background(), &orderdto.OrderSoldQuantityReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取商品销售总量失败: %v", err)
	}
	if res.Quantity != 8 {
		t.Errorf("件数 = %d，期望 8（A 的 5 件 + B 的 3 件；取消与待付款不计）", res.Quantity)
	}
	if res.OrderCount != 3 {
		t.Errorf("贡献订单数 = %d，期望 3（A 两单 + B 一单）", res.OrderCount)
	}
	// 回显生效窗口：页面标题要写「哪一段」，写请求值可能在收敛后变成一句错的说明。
	if res.From != "2026-09-01" || res.To != "2026-09-05" {
		t.Errorf("窗口回显 = %s ~ %s，期望 2026-09-01 ~ 2026-09-05", res.From, res.To)
	}

	// **自洽**：榜单里各商品的销量之和 == 总量。
	top, err := svc.TopProducts(context.Background(), &orderdto.OrderTopProductsReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Limit: 50,
	})
	if err != nil {
		t.Fatalf("取热销榜失败: %v", err)
	}
	var sum int64
	for _, it := range top.Items {
		sum += it.Quantity
	}
	if sum != res.Quantity {
		t.Errorf("榜单销量之和 = %d，总量 = %d —— 两处口径分叉了（同一张页面上的两个数字互相矛盾）", sum, res.Quantity)
	}
}

// 编译期断言：service 仍满足 contract 的两个只读聚合接口（装配处靠它接线）。
var (
	_ interface {
		DailySeries(context.Context, *orderdto.OrderDailySeriesReq) (*orderdto.OrderDailySeriesResp, error)
		TopProducts(context.Context, *orderdto.OrderTopProductsReq) (*orderdto.OrderTopProductsResp, error)
		StatusCounts(context.Context, *orderdto.OrderStatusCountsReq) (*orderdto.OrderStatusCountsResp, error)
		SoldQuantityByRange(context.Context, *orderdto.OrderSoldQuantityReq) (*orderdto.OrderSoldQuantityResp, error)
	} = (*orderservice.Service)(nil)
)
