package analyticsservice

import (
	"context"
	"time"

	"go_wp/pkg/logger"
)

// PurgeExpiredViews 删除超过工程保留期的 page_views 明细（IDX-001）。
func (s *Service) PurgeExpiredViews(ctx context.Context) error {
	if s == nil || s.m == nil {
		return nil
	}
	projects, err := s.m.ListRetentionPolicies(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, p := range projects {
		if p.AnalyticsRetentionDays <= 0 {
			continue
		}
		cutoff := now.AddDate(0, 0, -p.AnalyticsRetentionDays)
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

const analyticsRetentionInterval = 24 * time.Hour

// StartAnalyticsRetentionScheduler 每日清理过期访问明细（IDX-001）。
func StartAnalyticsRetentionScheduler(svc *Service) {
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
