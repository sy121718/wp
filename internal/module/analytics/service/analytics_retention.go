package analyticsservice

import (
	"context"
	"errors"
	"time"

	analyticsenums "go_wp/internal/module/analytics/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// PurgeExpiredViews 删除超过工程保留期的 page_views 明细（IDX-001）。
func (s *Service) PurgeExpiredViews(ctx context.Context) error {
	if s == nil || s.m == nil {
		return nil
	}
	policies, err := s.retentionPolicies(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, p := range policies {
		if p.RetentionDays <= 0 {
			continue
		}
		cutoff := now.AddDate(0, 0, -p.RetentionDays)
		n, derr := s.m.DeleteViewsBefore(ctx, p.ProjectID, cutoff)
		if derr != nil {
			logger.Scene("analytics").With("project_id", p.ProjectID).Error(derr, "清理过期访问明细失败")
			continue
		}
		if n > 0 {
			logger.Scene("analytics").With("project_id", p.ProjectID).With("deleted", n).Info("已清理过期访问明细")
		}
	}
	return nil
}

// retentionPolicies 工程的访问明细保留策略。
//
// 经 project 契约取：保留期那一列（`projects.analytics_retention_days`）长在 project 模块的
// 表上，读它的方法就该住在那个模块里（`projectmodel.ListRetentionPolicies`）—— 本模块原来
// 直接 `SELECT ... FROM projects WHERE analytics_retention_days > 0`，是越界读表。
func (s *Service) retentionPolicies(ctx context.Context) ([]projectcontract.RetentionPolicyResp, error) {
	if s == nil || s.retention == nil {
		return nil, errors.New(analyticsenums.ErrInvalidParam)
	}
	return s.retention.ListRetentionPolicies(ctx)
}

const analyticsRetentionInterval = 24 * time.Hour

// StartAnalyticsRetentionScheduler 每日清理过期访问明细（IDX-001）。
func StartAnalyticsRetentionScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_ = svc.PurgeExpiredViews(ctx)
		}
		run()
		ticker := time.NewTicker(analyticsRetentionInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
