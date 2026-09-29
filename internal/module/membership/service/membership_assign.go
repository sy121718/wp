// membership_assign.go — 会员归属的后台面：手工指定 / 取消锁定 / 列表（BIZ-3）。
//
// 本文件与 service/membership_recalc.go 的关系是这份模块里最要紧的一条约定：
// **手工指定的归属不会被自动重算覆盖**。它的实现不在本文件，也不在重算文件，
// 而在 model.UpsertAutoTx 的那条原子 SQL 的 `WHERE membership_assignments.source <> 'manual'`
// —— 守卫写进 SQL 而不是靠调用方记得跳过，是因为调用方会变多（日结、后台「重算全部」、
// 将来的批量导入），而每多一个调用点，「记得跳过」就多一次静默失效的机会。
// 失效的表现是「运营手工调的等级第二天自己变回去了」，没有任何报错。
package membershipservice

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/pkg/logger"
)

// AssignManual 手工指定某访客在某工程的等级。
//
// 事务内的三步是「写一条归属」这一个用户可感知的操作的全部：
// 确认等级在本工程作用域内可见 → 原子 upsert → 读回组装响应。
// 空写（等级不存在）在第一步就返回，不会留下任何行。
func (s *Service) AssignManual(ctx context.Context, req *membershipdto.AssignManualReq) (res *membershipdto.AssignmentResp, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	if err = requireUser(req.UserID); err != nil {
		return nil, err
	}
	if err = requireTier(req.TierID); err != nil {
		return nil, err
	}
	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		// 等级必须在同一工程作用域内可见 —— 用 role 的 tier_id 会让两个工程的会员体系串起来，
		// 而两个 tier 表的行都「存在」且合法，事后完全看不出哪一步错了。
		tier, gerr := s.model.GetTierTx(ctx, tx, req.ProjectID, req.TierID)
		if gerr != nil {
			return gerr
		}
		if uerr := s.model.UpsertManualTx(ctx, tx, req.ProjectID, req.UserID, req.TierID); uerr != nil {
			return uerr
		}
		row, rerr := s.model.GetAssignmentTx(ctx, tx, req.ProjectID, req.UserID)
		if rerr != nil {
			return rerr
		}
		res = toAssignmentResp(row, tier.Name)
		return nil
	})
	if isNotFound(err) {
		return nil, tierNotFound(req.TierID)
	}
	if err != nil {
		return nil, err
	}
	logger.Scene(errScene).
		With("projectId", req.ProjectID).With("userId", req.UserID).With("tierId", req.TierID).
		Info("会员归属已手工指定（自动重算不再覆盖）")
	return res, nil
}

// UnlockManual 取消手工锁定，把归属交还自动重算。
//
// 只改 source、不动 tier_id：解锁本身不该造成一次可见的等级跳变 ——
// 等级要等下一次日结按消费额重算才变，运营因此有机会先看一眼当前值。
func (s *Service) UnlockManual(ctx context.Context, req *membershipdto.UnlockManualReq) (err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return err
	}
	if err = requireUser(req.UserID); err != nil {
		return err
	}
	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		// 先确认有一条归属行：没有行时 UnlockManualTx 也返回 0，
		// 但那两种情况要分开说（「这里本来就没有归属」vs「已经解锁过了」）。
		if _, gerr := s.model.GetAssignmentTx(ctx, tx, req.ProjectID, req.UserID); gerr != nil {
			return gerr
		}
		n, uerr := s.model.UnlockManualTx(ctx, tx, req.ProjectID, req.UserID)
		if uerr != nil {
			return uerr
		}
		if n == 0 {
			return errors.New(membershipenums.WithDetail(membershipenums.ErrManualNotLocked,
				formatUser(req.UserID)))
		}
		return nil
	})
	if isNotFound(err) {
		return errors.New(membershipenums.WithDetail(membershipenums.ErrNotFound, formatUser(req.UserID)))
	}
	if err != nil {
		return err
	}
	logger.Scene(errScene).
		With("projectId", req.ProjectID).With("userId", req.UserID).
		Info("会员归属已解除手工锁定（下次日结按消费额重算）")
	return nil
}

// ListAssignments 列出某工程的归属（分页 + 可选按等级 / 来源 / 访客筛）。
func (s *Service) ListAssignments(ctx context.Context, req *membershipdto.ListAssignmentsReq) (list []*membershipdto.AssignmentResp, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	page, size := normalizePage(req.Page, req.Size)
	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		rows, lerr := s.model.ListAssignmentsTx(ctx, tx, req.ProjectID, req.TierID, req.Source, req.UserID, page, size)
		if lerr != nil {
			return lerr
		}
		// 等级名一次批量取回（不是每行查一次）：列表页的 N+1 会随行数线性增长，
		// 而归属列表恰恰是「一页二十行、每行都要显示等级名」的形态。
		tiers, terr := s.model.ListTiersTx(ctx, tx, req.ProjectID)
		if terr != nil {
			return terr
		}
		list = make([]*membershipdto.AssignmentResp, 0, len(rows))
		for _, row := range rows {
			list = append(list, toAssignmentResp(row, tierNameOf(tiers, row.TierID)))
		}
		return nil
	})
	return list, err
}

// CountAssignments 统计某工程的归属数（与 ListAssignments 同一组筛选条件）。
func (s *Service) CountAssignments(ctx context.Context, req *membershipdto.CountAssignmentsReq) (total int64, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return 0, err
	}
	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		var cerr error
		total, cerr = s.model.CountAssignmentsTx(ctx, tx, req.ProjectID, req.TierID, req.Source, req.UserID)
		return cerr
	})
	return total, err
}

// formatUser 访客 id 的定位信息。
func formatUser(userID uint64) string {
	return fmt.Sprintf("user_id=%d", userID)
}
