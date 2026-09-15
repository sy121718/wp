// coupon_audit.go —— 券计数对账（审计 DB-021）。
//
// 为什么需要一个对账入口：coupons.used_count 是为并发守卫而存在的**投影**
// （核销走「UPDATE ... WHERE used_count < max_uses」的原子守卫，不能改成每次 COUNT），
// 真源是 coupon_redemptions 明细。两者没有数据库层约束，偏差会以两种方式显形：
//   - 计数偏大：券提前用尽（用户看到「已抢完」而实际还有额度）
//   - 计数偏小：可超出 max_uses 继续核销
//
// 两种都只能靠对账发现 —— 但**不自动修正**：偏差原因决定了该往哪边改，
// 自动改可能把真源也改错，先查清原因再说。
package orderservice

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
	"go_wp/pkg/logger"
)

// AuditCouponCounts 对账券的 used_count 与核销明细行数。
func (s *Service) AuditCouponCounts(ctx context.Context, req *orderdto.CouponCountAuditReq) (res *orderdto.CouponCountAuditResp, err error) {
	if s == nil || s.coupons == nil {
		return &orderdto.CouponCountAuditResp{Items: []orderdto.CouponCountMismatch{}}, nil
	}
	projectID := ""
	limit := 100
	if req != nil {
		projectID = req.ProjectID
		if req.Limit > 0 {
			limit = req.Limit
		}
	}
	checked, cerr := s.coupons.CountCoupons(ctx, projectID)
	if cerr != nil {
		return nil, cerr
	}
	rows, lerr := s.coupons.ListCountMismatches(ctx, projectID, limit)
	if lerr != nil {
		return nil, lerr
	}
	res = &orderdto.CouponCountAuditResp{
		Checked:    int(checked),
		Mismatched: len(rows),
		Items:      make([]orderdto.CouponCountMismatch, 0, len(rows)),
	}
	for _, row := range rows {
		res.Items = append(res.Items, orderdto.CouponCountMismatch{
			CouponID: row.CouponID, ProjectID: row.ProjectID, Code: row.Code,
			UsedCount: row.UsedCount, ActualCount: row.ActualCount,
			Diff: row.UsedCount - row.ActualCount,
		})
	}
	if len(res.Items) > 0 {
		// 记日志而不告警升级：偏差不一定影响可用性，但需要有人知道。
		logger.Scene("order").With("mismatched", len(res.Items)).With("checked", res.Checked).
			Warn("券计数与核销明细不一致（只报告，不自动修正）")
	}
	return res, nil
}
