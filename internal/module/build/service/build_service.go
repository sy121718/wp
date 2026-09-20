// Package buildservice 构建任务队列：入队、消费、回收与可见性（审计 DB-007 / DB-01）。
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
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

var _ buildcontract.BuildService = (*Service)(nil)

// 队列的默认节奏。
const (
	// defaultWorkerIdle 队列空转时的休眠间隔。
	// 1 秒的取舍：队列里的任务通常成批到来（依赖失效扇出），
	// 用 LISTEN/NOTIFY 唤醒能把延迟压到毫秒，但要多一条长连接与一套重连逻辑 ——
	// 对「内容改完等几秒站点更新」这个场景不值。
	defaultWorkerIdle = time.Second
	// defaultLeaseTTL running 任务的租约时长：到期即视为「worker 已经做不完 / 已经不在了」。
	// 取 15 分钟：单页构建是秒级，全站级任务也远低于这个量级；阈值太短会让正常的长任务
	// 被回收重做（同一份工作做两遍），太长则僵尸任务会占着来源不放。
	//
	// 它与 jobTimeout（10 分钟）是同一条时间轴上的两个刻度：先由执行侧的 context 超时
	// 把任务判失败，租约到期只是「连失败都没来得及写」时的兜底。
	defaultLeaseTTL = 15 * time.Minute
	// defaultJobTimeout 单条任务的执行上限（避免一条任务永久占着 worker）。
	defaultJobTimeout = 10 * time.Minute
)

// Service 构建队列服务。
type Service struct {
	m *buildmodel.Model

	mu        sync.RWMutex
	executors map[string]buildcontract.Executor

	workerIdle time.Duration
	leaseTTL   time.Duration
	jobTimeout time.Duration
	now        func() time.Time
}

// NewService 构造。
func NewService(m *buildmodel.Model) *Service {
	return &Service{
		m:          m,
		executors:  map[string]buildcontract.Executor{},
		workerIdle: defaultWorkerIdle,
		leaseTTL:   defaultLeaseTTL,
		jobTimeout: defaultJobTimeout,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// Model 暴露底层 model 供测试检查队列行。
func (s *Service) Model() *buildmodel.Model { return s.m }

// SetLeaseTTL 覆盖租约时长（测试用；正常部署用 defaultLeaseTTL）。
//
// 它同时决定 claim 发出的 lease_expires_time 与回收的判定线，两者必须是同一个值 ——
// 分开设置会出现「刚认领就被判过期」这种自己咬自己的配置。
func (s *Service) SetLeaseTTL(d time.Duration) { s.leaseTTL = d }

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

// normalizeIntent 归一化构建意图（空 = 依赖重建；白名单外的值直接拒绝）。
//
// 白名单与迁移 295 的 CHECK 一致，但先在这里拦：数据库拒绝的报错是约束名，
// 这里能给出「意图不支持」这个可读结论，也不会留下半条任务。
func normalizeIntent(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "":
		return buildmodel.IntentDependency, true
	case buildmodel.IntentManual:
		return buildmodel.IntentManual, true
	case buildmodel.IntentDependency:
		return buildmodel.IntentDependency, true
	default:
		return "", false
	}
}

// Enqueue 入队一条构建任务。
//
// created=false 表示同一目标、同一构建输入的工作已在队列里（迁移 171 的部分唯一索引兜底）。
// 注意它**不覆盖正在跑的那份工作**：内容在构建期间又变了就该再排一份，
// 队列里那份待办会在这条 running 结束后被认领。
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
	intent, ok := normalizeIntent(req.Intent)
	if !ok {
		return nil, false, errors.New(buildenums.ErrInvalidParam)
	}
	e := &buildmodel.Entity{
		SourceType: sourceType, SourceID: sourceID,
		// 工程 / 语言 / 意图显式入库（DB-01）：消费侧要按工程设作用域、
		// 后台要能区分人工构建与依赖重建，都不能靠反查来源行或猜。
		ProjectID:      nullableString(req.ProjectID),
		Lang:           strings.TrimSpace(req.Lang),
		Intent:         intent,
		DraftVersion:   req.DraftVersion,
		BuildInputHash: strings.TrimSpace(req.BuildInputHash),
		Status:         buildmodel.StatusPending, CreateTime: s.now(),
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

// EnqueuePageBuild 供 page 模块把依赖重建 / 人工构建任务交给队列（审计 ARCH-04）。
//
// 参数按「任务上下文」排布：lang（构建语言）与 intent（构建意图）紧邻，
// draftVersion / buildInputHash 是**入队时冻结的输入版本**。
// 三个一起进待办去重键（迁移 307）：同一页面不同语言是不同工作、人工构建与依赖重建
// 更是两回事，任何一维缺失都会被误去重（多语言站点少构建一种语言 / 手工构建被吞掉）。
//
// intent 传空 = 依赖重建（normalizeIntent 的默认值，见 builddto.EnqueueReq 注释）。
func (s *Service) EnqueuePageBuild(ctx context.Context, pageID, projectID, lang, intent string, draftVersion int64, buildInputHash string) error {
	_, _, err := s.Enqueue(ctx, &builddto.EnqueueReq{
		SourceType: buildmodel.SourceTypePage, SourceID: pageID, ProjectID: projectID,
		Lang: strings.TrimSpace(lang), Intent: intent,
		DraftVersion: draftVersion, BuildInputHash: buildInputHash,
	})
	return err
}

// EnqueuePresentationBuild 供 presentation 模块把自动重建交给队列（PERF-020）。
//
// build_input_hash 传空串是刻意的：自动发布实例没有「页面草稿」这种输入版本，
// 空串 + 固定的 lang（空）与 intent（依赖重建）让「同一实例同时只有一条待办」成立 ——
// 依赖失效扇出反复标记同一实例时不会堆出多份任务。它是**幂等键的一个分量**，
// 不是「这里是什么意图」的操作码：意图由 intent 列显式表达（迁移 295 / ARCH-04）。
func (s *Service) EnqueuePresentationBuild(ctx context.Context, presentationID, projectID string) error {
	_, _, err := s.Enqueue(ctx, &builddto.EnqueueReq{
		SourceType: buildmodel.SourceTypePresentation, SourceID: presentationID, ProjectID: projectID,
		BuildInputHash: "",
		Intent:         buildmodel.IntentDependency,
	})
	return err
}

// List 按状态列出最近任务；req.ProjectID 非空时只列该工程。
//
// 工程过滤走 SQL（build_jobs **没有** RLS 策略，见 buildmodel 的说明）：
// 换非超级角色后不带过滤的查询不会 fail closed，而是把别的工程的任务一起列出来。
func (s *Service) List(ctx context.Context, req *builddto.ListReq) (list []*builddto.Job, err error) {
	projectID, status, limit := "", "", 0
	if req != nil {
		projectID, status, limit = strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.Status), req.Limit
	}
	rows, err := s.m.ListRecent(ctx, projectID, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*builddto.Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDto(r))
	}
	return out, nil
}

