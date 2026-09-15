package buildservice

// build_worker.go — 队列消费（审计 DB-007）。
//
// 消费循环的形状：有活就接着干，没活才睡。任务成批到来时（依赖失效扇出一次
// 可能排进几十条）这样不会每条都等一个休眠周期；空闲时又不会空转打库。

import (
	"context"
	"time"

	buildcontract "go_wp/internal/module/build/contract"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
	"go_wp/pkg/logger"
)

// reclaimInterval 僵尸回收的检查间隔。
const reclaimInterval = time.Minute

// RunOnce 取一条待办任务并执行；返回 processed=false 表示队列为空（此刻没有活）。
func (s *Service) RunOnce(ctx context.Context) (processed bool, err error) {
	if s == nil || s.m == nil {
		return false, nil
	}
	job, err := s.m.Claim(ctx)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}
	s.execute(ctx, job)
	return true, nil
}

// execute 执行一条任务并把结果落成状态。
//
// 执行器只回答「成没成」，状态机全在这里：一条任务要么 succeeded、要么 failed,
// 不会停在 running（除非 worker 本身被杀 —— 那种情况由 ReclaimStale 兜）。
func (s *Service) execute(ctx context.Context, job *buildmodel.Entity) {
	payload := toDto(job)
	fn, ok := s.executorFor(job.SourceType)
	if !ok {
		// 没有执行器 = 这份工作没人做得到。显式失败而不是出队丢弃：
		// 「排进来却没人做」若被静默吞掉，队列深度看着正常、站点就是不更新。
		msg := buildenums.ErrExecutorMissing + ": " + job.SourceType
		_ = s.m.MarkFailed(ctx, job.ID, msg, s.now())
		logger.Scene("build").With("jobId", job.ID).With("sourceType", job.SourceType).
			Warn("构建任务没有对应执行器，已标记失败")
		return
	}

	// 单条任务超时：避免一条卡住的任务永久占着 worker（拿到它的那个协程不会再看别的任务）。
	runCtx, cancel := context.WithTimeout(ctx, s.jobTimeout)
	defer cancel()
	if err := fn(runCtx, payload); err != nil {
		_ = s.m.MarkFailed(ctx, job.ID, err.Error(), s.now())
		logger.Scene("build").With("jobId", job.ID).With("sourceId", job.SourceID).
			Error(err, "构建任务执行失败")
		return
	}
	artifactID := ""
	if payload.ArtifactID != nil {
		artifactID = *payload.ArtifactID
	}
	_ = s.m.MarkSucceeded(ctx, job.ID, artifactID, s.now())
}

// StartWorkers 启动 n 个消费协程 + 一个僵尸回收协程。
//
// 多实例部署下每个实例都启动 worker 是安全的：取任务走 SKIP LOCKED，
// 同一条任务只会被一个 worker 拿到（这是选 PG 表而不是进程内队列的主要原因之一）。
// ctx 结束（应用退出）时所有 worker 停止 —— 正在执行的那条任务留给 ReclaimStale 处理。
func (s *Service) StartWorkers(ctx context.Context, n int) {
	if s == nil || s.m == nil || n <= 0 {
		return
	}
	for i := 0; i < n; i++ {
		go s.workerLoop(ctx)
	}
	go s.reclaimLoop(ctx)
}

// workerLoop 单个消费协程。
func (s *Service) workerLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		processed, err := s.RunOnce(ctx)
		if err != nil {
			logger.Scene("build").Error(err, "构建队列取任务失败")
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.workerIdle):
		}
	}
}

// reclaimLoop 周期性回收超时未结束的任务。
func (s *Service) reclaimLoop(ctx context.Context) {
	ticker := time.NewTicker(reclaimInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.ReclaimStale(ctx)
			if err != nil {
				logger.Scene("build").Error(err, "回收僵尸构建任务失败")
				continue
			}
			if n > 0 {
				logger.Scene("build").With("reclaimed", n).Warn("已回收超时未结束的构建任务")
			}
		}
	}
}

// 编译期断言：Executor 的类型来自 contract（装配层用同一类型注册）。
var _ buildcontract.Executor = func(context.Context, *buildcontract.Job) error { return nil }
