// Package buildservice 构建任务队列：入队、消费、回收与可见性（审计 DB-007）。
package buildservice

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	buildcontract "go_wp/internal/module/build/contract"
	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
)

var _ buildcontract.BuildService = (*Service)(nil)

// 队列的默认节奏。
const (
	// defaultWorkerIdle 队列空转时的休眠间隔。
	// 1 秒的取舍：队列里的任务通常成批到来（依赖失效扇出），
	// 用 LISTEN/NOTIFY 唤醒能把延迟压到毫秒，但要多一条长连接与一套重连逻辑 ——
	// 对「内容改完等几秒站点更新」这个场景不值。
	defaultWorkerIdle = time.Second
	// defaultStaleAfter running 任务多久没结束算僵尸。
	// 取 15 分钟：单页构建是秒级，全站级任务也远低于这个量级；
	// 阈值太短会把正常的长任务反复重排（同一份工作被做两遍）。
	defaultStaleAfter = 15 * time.Minute
	// defaultJobTimeout 单条任务的执行上限（避免一条任务永久占着 worker）。
	defaultJobTimeout = 10 * time.Minute
)

// Service 构建队列服务。
type Service struct {
	m *buildmodel.Model

	mu        sync.RWMutex
	executors map[string]buildcontract.Executor

	workerIdle time.Duration
	staleAfter time.Duration
	jobTimeout time.Duration
	now        func() time.Time
}

// NewService 构造。
func NewService(m *buildmodel.Model) *Service {
	return &Service{
		m:          m,
		executors:  map[string]buildcontract.Executor{},
		workerIdle: defaultWorkerIdle,
		staleAfter: defaultStaleAfter,
		jobTimeout: defaultJobTimeout,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// Model 暴露底层 model 供测试检查队列行。
func (s *Service) Model() *buildmodel.Model { return s.m }

// SetStaleAfter 覆盖僵尸判定阈值（测试用）。
func (s *Service) SetStaleAfter(d time.Duration) { s.staleAfter = d }

// RegisterExecutor 注册某来源类型的执行器。
//
// 未注册的来源类型会被显式标记失败（ErrExecutorMissing）而不是静默跳过：
// 「任务排进来却没人做」是队列最糟的失败形态 —— 队列深度看着正常，站点就是不更新。
func (s *Service) RegisterExecutor(sourceType string, fn buildcontract.Executor) {
	if s == nil || strings.TrimSpace(sourceType) == "" || fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executors[sourceType] = fn
}

// executorFor 取执行器。
func (s *Service) executorFor(sourceType string) (buildcontract.Executor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn, ok := s.executors[sourceType]
	return fn, ok
}

// Enqueue 入队一条构建任务。
//
// created=false 表示同一目标、同一构建输入的工作已在队列里（迁移 171 的部分唯一索引兜底）。
func (s *Service) Enqueue(ctx context.Context, req *builddto.EnqueueReq) (job *builddto.Job, created bool, err error) {
	if s == nil || s.m == nil || req == nil {
		return nil, false, errors.New(buildenums.ErrInvalidParam)
	}
	sourceType := strings.TrimSpace(req.SourceType)
	sourceID := strings.TrimSpace(req.SourceID)
	switch sourceType {
	case buildmodel.SourceTypePage, buildmodel.SourceTypePresentation:
	default:
		// 白名单与 DDL 的 CHECK 一致，但先在这里拦：数据库拒绝的报错是约束名，
		// 这里能给出「来源类型不支持」这个可读结论，也不会留下半条任务。
		return nil, false, errors.New(buildenums.ErrInvalidSource)
	}
	if sourceID == "" {
		return nil, false, errors.New(buildenums.ErrInvalidParam)
	}
	e := &buildmodel.Entity{
		SourceType: sourceType, SourceID: sourceID,
		DraftVersion: req.DraftVersion, BuildInputHash: strings.TrimSpace(req.BuildInputHash),
		Status: buildmodel.StatusPending, CreateTime: s.now(),
	}
	created, err = s.m.Enqueue(ctx, e)
	if err != nil {
		return nil, false, err
	}
	if !created {
		return nil, false, nil
	}
	job = toDto(e)
	return job, true, nil
}

// EnqueuePageBuild 供 page 模块把超出单次上限的自动重建交给队列。
func (s *Service) EnqueuePageBuild(ctx context.Context, pageID string, draftVersion int64, buildInputHash string) error {
	_, _, err := s.Enqueue(ctx, &builddto.EnqueueReq{
		SourceType: "page", SourceID: pageID,
		DraftVersion: draftVersion, BuildInputHash: buildInputHash,
	})
	return err
}

// EnqueuePresentationBuild 供 presentation 模块把自动重建交给队列（PERF-020）。
//
// build_input_hash 传空串是刻意的：队列的部分唯一索引按 (来源, 目标, hash) 去重，
// 空串让「同一实例同时只有一条待办」成立 —— 依赖失效扇出反复标记同一实例时不会堆出多份任务。
func (s *Service) EnqueuePresentationBuild(ctx context.Context, presentationID string) error {
	_, _, err := s.Enqueue(ctx, &builddto.EnqueueReq{
		SourceType: "presentation", SourceID: presentationID,
		BuildInputHash: "",
	})
	return err
}

// List 按状态列出最近任务。
func (s *Service) List(ctx context.Context, req *builddto.ListReq) (list []*builddto.Job, err error) {
	status, limit := "", 0
	if req != nil {
		status, limit = strings.TrimSpace(req.Status), req.Limit
	}
	rows, err := s.m.ListRecent(ctx, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*builddto.Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDto(r))
	}
	return out, nil
}

// Retry 把失败任务退回队列。
func (s *Service) Retry(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New(buildenums.ErrInvalidParam)
	}
	jid, perr := strconv.ParseInt(id, 10, 64)
	if perr != nil {
		return errors.New(buildenums.ErrJobNotFound)
	}
	ok, err := s.m.RetryFailed(ctx, jid)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New(buildenums.ErrJobNotFound)
	}
	return nil
}

