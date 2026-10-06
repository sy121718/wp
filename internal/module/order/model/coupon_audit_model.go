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

// CountCoupons 参与对账的券数量（分母）。projectID 必填（DB-009 第七批）。
//
// 原来 projectID 为空时走「全量统计」—— coupons 带 FORCE 策略、谓词读会话变量，
// 那条不带作用域的 Count 在非超级角色下**恒 0**（fail closed 不报错），于是对账输出
// 「检查了 0 张券、0 个偏差」的看起来正常的假报告。全站口径由 service 逐工程扇出后合并。
func (m *CouponModel) CountCoupons(ctx context.Context, projectID string) (n int64, err error) {
	id := strings.TrimSpace(projectID)
	if id == "" {
		return 0, ErrProjectRequired
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
	//
	// 聚合子查询用 GORM 链式而不是内联字符串：`Joins("LEFT JOIN (?) AS r ...", sub)`
	// 里的 sub 是另一个 `*gorm.DB`，GORM 会把它渲染成带括号的子查询 —— 这才是「走 GORM」
	// 的实质（拼串的地方只剩别名与 ON 条件，而它们没有可参数化的部分）。
	id := strings.TrimSpace(projectID)
	if id == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, id, func(tx *gorm.DB) error {
		// 对账一条语句扫 coupons + coupon_redemptions 两张带策略的表：scope 必须在
		// **同一条语句**上生效。否则换角色后 LEFT JOIN 的右表被策略挡空，产生**假的计数
		// 偏差**（used_count 全线「虚高」）—— 比查不到数据更坏，它是一份看起来合理的错误报告。
		redemptions := tx.Model(&CouponRedemptionEntity{}).
			Select("coupon_id, COUNT(*) AS cnt").
			Group("coupon_id")

		return tx.Table(CouponEntity{}.TableName()+" AS c").
			Select("c.id AS coupon_id, c.project_id, c.code, c.used_count, COALESCE(r.cnt, 0) AS actual_count").
			Joins("LEFT JOIN (?) AS r ON r.coupon_id = c.id", redemptions).
			Where("c.used_count <> COALESCE(r.cnt, 0)").
			Where("c.project_id = ?", id).
			Order("c.id").
			Limit(limit).
			Find(&rows).Error
	})
	return rows, err
}
