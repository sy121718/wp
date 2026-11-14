package buildservice

import (
	"sync"
	"context"
	"errors"
	"time"

	buildcontract "go_wp/internal/module/build/contract"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
	"go_wp/pkg/logger"
)

const reclaimInterval = time.Minute

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

func (s *Service) execute(ctx context.Context, job *buildmodel.Entity) {
	payload := toDto(job)
	leaseToken := ""
	if job.LeaseToken != nil {
		leaseToken = *job.LeaseToken
	}
	fn, ok := s.executorFor(job.SourceType)
	if !ok {

		msg := buildenums.ErrExecutorMissing + ": " + job.SourceType
		if merr := s.m.MarkFailed(ctx, job.ID, leaseToken, msg, s.now()); merr != nil {
			s.noteMarkRejected(job, "失败结论", merr)
		}
		logger.Scene("build").With("jobId", job.ID).With("sourceType", job.SourceType).
			Warn("构建任务没有对应执行器，已标记失败")
		return
	}

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

func (s *Service) noteMarkRejected(job *buildmodel.Entity, outcome string, err error) {
	if errors.Is(err, buildmodel.ErrLeaseLost) {
		logger.Scene("build").With("jobId", job.ID).With("sourceType", job.SourceType).
			Warn("构建任务租约已失效（任务已被回收或合并），本次" + outcome + "不落库")
		return
	}
	logger.Scene("build").With("jobId", job.ID).Error(err, "构建任务状态写入失败")
}

// 返回的 stop 等待全部 worker 与租约回收循环退出：调用方必须在关闭数据库之前调用它。
func (s *Service) StartWorkers(ctx context.Context, n int) func() {
	if s == nil || s.m == nil || n <= 0 {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var wg sync.WaitGroup
	wg.Add(n + 1)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			s.workerLoop(ctx)
		}()
	}
	go func() {
		defer wg.Done()
		s.reclaimLoop(ctx)
	}()
	return func() { wg.Wait() }
}

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

var _ buildcontract.Executor = func(context.Context, *buildcontract.Job) error { return nil }
