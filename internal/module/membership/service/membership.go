package membershipservice

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/membership/dto"
	"go_wp/internal/module/membership/enums"
	"go_wp/internal/module/membership/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

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

func (s *Service) UnlockManual(ctx context.Context, req *membershipdto.UnlockManualReq) (err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return err
	}
	if err = requireUser(req.UserID); err != nil {
		return err
	}
	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {

		if _, gerr := s.model.GetAssignmentTx(ctx, tx, req.ProjectID, req.UserID); gerr != nil {
			return gerr
		}
		n, uerr := s.model.UnlockManualTx(ctx, tx, req.ProjectID, req.UserID)
		if uerr != nil {
			return uerr
		}
		if n == 0 {
			return membershipenums.ErrDetail(membershipenums.ErrManualNotLocked,
				formatUser(req.UserID))
		}
		return nil
	})
	if isNotFound(err) {
		return membershipenums.ErrDetail(membershipenums.ErrNotFound, formatUser(req.UserID))
	}
	if err != nil {
		return err
	}
	logger.Scene(errScene).
		With("projectId", req.ProjectID).With("userId", req.UserID).
		Info("会员归属已解除手工锁定（下次日结按消费额重算）")
	return nil
}

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

func formatUser(userID uint64) string {
	return fmt.Sprintf("user_id=%d", userID)
}

const (
	recalcInterval = 24 * time.Hour

	recalcBatchSize = 500

	recalcTimeout = 30 * time.Minute
)

func (s *Service) RecalcProject(ctx context.Context, req *membershipdto.RecalcProjectReq) (res *membershipdto.RecalcResult, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	return s.recalcProject(ctx, req.ProjectID)
}

func (s *Service) recalcProject(ctx context.Context, projectID string) (res *membershipdto.RecalcResult, err error) {
	if s.purchases == nil {

		return nil, membershipenums.Err(membershipenums.ErrRecalcUnavailable)
	}

	totals, terr := s.purchases.SpentTotalsByUser(ctx, projectID)
	if terr != nil {
		return nil, terr
	}

	var tiers []*membershipmodel.TierEntity
	if lerr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		tiers, ierr = s.model.ListTiersTx(ctx, tx, projectID)
		return ierr
	}); lerr != nil {
		return nil, lerr
	}
	defaultTier := pickDefaultTier(tiers)
	if defaultTier == nil {

		return nil, membershipenums.ErrDetail(membershipenums.ErrDefaultTierMissing, projectID)
	}

	result := &membershipdto.RecalcResult{Scanned: len(totals)}

	batch := make([]recalcCandidate, 0, recalcBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		changed, skipped, ferr := s.applyRecalcBatch(ctx, projectID, batch)
		if ferr != nil {
			return ferr
		}
		result.Changed += changed
		result.Skipped += skipped
		batch = batch[:0]
		return nil
	}

	for userID, spent := range totals {
		tier := resolveSpendTier(tiers, defaultTier, spent)
		batch = append(batch, recalcCandidate{userID: userID, tierID: tier.ID})
		if len(batch) >= recalcBatchSize {
			if ferr := flush(); ferr != nil {
				return nil, ferr
			}
		}
	}
	if ferr := flush(); ferr != nil {
		return nil, ferr
	}
	return result, nil
}

type recalcCandidate struct {
	userID uint64
	tierID int64
}

func (s *Service) applyRecalcBatch(ctx context.Context, projectID string, batch []recalcCandidate) (changed, skipped int, err error) {
	err = s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		for _, item := range batch {

			ok, uerr := s.model.UpsertAutoTx(ctx, tx, projectID, item.userID, item.tierID)
			if uerr != nil {
				return uerr
			}
			if ok {
				changed++
			} else {
				skipped++
			}
		}
		return nil
	})
	return changed, skipped, err
}

func resolveSpendTier(tiers []*membershipmodel.TierEntity, defaultTier *membershipmodel.TierEntity, spent int64) *membershipmodel.TierEntity {
	if tier := selectTierBySpend(tiers, spent); tier != nil {
		return tier
	}
	return defaultTier
}

func (s *Service) RecalcReady() bool {
	if s == nil {
		return false
	}
	return s.purchases != nil
}

func (s *Service) RecalcAllProjects(ctx context.Context) (total *membershipdto.RecalcResult, err error) {
	total = &membershipdto.RecalcResult{}
	if s.purchases == nil {
		return total, membershipenums.Err(membershipenums.ErrRecalcUnavailable)
	}
	if s.projects == nil {
		return total, membershipenums.Err(membershipenums.ErrProjectRequired)
	}
	projects, perr := s.projects.List(ctx)
	if perr != nil {
		return total, perr
	}
	var lastErr error
	for _, p := range projects {
		res, rerr := s.recalcProject(ctx, p.ID)
		if rerr != nil {
			lastErr = rerr
			logger.Scene(errScene).With("projectId", p.ID).Error(rerr, "会员归属重算失败（该工程已跳过）")
			continue
		}
		total.Scanned += res.Scanned
		total.Changed += res.Changed
		total.Skipped += res.Skipped
	}
	return total, lastErr
}

func (s *Service) StartRecalcScheduler() {
	if utils.IsTestProcess() {
		return
	}
	if s == nil {
		return
	}
	if s.purchases == nil {

		logger.Scene(errScene).Warn(
			"会员归属日结未启动：消费额批量只读端口（membershipcontract.PurchaseSource）尚未注入，" +
				"归属重算整体不可用（等订单侧接线后调用 SetPurchaseSource + StartRecalcScheduler）")
		return
	}
	s.recalcOnce.Do(func() {
		go func() {
			run := func() {
				ctx, cancel := context.WithTimeout(context.Background(), recalcTimeout)
				defer cancel()
				res, err := s.RecalcAllProjects(ctx)
				if err != nil {
					logger.Scene(errScene).Error(err, "会员归属日结执行失败（部分工程已跳过）")
				}
				logger.Scene(errScene).
					With("scanned", res.Scanned).With("changed", res.Changed).With("skipped", res.Skipped).
					Info("会员归属日结完成")
			}
			run()
			ticker := time.NewTicker(recalcInterval)
			defer ticker.Stop()
			for range ticker.C {
				run()
			}
		}()
	})
}
