// membership_tier_crud.go — 等级与权益的后台配置面（BIZ-3）。
//
// 每条写用例都是**一个用户可感知的写操作**，且都涉及两处及以上持久化写入
// （等级主实体 + 权益关联行；或「取消旧默认 + 设新默认」两步），因此一律落在
// model.TransactionScoped 的同一个事务里（AGENTS.md §写操作的事务与回滚）。
//
// 冲突一律打回给人：唯一键撞车（等级名 / 非默认档门槛 / 默认等级）返回可辨识的业务错误
// 且带上定位信息（哪个名字、哪个门槛），不自动加后缀、不静默合并、不丢弃其中一方。
package membershipservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	"go_wp/pkg/logger"
)

// 唯一索引名（与迁移 462 逐字一致）。
//
// 提出来做常量是为了与迁移对账：改名时只改一处会表现为「23505 落到默认分支」——
// 症状是运营看到一句通用提示而不是「这个名字已经用了」。
const (
	indexTierName      = "uq_membership_tiers_name"
	indexTierThreshold = "uq_membership_tiers_threshold"
	indexTierDefault   = "uq_membership_tiers_default"
)

// ListTiers 列出某工程的等级（sort_order 降序，含各自权益）。
func (s *Service) ListTiers(ctx context.Context, req *membershipdto.ListTiersReq) (list []*membershipdto.TierResp, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		tiers, lerr := s.model.ListTiersTx(ctx, tx, req.ProjectID)
		if lerr != nil {
			return lerr
		}
		ents, eerr := s.model.ListByTierIDsTx(ctx, tx, tierIDsOf(tiers))
		if eerr != nil {
			return eerr
		}
		list = make([]*membershipdto.TierResp, 0, len(tiers))
		for _, tier := range tiers {
			list = append(list, toTierResp(tier, ents[tier.ID]))
		}
		return nil
	})
	return list, err
}

// GetTier 取单个等级详情（含权益）。
func (s *Service) GetTier(ctx context.Context, req *membershipdto.GetTierReq) (res *membershipdto.TierResp, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	if err = requireTier(req.TierID); err != nil {
		return nil, err
	}
	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		var ierr error
		res, ierr = s.tierRespTx(ctx, tx, req.ProjectID, req.TierID)
		return ierr
	})
	if isNotFound(err) {
		return nil, tierNotFound(req.TierID)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CreateTier 新建等级（可一次带上权益）。
//
// 事务内的顺序是刻意的：先读一次本工程的现有等级（一次查询同时支撑三条判据），
// 再建主实体，最后写权益 —— 任一步失败整体回滚，不会留下「有等级没权益」的半截状态。
func (s *Service) CreateTier(ctx context.Context, req *membershipdto.CreateTierReq) (res *membershipdto.TierResp, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	name, err := normalizeTierName(req.Name)
	if err != nil {
		return nil, err
	}
	remark, err := normalizeRemark(req.Remark)
	if err != nil {
		return nil, err
	}
	if err = validateThreshold(req.ThresholdAmount); err != nil {
		return nil, err
	}
	ents, err := normalizeEntitlements(req.Entitlements)
	if err != nil {
		return nil, err
	}
	if err = s.projectExists(ctx, req.ProjectID); err != nil {
		return nil, err
	}

	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		tiers, lerr := s.model.ListTiersTx(ctx, tx, req.ProjectID)
		if lerr != nil {
			return lerr
		}
		if n, cerr := s.model.CountByNameTx(ctx, tx, req.ProjectID, name, 0); cerr != nil {
			return cerr
		} else if n > 0 {
			return errors.New(membershipenums.WithDetail(membershipenums.ErrTierNameTaken, name))
		}
		isDefault := req.IsDefault
		if len(tiers) == 0 {
			// 工程的首个等级**强制**成为默认等级。
			//
			// 不是「静默改用户输入」：默认等级是读路径的兜底，工程一旦有等级却没有默认等级，
			// 所有访客的会员解析都会报 ErrDefaultTierMissing（整个会员能力不可用）。
			// 让运营先建一个普通等级、再手动改成默认，中间那段时间读路径是坏的 ——
			// 首建即默认把这个不可用窗口消掉，且结果在响应里如实回带（IsDefault = true）。
			isDefault = true
		}
		if isDefault {
			if n, cerr := s.model.CountDefaultTx(ctx, tx, req.ProjectID, 0); cerr != nil {
				return cerr
			} else if n > 0 {
				return errors.New(membershipenums.ErrDefaultTierExists)
			}
		} else if n, cerr := s.model.CountByThresholdTx(ctx, tx, req.ProjectID, req.ThresholdAmount, 0); cerr != nil {
			return cerr
		} else if n > 0 {
			return errors.New(membershipenums.WithDetail(
				membershipenums.ErrThresholdTaken, formatAmount(req.ThresholdAmount)))
		}

		row := &membershipmodel.TierEntity{
			ProjectID:       req.ProjectID,
			Name:            name,
			SortOrder:       req.SortOrder,
			ThresholdAmount: req.ThresholdAmount,
			IsDefault:       isDefault,
			Remark:          remark,
		}
		if cerr := s.model.CreateTierTx(ctx, tx, row); cerr != nil {
			return s.mapTierConflict(cerr)
		}
		if len(ents) > 0 {
			if cerr := s.model.ReplaceForTierTx(ctx, tx, row.ID, ents); cerr != nil {
				return cerr
			}
		}
		var rerr error
		res, rerr = s.tierRespTx(ctx, tx, req.ProjectID, row.ID)
		return rerr
	})
	if err != nil {
		return nil, err
	}
	logger.Scene(errScene).
		With("projectId", req.ProjectID).With("tierId", res.ID).With("name", name).
		Info("会员等级已创建")
	return res, nil
}

