// coupon_audit_model.go —— 券计数对账查询（审计 DB-021）。
package model

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// CouponCountMismatchRow 一行对账结果（model 层形状，service 转 dto）。
type CouponCountMismatchRow struct {
	CouponID    uint64
	ProjectID   string
	Code        string
	UsedCount   int64
	ActualCount int64
}

// CountCoupons 参与对账的券数量（分母）。projectID 为空时统计全部。
func (m *CouponModel) CountCoupons(ctx context.Context, projectID string) (n int64, err error) {
	// projectID 为空是「全量统计」：不设 scope 时策略谓词为 NULL，换非超级角色后
	// 该分支 fail closed（恒 0）而不会跨工程统计 —— 方向安全，但需要全站口径时
	// 必须由调用方给 projectID（列入 DB-009 剩余清单）。
	id := strings.TrimSpace(projectID)
	if id == "" {
		err = m.db.WithContext(ctx).Model(&CouponEntity{}).Count(&n).Error
		return n, err
	}
	err = rls.InProjectScope(ctx, m.db, id, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Where("project_id = ?", id).Count(&n).Error
	})
	return n, err
}

// ListCountMismatches 找出 used_count 与核销明细行数不一致的券。
//
// 明细的「有效」定义就是「行存在」：取消订单释放券走的是物理删除（见
// ReleaseRedemptionByOrderTx），所以这里直接 COUNT(*) 即可，不需要状态过滤。
//
// 只读：对账不修正任何数据 —— 偏差的原因（手工改库、早期逻辑缺口、并发守卫未命中）
// 决定了该往哪边修正，自动改可能把真源也改错。
func (m *CouponModel) ListCountMismatches(ctx context.Context, projectID string, limit int) (rows []CouponCountMismatchRow, err error) {
	if limit <= 0 {
		limit = 100
	}
	// LEFT JOIN 聚合子查询：没有核销记录的券也要参与对账（used_count > 0 而明细为空
	// 正是最需要发现的偏差 —— 计数虚高会让券提前用尽）。
	const q = "SELECT c.id AS coupon_id, c.project_id, c.code, c.used_count, " +
		"COALESCE(r.cnt, 0) AS actual_count " +
		"FROM coupons c " +
		"LEFT JOIN (SELECT coupon_id, COUNT(*) AS cnt FROM coupon_redemptions GROUP BY coupon_id) r " +
		"ON r.coupon_id = c.id " +
		"WHERE c.used_count <> COALESCE(r.cnt, 0) AND (? = '' OR c.project_id = ?) " +
		"ORDER BY c.id LIMIT ?"
	// 对账 SQL 一次扫 coupons + coupon_redemptions 两张带策略的表：scope 必须在同一条
	// 语句上生效。否则换角色后 LEFT JOIN 的右表被策略挡空，产生**假的计数偏差**
	// （used_count 全线「虚高」）—— 比查不到数据更坏，它是一份看起来合理的错误报告。
	id := strings.TrimSpace(projectID)
	if id == "" {
		err = m.db.WithContext(ctx).Raw(q, id, id, limit).Scan(&rows).Error
		return rows, err
	}
	err = rls.InProjectScope(ctx, m.db, id, func(tx *gorm.DB) error {
		return tx.Raw(q, id, id, limit).Scan(&rows).Error
	})
	return rows, err
}
