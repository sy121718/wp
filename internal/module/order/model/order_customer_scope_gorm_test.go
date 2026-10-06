package model_test

// order_customer_scope_gorm_test.go — 客户口径四个查询的带库判据。
//
// 为什么必须有这一批：orderCustomerCTEs / orderCohortCTEs / rfmScoredCTE 从 CTE 改写成
// GORM 子查询（`Table("(?) AS x", sub)`）时，最容易错的不是 SQL 语义而是**参数顺序**
// —— 多包一层子查询就多一层绑定，而参数错位**不报错**，只是数字全错（把状态名单当成
// project_id 时查询照样返回 0 行，页面显示「这段时间一个客户都没有」）。
// 读代码看不出这类错，只有带库跑一遍。
//
// 造数（project P，区间 = [2026-10-01, 2026-11-01)）：
//   - A(101) 首单 10-01，区间内 2 单  → 新客 + 复购
//   - B(102) 首单 08-01，区间内 1 单  → 回头客
//   - C(103) 只在 08-15 下过 1 单     → 本区间不出现
//   - D(104) 区间内 1 单但已取消      → 不计入（取消不是消费）
//
// 期望因此是一组**互相约束**的数：ordering=2 / new=1 / returning=1 / repurchasers=1。
// 任何一个口径写错，这四个数里至少有一个会不成立。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/order/model"
	"go_wp/public/test/support"
)

// insertCustomerOrder 插一条挂客户的订单（只用到 orders 一张表：
// 四个客户口径查询都不碰 order_items）。
func insertCustomerOrder(t *testing.T, db *gorm.DB, projectID string, userID int64, status string, createdAt time.Time) {
	t.Helper()
	err := db.Exec(`INSERT INTO orders (project_id, order_no, status, user_id, total, attribution, create_time, update_time)
		VALUES (?, ?, ?, ?, 10000, '{}'::jsonb, ?, ?)`,
		projectID, "T"+uuid.NewString()[:12], status, userID, createdAt, createdAt).Error
	if err != nil {
		t.Fatalf("插入订单失败: %v", err)
	}
}