// ReclaimStale 回收僵尸任务。
func (s *Service) ReclaimStale(ctx context.Context) (reclaimed int64, err error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	return s.m.ReclaimStale(ctx, s.now().Add(-s.staleAfter))
}

// Stats 队列深度 + 最近失败任务（后台可见性）。
func (s *Service) Stats(ctx context.Context) (res *builddto.QueueStatsResp, err error) {
	res = &builddto.QueueStatsResp{RecentFailed: []builddto.Job{}}
	if s == nil || s.m == nil {
		return res, nil
	}
	// 顺手回收僵尸：后台打开队列页时就该看到「真实可消费的任务数」，
	// 而不是把卡死的 running 也算成在跑。
	if n, rerr := s.ReclaimStale(ctx); rerr == nil {
		res.StaleReclaimed = int(n)
	}
	rows, err := s.m.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		res.Total += r.Count
		switch r.Status {
		case buildmodel.StatusPending:
			res.Pending = r.Count
		case buildmodel.StatusRunning:
			res.Running = r.Count
		case buildmodel.StatusFailed:
			res.Failed = r.Count
		case buildmodel.StatusSucceeded:
			res.Succeeded = r.Count
		case buildmodel.StatusSuperseded:
			res.Superseded = r.Count
		}
	}
	if res.Failed > 0 {
		failed, ferr := s.m.ListRecent(ctx, buildmodel.StatusFailed, 10)
		if ferr == nil {
			for _, r := range failed {
				res.RecentFailed = append(res.RecentFailed, *toDto(r))
			}
		}
	}
	return res, nil
}

// toDto 实体 → 投影。
func toDto(e *buildmodel.Entity) *builddto.Job {
	if e == nil {
		return nil
	}
	msg := ""
	if e.ErrorMessage != nil {
		msg = *e.ErrorMessage
	}
	return &builddto.Job{
		ID: strconv.FormatInt(e.ID, 10), SourceType: e.SourceType, SourceID: e.SourceID,
		DraftVersion: e.DraftVersion, BuildInputHash: e.BuildInputHash, Status: e.Status,
		ArtifactID: e.ArtifactID, ErrorMessage: msg,
		CreatedAt: e.CreateTime, StartedAt: e.StartedAt, CompletedAt: e.CompletedAt,
	}
}
