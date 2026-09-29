// membership_resolve.go — 读路径：解析一个访客在某工程的会员身份（BIZ-3）。
//
// 这是整个模块唯一会被**访问面**调用的方法（片段与客户页经 membershipcontract.Reader），
// 因此它守着三条硬约束：
//
//  1. **不写库**。读路径只读：查不到归属行就在内存里回退到默认等级，绝不「顺手建一行」
//     —— 那是在访问面写库，不在 AGENTS.md 不变量 1 的两个明文例外里（analytics 打点、
//     访问面守卫读会话）。
//  2. **默认等级缺失就打回给人**。不许静默归到 sort_order 最小的那一档：它可能是运营
//     已停用的，而「所有新访客突然都变成某个停用等级」不会报错、只会在对账时被发现。
//     错误里带上 project_id，让操作者能直接定位是哪个工程缺配置。
//  3. **一次事务内读完**。归属 → 等级 → 权益三步走同一个作用域事务，
//     避免「查到归属时等级还在、取权益时等级已被删」这种半截快照。
package membershipservice

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	"go_wp/pkg/logger"
)

// Resolve 解析某访客在某工程的会员身份。
func (s *Service) Resolve(ctx context.Context, req *membershipdto.ResolveReq) (res *membershipdto.MembershipResp, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	if err = requireUser(req.UserID); err != nil {
		return nil, err
	}

	var (
		tier       *membershipmodel.TierEntity
		ents       []*membershipmodel.EntitlementEntity
		source     string
		assignedAt *time.Time
		defaulted  bool
	)

	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		resolved, src, at, usedDefault, rerr := s.resolveTierTx(ctx, tx, req.ProjectID, req.UserID)
		if rerr != nil {
			return rerr
		}
		list, lerr := s.model.ListByTierTx(ctx, tx, resolved.ID)
		if lerr != nil {
			return lerr
		}
		tier, ents, source, assignedAt, defaulted = resolved, list, src, at, usedDefault
		return nil
	})
	if err != nil {
		return nil, err
	}

	res = &membershipdto.MembershipResp{
		UserID:        req.UserID,
		ProjectID:     req.ProjectID,
		TierID:        tier.ID,
		TierName:      tier.Name,
		Source:        source,
		IsDefaultTier: defaulted,
	}
	if assignedAt != nil {
		jt := toJSONTime(*assignedAt)
		res.AssignedAt = &jt
	}
	for _, ent := range ents {
		switch ent.Kind {
		case membershipenums.KindFreeShipping:
			res.FreeShipping = ent.ValueInt == 1
		case membershipenums.KindDiscount:
			res.DiscountPercent = ent.ValueInt
		}
	}
	return res, nil
}

// resolveTierTx 在事务内定出该访客的等级（归属 → 等级；落空则回退默认等级）。
//
// 返回值里带上 (source, assignedAt, defaulted) 而不是让调用方再查一遍归属：
// 「这份结果是归属行的结论」还是「回退到默认等级的兜底」是消费侧**必须**能分辨的
// —— 结算要据此决定「他是这个等级的会员」还是「他还没成为会员，等级只是个兜底值」。
func (s *Service) resolveTierTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64) (
	tier *membershipmodel.TierEntity, source string, assignedAt *time.Time, defaulted bool, err error) {
	assign, aerr := s.model.GetAssignmentTx(ctx, tx, projectID, userID)
	switch {
	case aerr == nil:
		row, terr := s.model.GetTierTx(ctx, tx, projectID, assign.TierID)
		if terr == nil {
			at := assign.AssignedAt
			return row, assign.Source, &at, false, nil
		}
		if !isNotFound(terr) {
			return nil, "", nil, false, terr
		}
		// 归属指向的等级已不可见（被软删 / 被手工 SQL 改过）。
		//
		// 与「没有归属行」分开处理、并留一条日志：两者的排障方向完全不同 ——
		// 数据不一致要人去看，而新访客是正常路径。DeleteTier 已经拒绝删除有归属的等级，
		// 所以走到这里说明有人绕过了应用层（或曾经的删除留下了历史行）。
		logger.Scene(errScene).
			With("projectId", projectID).With("userId", userID).With("tierId", assign.TierID).
			Warn("会员归属指向的等级已不可见，本次回退到默认等级（数据不一致，请检查该归属行）")
	case isNotFound(aerr):
		// 正常路径：还没有归属行 —— 落到下面的默认等级兜底。
	default:
		return nil, "", nil, false, aerr
	}

	def, derr := s.model.GetDefaultTierTx(ctx, tx, projectID)
	if derr != nil {
		if isNotFound(derr) {
			// 打回给人：错误里带 project_id（可定位的数据）。
			// 这里**不**退回 sort_order 最小的那一档 —— 那一档可能是运营已停用的。
			return nil, "", nil, false, errors.New(
				membershipenums.WithDetail(membershipenums.ErrDefaultTierMissing, projectID))
		}
		return nil, "", nil, false, derr
	}
	return def, "", nil, true, nil
}
