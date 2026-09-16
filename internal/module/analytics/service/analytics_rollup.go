package analyticsservice

// analytics_rollup.go — 访问统计的按天预聚合任务（审计 DB-005 / IDX-010）。
//
// 明细表（page_views）只增不减，而统计查询几乎总是查过去若干天。
// 那些天一旦过去就不会再变，却每次都被重新 GROUP BY 一遍。
// 本文件把「已经落定的天」提前算好写进 page_views_daily，
// 让历史窗口的查询成本与明细行数脱钩。
//
// 两条刻意的取舍：
//   - **重算而不是增量**：每次都从明细全量重算当天，靠 ON CONFLICT 覆盖。
//     增量累加一旦漏算就永久留下偏差（且无法与明细对账），重算不会。
//   - **今天也汇总，但查询仍走明细**：汇总每小时跑一次，今天那一行是快照，
//     用它回答「刚才那篇文章有没有人看」必然滞后；查询侧因此只在
//     「窗口完全落在今天之前」时才读汇总（见 analytics_query.go）。

import (
	"context"
	"time"

	"go_wp/pkg/logger"
)

const (
	// rollupInterval 汇总间隔。
	// 小时级足够：汇总只服务「已经过去的天」，当天的实时性由明细表保证；
	// 跑得再勤也只是把同一批不可变数据重算一遍。
	rollupInterval = time.Hour
	// rollupBackfillMaxDays 单次运行最多补齐的天数。
	//
	// 首次汇总时历史可能很长（明细保留期默认 90 天），一次补齐可接受；
	// 设上限是为了兜住「保留期被调得很长」的极端配置，让单次任务时间有界，
	// 剩下的下次接着补（水位是持久化的，不会漏）。
	rollupBackfillMaxDays = 400
)

// RollupRecent 补齐历史水位并刷新最近两天，返回处理的工程数。
//
// 两件事按顺序做：
//
//  1. **补齐**：从「最后已汇总日的次日」（从未汇总过则从最早明细日）逐日算到昨天。
//     没有这一步，历史窗口读汇总会读到空 —— 数字比明细少，比查得慢严重得多。
//  2. **刷新**：无条件重算今天与昨天。今天那行是当前快照（查询侧并不使用它，
//     但保留它让「今天已汇总」可判定，运维也能从 rolled_at 看出任务在跑）；
//     昨天那行是兜住跨日边界 —— 临近零点写入的访问可能落在昨天，
//     而昨天是在它还是今天时汇总的。
//
// 幂等：同一天被重算多少次，结果都等于从明细重新算一遍的值。
// 单个工程失败只记日志并继续 —— 一个工程的异常不该让其它工程的汇总停摆。
func (s *Service) RollupRecent(ctx context.Context) (projects int, err error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	ids, err := s.m.ListProjectsWithViews(ctx)
	if err != nil {
		return 0, err
	}
	today := dayStart(s.now())
	yesterday := today.AddDate(0, 0, -1)
	// lastErr 只用于把失败原因交回调用方（部分失败不影响其它工程继续跑）：
	// 单日失败已经逐条记了日志，但调度器看不到；返回给调用方才有机会被发现。
	var lastErr error
	for _, id := range ids {
		start, ok, gerr := s.rollupStart(ctx, id)
		if gerr != nil {
			logger.Scene("analytics").With("project_id", id).Error(gerr, "读取汇总水位失败")
			continue
		}
		days := 0
		for d := start; ok && !d.After(yesterday) && days < rollupBackfillMaxDays; d = d.AddDate(0, 0, 1) {
			if rerr := s.rollupOne(ctx, id, d, d, d.AddDate(0, 0, 1)); rerr != nil {
				lastErr = rerr
				break
			}
			days++
		}
		if days >= rollupBackfillMaxDays {
			logger.Scene("analytics").With("project_id", id).With("days", days).
				Warn("访问统计补齐达到单次上限，剩余天数由下次任务继续")
		}
		if rerr := s.rollupOne(ctx, id, yesterday, yesterday, today); rerr != nil {
			lastErr = rerr
		}
		if rerr := s.rollupOne(ctx, id, today, today, today.AddDate(0, 0, 1)); rerr != nil {
			lastErr = rerr
		}
	}
	return len(ids), lastErr
}

// rollupStart 计算补齐的起点：已有水位则从次日继续，否则从最早明细日重头补。
func (s *Service) rollupStart(ctx context.Context, projectID string) (start time.Time, ok bool, err error) {
	last, has, err := s.m.LastRolledDay(ctx, projectID)
	if err != nil {
		return time.Time{}, false, err
	}
	if has {
		return last.AddDate(0, 0, 1), true, nil
	}
	earliest, hasEarliest, err := s.m.EarliestViewDay(ctx, projectID)
	if err != nil {
		return time.Time{}, false, err
	}
	if !hasEarliest {
		return time.Time{}, false, nil
	}
	return earliest, true, nil
}

// rollupOne 汇总一天：失败记日志并返回错误（补齐循环据此停在该天，下次继续）。
func (s *Service) rollupOne(ctx context.Context, projectID string, day, from, to time.Time) error {
	if err := s.m.RollupDay(ctx, projectID, day, from, to); err != nil {
		logger.Scene("analytics").With("project_id", projectID).With("day", day.Format(dateLayout)).
			Error(err, "访问统计汇总失败")
		return err
	}
	return nil
}

// StartAnalyticsRollupScheduler 启动每小时汇总（先跑一次再等间隔，与其它调度同形）。
func StartAnalyticsRollupScheduler(svc *Service) {
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_, _ = svc.RollupRecent(ctx)
		}
		run()
		ticker := time.NewTicker(rollupInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