// UpdateTier 更新等级（指针字段 = 只在非 nil 时改）。
//
// IsDefault = true 的语义是「设为默认等级（原默认自动让位）」而不是「再要一个默认等级」——
// 两步写在同一事务里，配合部分唯一索引 uq_membership_tiers_default，
// 既不会留下两个默认，也不会留下零个默认。
func (s *Service) UpdateTier(ctx context.Context, req *membershipdto.UpdateTierReq) (res *membershipdto.TierResp, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	if err = requireTier(req.TierID); err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if req.Name != nil {
		name, nerr := normalizeTierName(*req.Name)
		if nerr != nil {
			return nil, nerr
		}
		fields["name"] = name
	}
	if req.Remark != nil {
		remark, rerr := normalizeRemark(*req.Remark)
		if rerr != nil {
			return nil, rerr
		}
		fields["remark"] = remark
	}
	if req.SortOrder != nil {
		fields["sort_order"] = *req.SortOrder
	}
	if req.ThresholdAmount != nil {
		if verr := validateThreshold(*req.ThresholdAmount); verr != nil {
			return nil, verr
		}
		fields["threshold_amount"] = *req.ThresholdAmount
	}

	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		current, gerr := s.model.GetTierTx(ctx, tx, req.ProjectID, req.TierID)
		if gerr != nil {
			return gerr
		}
		if req.Name != nil {
			name := fields["name"].(string)
			if n, cerr := s.model.CountByNameTx(ctx, tx, req.ProjectID, name, req.TierID); cerr != nil {
				return cerr
			} else if n > 0 {
				return errors.New(membershipenums.WithDetail(membershipenums.ErrTierNameTaken, name))
			}
		}
		if req.ThresholdAmount != nil && !current.IsDefault {
			if n, cerr := s.model.CountByThresholdTx(ctx, tx, req.ProjectID, *req.ThresholdAmount, req.TierID); cerr != nil {
				return cerr
			} else if n > 0 {
				return errors.New(membershipenums.WithDetail(
					membershipenums.ErrThresholdTaken, formatAmount(*req.ThresholdAmount)))
			}
		}
		if req.IsDefault != nil {
			switch {
			case *req.IsDefault && !current.IsDefault:
				// 切换默认等级：取消旧的与设新的必须同事务（分两次失败一半 = 零个默认等级）。
				if _, cerr := s.model.ClearDefaultTx(ctx, tx, req.ProjectID, req.TierID); cerr != nil {
					return cerr
				}
				fields["is_default"] = true
			case !*req.IsDefault && current.IsDefault:
				// 取消默认：工程必须始终有且只有一个默认等级。
				// 打回给人而不是自动把另一档提上来 —— 「哪一档该接替」是运营的决定，
				// 猜错会让所有未归属访客的等级在一夜之间变化而没人知道为什么。
				return errors.New(membershipenums.ErrDefaultTierRequired)
			}
		}
		n, uerr := s.model.UpdateTierFieldsTx(ctx, tx, req.ProjectID, req.TierID, fields)
		if uerr != nil {
			return s.mapTierConflict(uerr)
		}
		if n == 0 {
			return tierNotFound(req.TierID)
		}
		var rerr error
		res, rerr = s.tierRespTx(ctx, tx, req.ProjectID, req.TierID)
		return rerr
	})
	if isNotFound(err) {
		return nil, tierNotFound(req.TierID)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// DeleteTier 删除等级（软删）。
func (s *Service) DeleteTier(ctx context.Context, req *membershipdto.DeleteTierReq) (err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return err
	}
	if err = requireTier(req.TierID); err != nil {
		return err
	}
	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		current, gerr := s.model.GetTierTx(ctx, tx, req.ProjectID, req.TierID)
		if gerr != nil {
			return gerr
		}
		// 引用检查与删除同事务：先查再删之间的窗口里若有人新增归属，
		// 就会留下一条指向已软删等级的归属（解析时静默回落到默认等级，页面上看不出问题）。
		if n, cerr := s.model.CountByTierTx(ctx, tx, req.ProjectID, req.TierID); cerr != nil {
			return cerr
		} else if n > 0 {
			return errors.New(membershipenums.WithDetail(membershipenums.ErrTierInUse,
				fmt.Sprintf("%s×%d", current.Name, n)))
		}
		if current.IsDefault {
			// 删掉默认等级还有别的档时，工程会立刻进入「没有默认等级」状态 ——
			// 打回给人，让他先把另一档设为默认。
			// 只剩这一档时允许删除（工程回到「还没配等级」，读路径如实报缺默认等级）。
			others, lerr := s.model.ListTiersTx(ctx, tx, req.ProjectID)
			if lerr != nil {
				return lerr
			}
			if len(others) > 1 {
				return errors.New(membershipenums.ErrDefaultTierRequired)
			}
		}
		if _, derr := s.model.SoftDeleteTierTx(ctx, tx, req.ProjectID, req.TierID); derr != nil {
			return derr
		}
		return nil
	})
	if isNotFound(err) {
		return tierNotFound(req.TierID)
	}
	if err != nil {
		return err
	}
	logger.Scene(errScene).With("projectId", req.ProjectID).With("tierId", req.TierID).
		Info("会员等级已删除")
	return nil
}