// Retry 把失败任务退回队列；projectID 非空时只允许退回该工程的任务。
//
// 同样因为 build_jobs 没有 RLS 策略：不带工程过滤的重试会把**别的工程**的失败任务
// 一起退回去（跨工程写）。projectID 为空保持既有语义（全局运维视角）。
func (s *Service) Retry(ctx context.Context, projectID, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New(buildenums.ErrInvalidParam)
	}
	jid, perr := strconv.ParseInt(id, 10, 64)
	if perr != nil {
		return errors.New(buildenums.ErrJobNotFound)
	}
	ok, err := s.m.RetryFailed(ctx, jid, strings.TrimSpace(projectID))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New(buildenums.ErrJobNotFound)
	}
	return nil
}

// ReclaimStale 回收租约到期的任务，返回退回 pending 的行数。
//
// 同键已有待办的陈旧任务会被合并为 superseded（不计入返回值，但记日志）：
// 旧实现把这类行也改回 pending，撞上唯一索引 23505，一条坏任务就能让
// 整条回收语句失败、其它僵尸任务继续卡在 running 上（审计 DB-01）。
func (s *Service) ReclaimStale(ctx context.Context) (reclaimed int64, err error) {
	res, err := s.reclaimStale(ctx)
	return res.Reclaimed, err
}

// reclaimStale 回收的内部入口：service 的两个调用点（worker 定时回收、后台队列页顺手回收）
// 走同一段逻辑，合并计数也只在日志里出现一次。
func (s *Service) reclaimStale(ctx context.Context) (buildmodel.ReclaimResult, error) {
	if s == nil || s.m == nil {
		return buildmodel.ReclaimResult{}, nil
	}
	res, err := s.m.ReclaimStale(ctx, s.now())
	if err != nil {
		return res, err
	}
	if res.Merged > 0 {
		logger.Scene("build").With("merged", res.Merged).
			Warn("陈旧构建任务与队列里的待办重复，已合并为 superseded")
	}
	return res, nil
}

// Stats 队列深度 + 最近失败任务（后台可见性）；projectID 非空时只统计该工程。
//
// 工程过滤走 SQL（build_jobs 没有 RLS 策略，见 buildmodel 的说明）。
// 顺手做的僵尸回收（reclaimStale）**始终是全局的**：它是队列自身的维护动作，
// 与「看哪个工程的队列」无关 —— 只回收某个工程的僵尸任务会让其它工程的僵尸一直卡着。
func (s *Service) Stats(ctx context.Context, projectID string) (res *builddto.QueueStatsResp, err error) {
	res = &builddto.QueueStatsResp{RecentFailed: []builddto.Job{}}
	if s == nil || s.m == nil {
		return res, nil
	}
	projectID = strings.TrimSpace(projectID)
	// 顺手回收僵尸：后台打开队列页时就该看到「真实可消费的任务数」，
	// 而不是把卡死的 running 也算成在跑。
	if r, rerr := s.reclaimStale(ctx); rerr == nil {
		res.StaleReclaimed = int(r.Reclaimed)
		res.StaleMerged = int(r.Merged)
	}
	rows, err := s.m.CountByStatus(ctx, projectID)
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
		failed, ferr := s.m.ListRecent(ctx, projectID, buildmodel.StatusFailed, 10)
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
	projectID := ""
	if e.ProjectID != nil {
		projectID = *e.ProjectID
	}
	return &builddto.Job{
		ID: strconv.FormatInt(e.ID, 10), SourceType: e.SourceType, SourceID: e.SourceID,
		ProjectID: projectID, Lang: e.Lang, Intent: e.Intent,
		DraftVersion: e.DraftVersion, BuildInputHash: e.BuildInputHash, Status: e.Status,
		ArtifactID: e.ArtifactID, ErrorMessage: msg, Attempt: e.Attempt,
		CreatedAt: utils.NewJSONTime(e.CreateTime), StartedAt: utils.NewJSONTimePtr(e.StartedAt), CompletedAt: utils.NewJSONTimePtr(e.CompletedAt),
	}
}

// nullableString 空串 → NULL（列可空语义：没拿到就留空，而不是塞一个空串进 uuid 列）。
func nullableString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}
