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
	"strings"

	orderdto "go_wp/internal/module/order/dto"
	"go_wp/pkg/logger"
)

// auditProjectIDs 对账要跑哪些工程：显式工程优先，留空则枚举全部工程逐工程对账。
//
// 为什么不保留「不限工程一次查完」（DB-009 第七批）：coupons 与 coupon_redemptions
// 都带 FORCE 策略、谓词读会话变量 —— 没有作用域的查询恒返回空集、计数恒 0，
// 对账于是输出一份「检查了 0 张券、0 个偏差」的**看起来正常的假报告**，
// 而那正是对账要发现问题的场景。取不到工程清单时显式失败，不退化成「不限工程」。
func (s *Service) auditProjectIDs(ctx context.Context, explicit string) ([]string, error) {
	if pid := strings.TrimSpace(explicit); pid != "" {
		return []string{pid}, nil
	}
	return s.projectIDs(ctx)
}

// AuditCouponCounts 对账券的 used_count 与核销明细行数。
func (s *Service) AuditCouponCounts(ctx context.Context, req *orderdto.CouponCountAuditReq) (res *orderdto.CouponCountAuditResp, err error) {
	if s == nil || s.coupons == nil {
		return &orderdto.CouponCountAuditResp{Items: []orderdto.CouponCountMismatch{}}, nil
	}
	projectID := ""
	limit := 100
	if req != nil {
		projectID = strings.TrimSpace(req.ProjectID)
		if req.Limit > 0 {
			limit = req.Limit
		}
	}
	// 工程清单（DB-009 第七批）：显式工程优先，留空则逐工程独立作用域跑一遍再合并。
	projects, perr := s.auditProjectIDs(ctx, projectID)
	if perr != nil {
		return nil, perr
	}
	res = &orderdto.CouponCountAuditResp{Items: make([]orderdto.CouponCountMismatch, 0)}
	for _, pid := range projects {
		if ctx.Err() != nil {
			break
		}
		checked, cerr := s.coupons.CountCoupons(ctx, pid)
		if cerr != nil {
			return nil, cerr
		}
		// limit 是**每工程**的明细上限（全站口径下原来是全局 top-N）：会话变量是单值，
		// 多个工程不可能并进一次查询。它只决定一次返回多少行明细，
		// 不改变 Checked / Mismatched 的口径。
		rows, lerr := s.coupons.ListCountMismatches(ctx, pid, limit)
		if lerr != nil {
			return nil, lerr
		}
		res.Checked += int(checked)
		res.Mismatched += len(rows)
		for _, row := range rows {
			res.Items = append(res.Items, orderdto.CouponCountMismatch{
				CouponID: row.CouponID, ProjectID: row.ProjectID, Code: row.Code,
				UsedCount: row.UsedCount, ActualCount: row.ActualCount,
				Diff: row.UsedCount - row.ActualCount,
			})
		}
	}
	if len(res.Items) > 0 {
		// 记日志而不告警升级：偏差不一定影响可用性，但需要有人知道。
		logger.Scene("order").With("mismatched", len(res.Items)).With("checked", res.Checked).
			Warn("券计数与核销明细不一致（只报告，不自动修正）")
	}
	return res, nil
}