// SaveEntitlements 全量保存某等级的权益（清单即最终状态，没列出的 kind 会被删掉）。
func (s *Service) SaveEntitlements(ctx context.Context, req *membershipdto.SaveEntitlementsReq) (res *membershipdto.SaveEntitlementsResp, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	if err = requireTier(req.TierID); err != nil {
		return nil, err
	}
	ents, err := normalizeEntitlements(req.Items)
	if err != nil {
		return nil, err
	}
	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		// 先确认该等级在本工程作用域内可见，才允许动它的权益。
		// membership_entitlements 没有 project_id、不受 RLS 约束 ——
		// 少了这一步，「拿别人的 tier_id 来改权益」会一路成功（表本身没有第二道防线）。
		if _, gerr := s.model.GetTierTx(ctx, tx, req.ProjectID, req.TierID); gerr != nil {
			return gerr
		}
		if rerr := s.model.ReplaceForTierTx(ctx, tx, req.TierID, ents); rerr != nil {
			return rerr
		}
		res = &membershipdto.SaveEntitlementsResp{TierID: req.TierID, Saved: len(ents)}
		return nil
	})
	if isNotFound(err) {
		return nil, tierNotFound(req.TierID)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// —— 内部助手 ——

// tierRespTx 在事务内组装一个等级的完整响应（等级 + 权益）。
func (s *Service) tierRespTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64) (*membershipdto.TierResp, error) {
	row, err := s.model.GetTierTx(ctx, tx, projectID, tierID)
	if err != nil {
		return nil, err
	}
	ents, err := s.model.ListByTierIDsTx(ctx, tx, []int64{tierID})
	if err != nil {
		return nil, err
	}
	return toTierResp(row, ents[tierID]), nil
}

// mapTierConflict 把唯一索引的 23505 映射成可辨识的业务错误。
//
// 为什么还要这层：事务内的主动检查（CountByNameTx 等）给出的是**可行动**的说法，
// 但它在并发下不是判据 —— 两个请求同时通过检查、同时插入，最后拦住它们的是索引。
// 没有这层映射，那种情况会落到归口错误（「操作失败，请稍后重试」），
// 运营看到的是「重试」而不是「这个名字已经用了」。
func (s *Service) mapTierConflict(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case uniqueViolationOn(err, indexTierName):
		return errors.New(membershipenums.ErrTierNameTaken)
	case uniqueViolationOn(err, indexTierThreshold):
		return errors.New(membershipenums.ErrThresholdTaken)
	case uniqueViolationOn(err, indexTierDefault):
		return errors.New(membershipenums.ErrDefaultTierExists)
	}
	return err
}

// tierNotFound 等级不存在的业务错误（带上 tier_id，让调用方能定位）。
func tierNotFound(tierID int64) error {
	return errors.New(membershipenums.WithDetail(membershipenums.ErrNotFound, fmt.Sprintf("tier_id=%d", tierID)))
}

// tierIDsOf 抽出等级 id 列表（批量查权益用）。
func tierIDsOf(tiers []*membershipmodel.TierEntity) []int64 {
	ids := make([]int64, 0, len(tiers))
	for _, tier := range tiers {
		if tier != nil {
			ids = append(ids, tier.ID)
		}
	}
	return ids
}

// formatAmount 门槛的定位信息（纯数字，单位由文案说明）。
//
// 不做元换算：换算只属于展示层，而错误信息里的数值必须与运营在表单里填的口径一致 ——
// 一个「1.00」在「单位分」的表单旁边会被读成 1 元，而它其实是 1 分。
func formatAmount(fen int64) string {
	return strconv.FormatInt(fen, 10)
}
