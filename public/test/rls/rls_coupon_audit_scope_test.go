package rlstest

// rls_coupon_audit_scope_test.go — 券计数对账的工程作用域（DB-009 第七批）。
//
// 缺口形状：CountCoupons / ListCountMismatches 在 projectID 为空时走「全量对账」分支，
// 两条语句都**不带任何工程作用域**，而 coupons 与 coupon_redemptions 都带 FORCE 策略、
// 谓词读会话变量 app.project_id。换非超级角色后：
//   · COUNT 恒 0（分母没了）；
//   · 对账 SQL 里 LEFT JOIN 的右表被策略挡空，于是每一张券看起来都「计数虚高」——
//     这比查不到数据更坏，它是一份**看起来合理的错误报告**。
// 现在 projectID 必填（model 显式报 ErrProjectRequired），全站口径由 service 逐工程扇出合并。

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	"go_wp/pkg/rls"
)

// seedCoupon 经显式作用域写一张券，返回其自增 id。
func seedCoupon(t *testing.T, db *gorm.DB, projectID, code string, usedCount int) uint64 {
	t.Helper()
	var id uint64
	err := rls.InProjectScope(context.Background(), db, projectID, func(tx *gorm.DB) error {
		return tx.Raw("INSERT INTO coupons "+
			"(project_id, code, name, discount_type, discount_value, max_uses, used_count) "+
			"VALUES (?, ?, '', 'percent', 10, 0, ?) RETURNING id",
			projectID, code, usedCount).Scan(&id).Error
	})
	if err != nil {
		t.Fatalf("写入优惠券失败（RLS 生效时写入必须承工程作用域）: %v", err)
	}
	return id
}

// seedRedemption 经显式作用域写一行核销明细。
func seedRedemption(t *testing.T, db *gorm.DB, projectID string, couponID uint64, orderID uint64, code string) {
	t.Helper()
	err := rls.InProjectScope(context.Background(), db, projectID, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO coupon_redemptions "+
			"(coupon_id, project_id, code, order_id, order_no, discount_amount) VALUES (?, ?, ?, ?, '', 100)",
			couponID, projectID, code, orderID).Error
	})
	if err != nil {
		t.Fatalf("写入核销明细失败: %v", err)
	}
}

// TestRLS_CouponAuditScopedByProject 对账必须在工程作用域内跑。
func TestRLS_CouponAuditScopedByProject(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	// 工程 A：used_count = 1 但没有核销明细 → 偏差（计数虚高）。
	couponA := seedCoupon(t, db, pA, "AAA", 1)
	// 工程 B：used_count = 1 且有一行明细 → 一致。
	couponB := seedCoupon(t, db, pB, "BBB", 1)
	seedRedemption(t, db, pB, couponB, 9001, "BBB")

	m := ordermodel.NewCouponModel(db)

	nA, err := m.CountCoupons(ctx, pA)
	if err != nil {
		t.Fatalf("统计工程 A 的券失败: %v", err)
	}
	if nA != 1 {
		t.Fatalf("工程 A 应有 1 张券，实际 %d（无作用域时这里恒 0）", nA)
	}

	rowsA, err := m.ListCountMismatches(ctx, pA, 100)
	if err != nil {
		t.Fatalf("工程 A 对账失败: %v", err)
	}
	if len(rowsA) != 1 || rowsA[0].CouponID != couponA {
		t.Fatalf("工程 A 应检出 1 条偏差（%d），实际 %+v", couponA, rowsA)
	}
	if rowsA[0].UsedCount != 1 || rowsA[0].ActualCount != 0 {
		t.Fatalf("偏差应是 used_count=1 / actual=0，实际 %+v", rowsA[0])
	}

	// 工程 B 一致：没有偏差，且绝不能看到工程 A 的行。
	rowsB, err := m.ListCountMismatches(ctx, pB, 100)
	if err != nil {
		t.Fatalf("工程 B 对账失败: %v", err)
	}
	if len(rowsB) != 0 {
		t.Fatalf("工程 B 的券计数一致，不该有偏差，实际 %+v", rowsB)
	}

	// 缺口对照：**不带作用域**的裸查询看不到任何券 —— 改造前那个「全量对账」分支
	// 看到的就是这个集合（0 张券、0 个偏差的假报告）。
	var raw int64
	if err = db.Table("coupons").Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %d", raw)
	}
}

// TestRLS_CouponAuditRequiresProject 缺工程参数时显式报错，不退化成「不限工程」。
func TestRLS_CouponAuditRequiresProject(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()

	m := ordermodel.NewCouponModel(db)
	if _, err := m.CountCoupons(ctx, "  "); !errors.Is(err, ordermodel.ErrProjectRequired) {
		t.Fatalf("缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.ListCountMismatches(ctx, "", 10); !errors.Is(err, ordermodel.ErrProjectRequired) {
		t.Fatalf("缺工程应 ErrProjectRequired，实际 %v", err)
	}
}

// TestRLS_CouponAuditServiceFanout 留空工程时 service 逐工程扇出后合并结果。
func TestRLS_CouponAuditServiceFanout(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	seedCoupon(t, db, pA, "AAA", 1) // 有偏差
	couponB := seedCoupon(t, db, pB, "BBB", 1)
	seedRedemption(t, db, pB, couponB, 9002, "BBB") // 一致

	svc := orderservice.NewService(
		ordermodel.NewOrderModel(db), nil, nil,
		ordermodel.NewCouponModel(db), nil,
		nil, nil, nil, nil,
	)
	res, err := svc.AuditCouponCounts(ctx, &orderdto.CouponCountAuditReq{ProjectID: "", Limit: 100})
	if err != nil {
		t.Fatalf("全站对账失败: %v", err)
	}
	if res.Checked != 2 {
		t.Fatalf("全站对账应检查 2 张券（逐工程扇出），实际 %d", res.Checked)
	}
	if res.Mismatched != 1 {
		t.Fatalf("全站对账应检出 1 条偏差，实际 %d", res.Mismatched)
	}
	if len(res.Items) != 1 || res.Items[0].ProjectID != pA {
		t.Fatalf("偏差应落在工程 A，实际 %+v", res.Items)
	}

	// 显式指定工程：只跑那一个工程。
	one, err := svc.AuditCouponCounts(ctx, &orderdto.CouponCountAuditReq{ProjectID: pB, Limit: 100})
	if err != nil {
		t.Fatalf("按工程对账失败: %v", err)
	}
	if one.Checked != 1 || one.Mismatched != 0 {
		t.Fatalf("工程 B 应检查 1 张、0 偏差，实际 checked=%d mismatched=%d", one.Checked, one.Mismatched)
	}
}
