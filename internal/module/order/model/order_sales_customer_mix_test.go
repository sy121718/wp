package model_test

// order_sales_customer_mix_test.go — 月度趋势的「新客 / 回头客 / 游客」拆分（带库）。
//
// 两条判据，各守一个方向：
//
//  ① **首单子查询不受区间裁剪**：客户的「第一次下单」是全历史的，不是区间内的第一单。
//     把 firsts 子查询也加上 from/to 时，区间外的首单看不见 → 区间内的第一单被当成首单
//     → 老客户被判成新客。测试数据刻意让老客的首单落在**查询区间之前**，
//     所以这条缺陷会直接把「10 月回头客数」从 1 打成 0，不会静默通过。
//
//  ② **三段之和恒等于总量**（new + returning + guest == total）。
//     本仓按 user_id 认客户，游客单 user_id 为空 —— 只拆两段时游客单的销售额
//     就无处可去，两段之和小于总额，而每一段单独看都对。这正是「同一张页面上的
//     两个数字必须同源」要防的形状，所以判据断言等式而不是各段的值。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/public/test/support"
)

func TestMonthlyCustomerMixSplitsNewReturningGuest(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	project := uuid.NewString()
	support.SeedProjectRow(t, db, project, "客户拆分")

	ctx := context.Background()

	// seedOrder 插一张已付款订单 + 一行明细（line_total = cents）。
	//
	// userID 是 `any`：`orders.user_id` 是 **bigint 且可空**，游客单存的不是 0 而是 NULL
	// （0 会变成一个真实存在的客户 id）。用 any 让游客那一处直接传 nil。
	seedOrder := func(orderNo string, userID any, at time.Time, cents int64) {
		t.Helper()
		var orderID string
		if err := db.Raw(`INSERT INTO orders (project_id, order_no, status, user_id, attribution, create_time, update_time)
			VALUES (?, ?, 'paid', ?, '{}'::jsonb, ?, ?) RETURNING id`,
			project, orderNo, userID, at, at).Scan(&orderID).Error; err != nil {
			t.Fatalf("插入订单 %s 失败：%v", orderNo, err)
		}
		if err := db.Exec(`INSERT INTO order_items (order_id, product_id, variant_id, product_name, sku, unit_price, quantity, line_total, create_time)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			orderID, uuid.NewString(), uuid.NewString(), "测试商品", "MIX-SKU", cents, 1, cents, at).Error; err != nil {
			t.Fatalf("插入明细失败：%v", err)
		}
	}

	const (
		userA int64 = 9001
		userB int64 = 9002
	)
	// 老客 A：首单在 9-15（**查询区间之前**），10 月又下了一单。
	seedOrder("MIX-A-1", userA, time.Date(2026, 9, 15, 3, 0, 0, 0, time.UTC), 10000)
	seedOrder("MIX-A-2", userA, time.Date(2026, 10, 10, 3, 0, 0, 0, time.UTC), 20000)
	// 新客 B：首单就在 10 月。
	seedOrder("MIX-B-1", userB, time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC), 30000)
	// 游客单：user_id 为 NULL —— 既不是新客也不是回头客，但钱必须出现在某一段里。
	seedOrder("MIX-G-1", nil, time.Date(2026, 10, 25, 3, 0, 0, 0, time.UTC), 40000)

	m := ordermodel.NewOrderModel(db)
	// 区间从 10-01 开始：A 的首单（9-15）在区间之外 —— 这正是判据 ① 要考的边界。
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	rows, err := m.SalesMonthlyByRange(ctx, project, from, to, ordermodel.OrderSalesFilter{})
	if err != nil {
		t.Fatalf("SalesMonthlyByRange: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("10 月应只回 1 个桶（9 月的单在区间外），实得 %d: %+v", len(rows), rows)
	}
	r := rows[0]
	if got := r.Month.UTC().Format("2006-01"); got != "2026-10" {
		t.Fatalf("桶键 = %q，期望 2026-10", got)
	}

	// 判据 ①：A 的首单在区间之前 → 回头客；B 的首单在 10 月 → 新客；游客单 → guest。
	if r.NewOrderCount != 1 {
		t.Errorf("新客订单数 = %d，期望 1（B 的首单在 10 月；A 不该被算成新客）", r.NewOrderCount)
	}
	if r.ReturningOrderCount != 1 {
		t.Errorf("回头客订单数 = %d，期望 1（A 的首单在 9-15，在查询区间之外 —— 首单子查询被区间裁剪时这里会变 0）", r.ReturningOrderCount)
	}
	if r.GuestOrderCount != 1 {
		t.Errorf("游客订单数 = %d，期望 1", r.GuestOrderCount)
	}
	if r.NewSales != 30000 {
		t.Errorf("新客销售额 = %d，期望 30000", r.NewSales)
	}
	if r.ReturningSales != 20000 {
		t.Errorf("回头客销售额 = %d，期望 20000", r.ReturningSales)
	}
	if r.GuestSales != 40000 {
		t.Errorf("游客销售额 = %d，期望 40000", r.GuestSales)
	}
	if r.NewCustomers != 1 || r.ReturningCustomers != 1 {
		t.Errorf("客户数 = 新 %d / 回头 %d，期望 1 / 1", r.NewCustomers, r.ReturningCustomers)
	}

	// 判据 ②：三段之和恒等于总量。少任何一段（例如只留 new/returning）这里立刻红。
	if got := r.NewSales + r.ReturningSales + r.GuestSales; got != r.Sales {
		t.Errorf("三段销售额之和 = %d，总量 = %d —— 拆开的数字必须能拼回去", got, r.Sales)
	}
	if got := r.NewOrderCount + r.ReturningOrderCount + r.GuestOrderCount; got != r.OrderCount {
		t.Errorf("三段订单数之和 = %d，总量 = %d —— 拆开的数字必须能拼回去", got, r.OrderCount)
	}
}
