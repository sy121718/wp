// mail_retention.go —— 邮件域的生命周期任务（审计 IDX-012 / IDX-019）。
//
// 顺序是「先固化、后清理」：明细是报表的唯一数据来源，直接删等于把历史统计一起删掉。
// 固化只针对「全部事件都已过期」的活动（那些明细马上要被删，此刻统计出来就是终值）。
package mailservice

import (
	"context"
	"time"

	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/retention"
	"go_wp/pkg/logger"
)

const (
	// mailEventRetainDays 事件明细与发送日志的保留期。
	mailEventRetainDays = 180
	// mailRetentionBatch 单批删除行数。
	mailRetentionBatch = 500
	// mailRetentionInterval 保留期任务运行间隔。
	mailRetentionInterval = 24 * time.Hour
	// mailTotalsSweepLimit 单轮固化的活动数上限（防止一次跑太久）。
	mailTotalsSweepLimit = 500
)

// EnsureCampaignTotals 把「明细即将被清理」的活动汇总固化到活动记录。
//
// 必须在清理之前执行：事件行一旦删掉就再也统计不出打开/点击数，报表只能看到 0。
func (s *Service) EnsureCampaignTotals(ctx context.Context, cutoff time.Time) (fixed int, err error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	ids, lerr := s.m.ListCampaignsWithExpiredEvents(ctx, cutoff, mailTotalsSweepLimit)
	if lerr != nil {
		return 0, lerr
	}
	for _, id := range ids {
		counts, cerr := s.m.CountEventsByType(ctx, id)
		if cerr != nil {
			logger.Scene("mail").With("campaign_id", id).Error(cerr, "统计活动事件失败，跳过固化")
			continue
		}
		// 事件类型常量与写入侧同源（model 包），不在两处各写一遍字符串。
		opened := counts[mailmodel.EventTypeOpen]
		clicked := counts[mailmodel.EventTypeClick]
		n, uerr := s.m.SetCampaignEventTotalsIfUnset(ctx, id, opened, clicked)
		if uerr != nil {
			logger.Scene("mail").With("campaign_id", id).Error(uerr, "固化活动事件汇总失败")
			continue
		}
		if n > 0 {
			fixed++
		}
	}
	return fixed, nil
}

// PurgeRetention 执行一次邮件域保留期清理：先固化汇总，再清事件明细与发送日志。
func (s *Service) PurgeRetention(ctx context.Context) (deleted int64, err error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	retain := mailEventRetainDays * 24 * time.Hour
	cutoff := now.Add(-retain)
	fixed, ferr := s.EnsureCampaignTotals(ctx, cutoff)
	if ferr != nil {
		logger.Scene("mail").Error(ferr, "固化活动事件汇总失败")
	} else if fixed > 0 {
		logger.Scene("mail").With("fixed", fixed).Info("已固化活动事件汇总")
	}
	tasks := []retention.Task{
		{
			Name: "mail_campaign_events", Table: "mail_campaign_events", TimeColumn: "create_time",
			Retain: retain, BatchSize: mailRetentionBatch,
			Note: "一次打开/点击一行；报表用「固化汇总 + 保留期内明细」，因此明细可过期清理",
			Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
				return s.m.DeleteEventsBefore(ctx, cutoff, limit)
			},
		},
		{
			Name: "mail_logs", Table: "mail_logs", TimeColumn: "create_time",
			Retain: retain, BatchSize: mailRetentionBatch,
			Note: "逐封发送留档；可追溯价值随时间衰减，异常排查窗口远小于保留期",
			Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
				return s.m.DeleteLogsBefore(ctx, cutoff, limit)
			},
		},
		{
			Name: "mail_automation_node_logs", Table: "mail_automation_node_logs", TimeColumn: "create_time",
			Retain: retain, BatchSize: mailRetentionBatch,
			Note: "自动化流程逐节点一行（启用后增长最快）；按「最近发生了什么」的排障视图定位而非常年留档",
			Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
				return s.m.DeleteNodeLogsBefore(ctx, cutoff, limit)
			},
		},
	}
	outcomes := retention.RunAll(ctx, tasks, now)
	total, failed := retention.Summary(outcomes)
	if len(failed) > 0 {
		logger.Scene("mail").With("failed", failed).Warn("邮件保留期任务部分失败")
	}
	if total > 0 {
		logger.Scene("mail").With("deleted", total).Info("已清理过期邮件明细")
	}
	return total, nil
}

// StartMailRetentionScheduler 启动每日邮件明细清理（与 analytics / order 的既有调度同形）。
func StartMailRetentionScheduler(svc *Service) {
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, _ = svc.PurgeRetention(ctx)
		}
		run()
		ticker := time.NewTicker(mailRetentionInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
