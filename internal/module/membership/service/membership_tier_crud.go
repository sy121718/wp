package membershipservice

import (
	"context"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	"go_wp/pkg/logger"
)

const (
	indexTierName      = "uq_membership_tiers_name"
	indexTierThreshold = "uq_membership_tiers_threshold"
	indexTierDefault   = "uq_membership_tiers_default"
)

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
			return membershipenums.ErrDetail(membershipenums.ErrTierNameTaken, name)
		}
		isDefault := req.IsDefault
		if len(tiers) == 0 {

			isDefault = true
		}
		if isDefault {
			if n, cerr := s.model.CountDefaultTx(ctx, tx, req.ProjectID, 0); cerr != nil {
				return cerr
			} else if n > 0 {
				return membershipenums.Err(membershipenums.ErrDefaultTierExists)
			}
		} else if n, cerr := s.model.CountByThresholdTx(ctx, tx, req.ProjectID, req.ThresholdAmount, 0); cerr != nil {
			return cerr
		} else if n > 0 {
			return membershipenums.ErrDetail(
				membershipenums.ErrThresholdTaken, formatAmount(req.ThresholdAmount))
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
				return membershipenums.ErrDetail(membershipenums.ErrTierNameTaken, name)
			}
		}
		if req.ThresholdAmount != nil && !current.IsDefault {
			if n, cerr := s.model.CountByThresholdTx(ctx, tx, req.ProjectID, *req.ThresholdAmount, req.TierID); cerr != nil {
				return cerr
			} else if n > 0 {
				return membershipenums.ErrDetail(
					membershipenums.ErrThresholdTaken, formatAmount(*req.ThresholdAmount))
			}
		}
		if req.IsDefault != nil {
			switch {
			case *req.IsDefault && !current.IsDefault:

				if _, cerr := s.model.ClearDefaultTx(ctx, tx, req.ProjectID, req.TierID); cerr != nil {
					return cerr
				}
				fields["is_default"] = true
			case !*req.IsDefault && current.IsDefault:

				return membershipenums.Err(membershipenums.ErrDefaultTierRequired)
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

		if n, cerr := s.model.CountByTierTx(ctx, tx, req.ProjectID, req.TierID); cerr != nil {
			return cerr
		} else if n > 0 {
			return membershipenums.ErrDetail(membershipenums.ErrTierInUse,
				fmt.Sprintf("%s×%d", current.Name, n))
		}
		if current.IsDefault {

			others, lerr := s.model.ListTiersTx(ctx, tx, req.ProjectID)
			if lerr != nil {
				return lerr
			}
			if len(others) > 1 {
				return membershipenums.Err(membershipenums.ErrDefaultTierRequired)
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

func (s *Service) mapTierConflict(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case uniqueViolationOn(err, indexTierName):
		return membershipenums.Err(membershipenums.ErrTierNameTaken)
	case uniqueViolationOn(err, indexTierThreshold):
		return membershipenums.Err(membershipenums.ErrThresholdTaken)
	case uniqueViolationOn(err, indexTierDefault):
		return membershipenums.Err(membershipenums.ErrDefaultTierExists)
	}
	return err
}

func tierNotFound(tierID int64) error {
	return membershipenums.ErrDetail(membershipenums.ErrNotFound, fmt.Sprintf("tier_id=%d", tierID))
}

func tierIDsOf(tiers []*membershipmodel.TierEntity) []int64 {
	ids := make([]int64, 0, len(tiers))
	for _, tier := range tiers {
		if tier != nil {
			ids = append(ids, tier.ID)
		}
	}
	return ids
}

func formatAmount(fen int64) string {
	return strconv.FormatInt(fen, 10)
}
