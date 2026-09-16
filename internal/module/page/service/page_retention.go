// page_retention.go —— 页面域的生命周期任务（审计 IDX-004 产物 GC / IDX-005 历史快照 / IDX-019）。
//
// 两件事：历史快照收敛（保存时顺手做一次 + 定时兜底）与产物 GC 定时化。
// 保留期都写成常量并从声明处引用：此前「产物 GC 默认 dryRun 且没有定时任务、修订快照
// 完全不清理」的根因不是写不出清理，而是**没有任何地方承诺保留期**。
package pageservice

import (
	"context"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/retention"
	"go_wp/pkg/logger"
)

const (
	// pageRevisionKeep 每页保留的历史快照条数。
	pageRevisionKeep = 20
	// pageRevisionRetainDays 历史快照保留期：超出条数**且**早于该窗口才清理。
	pageRevisionRetainDays = 90
	// artifactRetentionDays 产物保留窗口：早于它且不再被任何指针引用的产物可回收。
	artifactRetentionDays = 30
	// pageRetentionInterval 保留期任务的运行间隔。
	pageRetentionInterval = 24 * time.Hour
	// pageRetentionBatch 单批删除行数：批次存在的意义是不制造长事务与锁表。
	pageRetentionBatch = 500
)

// pruneRevisions 保存草稿后收敛该页的历史快照。
//
// 放在保存路径上是刻意的：定时任务一天只跑一次，高频编辑的页面在这之间照样能堆出
// 成百上千份完整文档快照。失败只记日志 —— 清理不该让一次保存失败。
// projectID 必填（DB-009 第四批）：修订表没有 project_id 列、不受策略约束，
// 归属经 pages 判断 —— 缺它这条清理会删到别的工程页面的历史版本。
func (s *Service) pruneRevisions(ctx context.Context, projectID, pageID string) {
	if s == nil || s.model == nil {
		return
	}
	n, err := s.model.PruneRevisions(ctx, projectID, pageID, pageRevisionKeep)
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "收敛页面历史快照失败")
		return
	}
	if n > 0 {
		logger.Scene("page").With("pageId", pageID).With("deleted", n).Info("已收敛页面历史快照")
	}
}

// PurgeRetention 执行一次保留期清理：历史快照（分批删）+ 产物 GC（真删）。
//
// 产物 GC 的顺序是「先确认不在保护集合（已激活/已暂存）→ 删 DB 行 → 删磁盘」，
// 磁盘删除失败只记日志：孤儿文件由反向对账（IDX-015）暴露，不需要在这里回滚 DB。
func (s *Service) PurgeRetention(ctx context.Context) (deletedRevisions int64, err error) {
	if s == nil || s.model == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	revisions := retention.Task{
		Name: "page_revisions", Table: "page_revisions", TimeColumn: "create_time",
		Retain: pageRevisionRetainDays * 24 * time.Hour, BatchSize: pageRetentionBatch,
		Note: "每页保留最近若干版本；只有既超出条数、又早于保留期的才删（回退需要近期版本）",
		Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
			// 逐工程（DB-009 第四批）：修订表不受策略约束，「全库清理」必须由调用方
			// 逐工程展开，否则这条 DELETE 会跨工程删历史快照，而且一句日志都不报。
			projects, perr := s.fanoutProjectIDs(ctx)
			if perr != nil {
				return 0, perr
			}
			var total int64
			for _, pid := range projects {
				if ctx.Err() != nil {
					break
				}
				n, derr := s.model.DeleteStaleRevisions(ctx, pid, pageRevisionKeep, cutoff, limit)
				if derr != nil {
					return total, derr
				}
				total += n
			}
			return total, nil
		},
	}
	outcomes := retention.RunAll(ctx, []retention.Task{revisions}, now)
	total, failed := retention.Summary(outcomes)
	if len(failed) > 0 {
		logger.Scene("page").With("failed", failed).Warn("保留期任务部分失败")
	}
	if total > 0 {
		logger.Scene("page").With("deleted", total).Info("已清理超期页面历史快照")
	}

	// 产物 GC：显式传 dryRun=false 才会真删（安全默认仍在接口侧保留）。
	notDryRun := false
	res, err := s.GarbageCollectArtifacts(ctx, &pagedto.GCArtifactsReq{
		RetentionDays: artifactRetentionDays, DryRun: &notDryRun,
	})
	if err != nil {
		logger.Scene("page").Error(err, "定时回收产物失败")
		// 产物回收失败不算整个保留期任务失败：历史快照那一半已经做完了。
		return total, err
	}
	if res != nil && (res.Deleted > 0 || res.Failed > 0) {
		logger.Scene("page").
			With("scanned", res.Scanned).With("deleted", res.Deleted).With("failed", res.Failed).
			Info("已回收超期产物")
	}
	return total, nil
}

// StartPageRetentionScheduler 启动每日保留期清理（IDX-004 / IDX-005）。
//
// 进程内 goroutine + ticker，与 analytics / order 的既有调度同形（先跑一次再等间隔）：
// 单实例部署够用；多实例部署下重复执行是安全的（删除按时间分界幂等）。
func StartPageRetentionScheduler(svc *Service) {
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
		ticker := time.NewTicker(pageRetentionInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