func TestCustomerScopeQueriesAfterGormRewrite(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "客户口径测试工程")

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 10, 0, 0, 0, time.UTC) }
	ago := func(month, d int) time.Time { return time.Date(2026, time.Month(month), d, 10, 0, 0, 0, time.UTC) }

	insertCustomerOrder(t, db, projectID, 101, "paid", day(1))
	insertCustomerOrder(t, db, projectID, 101, "completed", day(3))
	insertCustomerOrder(t, db, projectID, 102, "paid", day(2))
	insertCustomerOrder(t, db, projectID, 102, "paid", ago(8, 1)) // 首单在区间前
	insertCustomerOrder(t, db, projectID, 103, "paid", ago(8, 15))
	insertCustomerOrder(t, db, projectID, 104, "cancelled", day(4))

	m := model.NewOrderModel(db)
	ctx := context.Background()

	t.Run("客户增长五数", func(t *testing.T) {
		row, err := m.CustomerGrowthByRange(ctx, projectID, from, to)
		if err != nil {
			t.Fatalf("CustomerGrowthByRange: %v", err)
		}
		if row.OrderingCustomers != 2 {
			t.Errorf("ordering_customers = %d，期望 2", row.OrderingCustomers)
		}
		if row.NewCustomers != 1 {
			t.Errorf("new_customers = %d，期望 1", row.NewCustomers)
		}
		if row.ReturningCustomers != 1 {
			t.Errorf("returning_customers = %d，期望 1（B 首单在区间前）", row.ReturningCustomers)
		}
		if row.Repurchasers != 1 {
			t.Errorf("repurchasers = %d，期望 1（A 区间内两单）", row.Repurchasers)
		}
		// **new_repurchasers 的名字与它的实际口径不符**（既有问题，本批重构不改行为）：
		// 表达式是 `first_at < ? AND order_count >= 2` = **老客**里复购的人数，
		// 不是「新客里复购的人数」。本用例里 A 是新客（首单在区间内）、
		// B 是老客但区间内只有 1 单，所以两边都不满足 → 0。
		// 记在这里是为了让下一个人读代码时不必再推一遍；改名/改口径要连 service 的
		// 复购率分子一起改，那是另一件事。
		if row.NewRepurchasers != 0 {
			t.Errorf("new_repurchasers = %d，期望 0（该列实际是「老客复购数」，本用例无人满足）", row.NewRepurchasers)
		}
		// 恒等式：新客 + 回头客 == 下单客户（每个人都属于且只属于一侧）。
		if row.NewCustomers+row.ReturningCustomers != row.OrderingCustomers {
			t.Errorf("五数不自洽：new+returning=%d ≠ ordering=%d",
				row.NewCustomers+row.ReturningCustomers, row.OrderingCustomers)
		}
	})

	t.Run("分段 id 列表", func(t *testing.T) {
		cases := []struct {
			seg  model.CustomerSegment
			want []int64
		}{
			{model.CustomerSegmentNew, []int64{101}},
			{model.CustomerSegmentReturning, []int64{102}},
			{model.CustomerSegmentRepurchasing, []int64{101}},
		}
		for _, c := range cases {
			ids, total, err := m.CustomerSegmentIDsByRange(ctx, projectID, from, to, c.seg, 0, 50, 0)
			if err != nil {
				t.Fatalf("segment %s: %v", c.seg, err)
			}
			if len(ids) != len(c.want) {
				t.Fatalf("segment %s = %v，期望 %v", c.seg, ids, c.want)
			}
			for i := range ids {
				if ids[i] != c.want[i] {
					t.Errorf("segment %s[%d] = %d，期望 %d", c.seg, i, ids[i], c.want[i])
				}
			}
			if total != int64(len(c.want)) {
				t.Errorf("segment %s 的 total = %d，期望 %d", c.seg, total, len(c.want))
			}
		}
	})

	t.Run("群组留存", func(t *testing.T) {
		rows, err := m.CohortRetention(ctx, projectID, from, to)
		if err != nil {
			t.Fatalf("CohortRetention: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("留存格数 = %d，期望 1（只有 A 的首单落在区间内）", len(rows))
		}
		got := rows[0]
		if got.CohortSize != 1 {
			t.Errorf("cohort_size = %d，期望 1", got.CohortSize)
		}
		if got.ActiveCustomers != 1 {
			t.Errorf("active_customers = %d，期望 1", got.ActiveCustomers)
		}
		wantMonth := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		if !got.CohortMonth.Equal(wantMonth) {
			t.Errorf("cohort_month = %v，期望 %v", got.CohortMonth, wantMonth)
		}
	})

	t.Run("RFM 分段计数", func(t *testing.T) {
		row, err := m.CustomerRfmSummary(ctx, projectID, from, to)
		if err != nil {
			t.Fatalf("CustomerRfmSummary: %v", err)
		}
		// 只有区间内下过单的人进 RFM（A 两单、B 一单）；C 区间内无单、D 已取消。
		if row.Customers != 2 {
			t.Fatalf("rfm customers = %d，期望 2", row.Customers)
		}
		if row.Vip+row.Potential+row.LowValue != row.Customers {
			t.Errorf("三个分段和 %d ≠ customers %d", row.Vip+row.Potential+row.LowValue, row.Customers)
		}
	})

	t.Run("RFM 明细与按段筛选", func(t *testing.T) {
		rows, total, err := m.CustomerRfmList(ctx, projectID, from, to, "", 50, 0)
		if err != nil {
			t.Fatalf("CustomerRfmList: %v", err)
		}
		if total != 2 || len(rows) != 2 {
			t.Fatalf("明细 total=%d len=%d，期望 2/2", total, len(rows))
		}
		// 按段筛出来的条数必须与 summary 的对应分段计数相等（同一套打分）。
		summary, err := m.CustomerRfmSummary(ctx, projectID, from, to)
		if err != nil {
			t.Fatalf("CustomerRfmSummary: %v", err)
		}
		for _, seg := range []string{model.RfmSegmentVip, model.RfmSegmentPotential, model.RfmSegmentLowValue} {
			_, segTotal, err := m.CustomerRfmList(ctx, projectID, from, to, seg, 50, 0)
			if err != nil {
				t.Fatalf("segment %s: %v", seg, err)
			}
			var want int64
			switch seg {
			case model.RfmSegmentVip:
				want = summary.Vip
			case model.RfmSegmentPotential:
				want = summary.Potential
			default:
				want = summary.LowValue
			}
			if segTotal != want {
				t.Errorf("段 %s 明细数 %d ≠ summary %d", seg, segTotal, want)
			}
		}
	})
}
