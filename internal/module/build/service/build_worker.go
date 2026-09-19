package buildservice

// build_worker.go — 队列消费（审计 DB-007 / DB-01）。
//
// 消费循环的形状：有活就接着干，没活才睡。任务成批到来时（依赖失效扇出一次
// 可能排进几十条）这样不会每条都等一个休眠周期；空闲时又不会空转打库。

import (
	"context"
	"errors"
	"time"

	buildcontract "go_wp/internal/module/build/contract"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
	"go_wp/pkg/logger"
)

// reclaimInterval 租约回收的检查间隔。
const reclaimInterval = time.Minute

// RunOnce 取一条待办任务并执行；返回 processed=false 表示队列为空（此刻没有活）。
//
// 认领时按租约时长发令牌：令牌是完成写入的凭据，也是「这条 running 是谁的」的唯一答案。
func (s *Service) RunOnce(ctx context.Context) (processed bool, err error) {
	if s == nil || s.m == nil {
		return false, nil
	}
	job, err := s.m.Claim(ctx, s.leaseTTL)
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
// 不会停在 running（除非 worker 本身被杀 —— 那种情况由租约到期回收兜）。
//
// 每处完成写入都带上认领时拿到的租约令牌（审计 DB-01）：任务若在 worker 执行期间
// 因租约到期被回收、合并或重新认领，旧 worker 的结论会拿到 ErrLeaseLost 并被丢弃 ——
// 「谁认领的谁才能结案」，迟到的成功不能把新 worker 的状态覆盖掉。
func (s *Service) execute(ctx context.Context, job *buildmodel.Entity) {
	payload := toDto(job)
	leaseToken := ""
	if job.LeaseToken != nil {
		leaseToken = *job.LeaseToken
	}
	fn, ok := s.executorFor(job.SourceType)
	if !ok {
		// 没有执行器 = 这份工作没人做得到。显式失败而不是出队丢弃：
		// 「排进来却没人做」若被静默吞掉，队列深度看着正常、站点就是不更新。
		msg := buildenums.ErrExecutorMissing + ": " + job.SourceType
		if merr := s.m.MarkFailed(ctx, job.ID, leaseToken, msg, s.now()); merr != nil {
			s.noteMarkRejected(job, "失败结论", merr)
		}
		logger.Scene("build").With("jobId", job.ID).With("sourceType", job.SourceType).
			Warn("构建任务没有对应执行器，已标记失败")
		return
	}

	// 单条任务超时：避免一条卡住的任务永久占着 worker（拿到它的那个协程不会再看别的任务）。
	// 它的截止时间早于租约到期，因此正常情况下「超时 → 写失败」先落地，
	// 回收只处理「连失败都没来得及写」的那种 worker。
	runCtx, cancel := context.WithTimeout(ctx, s.jobTimeout)
	defer cancel()
	if err := fn(runCtx, payload); err != nil {
		if merr := s.m.MarkFailed(ctx, job.ID, leaseToken, err.Error(), s.now()); merr != nil {
			s.noteMarkRejected(job, "失败结论", merr)
		} else {
			logger.Scene("build").With("jobId", job.ID).With("sourceId", job.SourceID).
				Error(err, "构建任务执行失败")
		}
		return
	}
	artifactID := ""
	if payload.ArtifactID != nil {
		artifactID = *payload.ArtifactID
	}
	if merr := s.m.MarkSucceeded(ctx, job.ID, leaseToken, artifactID, s.now()); merr != nil {
		s.noteMarkRejected(job, "成功结论", merr)
	}
}

// noteMarkRejected 记录「完成写入没落库」的两种原因。
//
// 租约失效是**预期内**的结果（回收先行、执行收尾），只记 warning；
// 其余写库失败是真故障，必须带原样错误进日志。
func (s *Service) noteMarkRejected(job *buildmodel.Entity, outcome string, err error) {
	if errors.Is(err, buildmodel.ErrLeaseLost) {
		logger.Scene("build").With("jobId", job.ID).With("sourceType", job.SourceType).
			Warn("构建任务租约已失效（任务已被回收或合并），本次" + outcome + "不落库")
		return
	}
	logger.Scene("build").With("jobId", job.ID).Error(err, "构建任务状态写入失败")
}

// StartWorkers 启动 n 个消费协程 + 一个租约回收协程。
//
// 多实例部署下每个实例都启动 worker 是安全的：认领走 FOR UPDATE SKIP LOCKED，
// 且迁移 295 的唯一索引保证同一来源同时只有一条 running —— 这就是选 PG 表
// 而不是进程内队列的主要原因（进程内锁拦不住另一个实例）。
// ctx 结束（应用退出）时所有 worker 停止 —— 正在执行的那条任务留给租约回收处理。
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

// reclaimLoop 周期性回收租约到期未结束的任务。
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
				logger.Scene("build").Error(err, "回收过期租约的构建任务失败")
				continue
			}
			if n > 0 {
				logger.Scene("build").With("reclaimed", n).Warn("已回收租约到期的构建任务")
			}
		}
	}
}

// 编译期断言：Executor 的类型来自 contract（装配层用同一类型注册）。
var _ buildcontract.Executor = func(context.Context, *buildcontract.Job) error { return nil }
