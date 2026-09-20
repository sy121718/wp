// Package buildcontract 定义 build 模块对外能力（构建任务队列，审计 DB-007）。
package buildcontract

import (
	"context"

	builddto "go_wp/internal/module/build/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract。
type (
	Job            = builddto.Job
	EnqueueReq     = builddto.EnqueueReq
	ListReq        = builddto.ListReq
	QueueStatsResp = builddto.QueueStatsResp
)

// 任务状态常量（跨模块传值用；与 build_jobs 的 CHECK 逐字对应）。
const (
	StatusPending    = "pending"
	StatusRunning    = "running"
	StatusSuperseded = "superseded"
	StatusFailed     = "failed"
	StatusSucceeded  = "succeeded"
)

// Executor 执行一条构建任务（由来源模块在装配期注册）。
//
// 返回 error 即任务失败（原因会写进 error_message 供后台查看与重试）；
// 执行器不需要自己改任务状态 —— 状态机在队列侧，执行器只回答「成没成」。
type Executor func(ctx context.Context, job *builddto.Job) error

// BuildService 构建任务队列能力。
type BuildService interface {
	// Enqueue 入队（同一目标同一构建输入的待办任务幂等，不会重复排队）。
	Enqueue(ctx context.Context, req *builddto.EnqueueReq) (job *builddto.Job, created bool, err error)
	// EnqueuePageBuild 供 page 模块把超出单次上限的自动重建交给队列。
	//
	// projectID 由调用方显式传入（审计 DB-01）：它是任务的工程作用域，队列不跨模块
	// 反查来源表，来源模块手里有就顺手带过来；确实拿不到时传空串（列可空）。
	// lang / intent 是任务上下文的另外两维（审计 ARCH-04），一起进待办去重键（迁移 307）：
	// lang 为完整语言码，intent 取 manual / dependency（空 = dependency）。
	EnqueuePageBuild(ctx context.Context, pageID, projectID, lang, intent string, draftVersion int64, buildInputHash string) error
	// EnqueuePresentationBuild 供 presentation 模块把自动重建交给队列（PERF-020）。
	// 同一实例的待办任务幂等（部分唯一索引去重），重复入队不会堆出多份。
	EnqueuePresentationBuild(ctx context.Context, presentationID, projectID string) error
	// RunOnce 取一条待办任务并执行；返回 false 表示队列为空。
	//
	// 认领是原子的：同一条任务只会被一个 worker 拿到，且同一来源同时最多一条在跑；
	// 认领时产生租约令牌，完成写入必须带回同一个令牌（旧 worker 的迟到结果会被丢弃）。
	RunOnce(ctx context.Context) (processed bool, err error)
	// ReclaimStale 回收租约到期未结束的 running 任务：
	// 与队列里的待办同键的合并为 superseded，其余退回 pending。返回退回队列的行数。
	ReclaimStale(ctx context.Context) (reclaimed int64, err error)
	// Stats 队列深度与最近失败任务（后台可见性）。
	Stats(ctx context.Context) (res *builddto.QueueStatsResp, err error)
	// List 按状态列出最近任务。
	List(ctx context.Context, req *builddto.ListReq) (list []*builddto.Job, err error)
	// Retry 把失败任务退回队列。
	Retry(ctx context.Context, id string) error
	// RegisterExecutor 注册某来源类型的执行器（装配期调用）。
	RegisterExecutor(sourceType string, fn Executor)
	// StartWorkers 启动 n 个消费协程（含僵尸回收）。
	StartWorkers(ctx context.Context, n int)
}
