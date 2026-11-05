package model_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/order/model"
	"go_wp/public/test/support"
)

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
	insertCustomerOrder(t, db, projectID, 102, "paid", ago(8, 1))
	insertCustomerOrder(t, db, projectID, 103, "paid", ago(8, 15))
	insertCustomerOrder(t, db, projectID, 104, "cancelled", day(4))

	m := model.NewOrderModel(db)
	ctx := context.Background()

	t.Run("客户增长五数", func(t *testing.T) {
		row, err := m.CustomerGrowthByRange(ctx, projectID, from, to, nil)
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

		if row.NewRepurchasers != 0 {
			t.Errorf("new_repurchasers = %d，期望 0（该列实际是「老客复购数」，本用例无人满足）", row.NewRepurchasers)
		}

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
