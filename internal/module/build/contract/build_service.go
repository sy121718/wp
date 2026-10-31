// Package buildcontract 定义 build 模块对外能力（构建任务队列，审计 DB-007）。
package buildcontract

import (
	"context"

	"gorm.io/gorm"

	builddto "go_wp/internal/module/build/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract。
type (
	Job            = builddto.Job
	EnqueueReq     = builddto.EnqueueReq
	ListReq        = builddto.ListReq
	QueueStatsResp = builddto.QueueStatsResp
)

// MenuStaleSourceID / SplitMenuStaleSourceID 是 menu_stale 来源标识的编解码，对消费方暴露为**函数**。
//
// 为什么是 func 包装而不是 var 重导出（var X = builddto.Y）：var 是可写变量，任何一处
// contract.SplitMenuStaleSourceID = ... 都能在运行期换掉全部消费方的解析规则，而契约层
// 的语义正好要求「不可变」。包装还能把实现细节（dto 包的存在）挡在契约后面：
// 消费方只依赖 contract，dto 以后拆文件 / 改签名都不会穿透到它们。
func MenuStaleSourceID(projectID, kind string) (sourceID string, ok bool) {
	return builddto.MenuStaleSourceID(projectID, kind)
}

// SplitMenuStaleSourceID 拆 menu_stale 的 source_id（worker 消费侧据此还原工程与位置）。
func SplitMenuStaleSourceID(sourceID string) (projectID, kind string, ok bool) {
	return builddto.SplitMenuStaleSourceID(sourceID)
}

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
	//
	// req.SourceID 必须是 uuid：入队前归一到规范文本（迁移 698 之后 source_id 是 text 列，
	// 逐字比较 —— 不归一会让同一实体的大写 / 无连字符写法各占一条互斥键与去重键）。
	// 非法 UUID 返回 ErrInvalidParam（400）。只收 page / presentation；menu_stale 走
	// EnqueueMenuStaleTx 这条同事务专用入口。
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
	// EnqueueMenuStaleTx 在调用方事务里为「某工程的某个菜单位置」可靠入队一条 menu_stale 任务，
	// 返回本次分配的输入版本号（单调递增，同版本唯一）。
	//
	// 契约边界（逐条都是调用方的责任，本方法一件都不代做）：
	//   - tx 必须是调用方已开启、已 scope 的真实事务；本方法不开事务、不提交、不回滚、不设 scope；
	//   - 只在**确有真实变化**的这一条路径上调用：每次调用都会分配一个新的、更大的版本号；
	//   - 返回的版本号进 build_input_hash（与 source_id 一起），因此 A→B→A 的来回改动
	//     仍然是三次不同的工作，不会被队列里的旧任务吞掉；
	//   - 失败时返回 error 且不留任务：版本号与任务行都在同一个事务里，由调用方统一回滚。
	EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (inputVersion int64, err error)
	// RunOnce 取一条待办任务并执行；返回 false 表示队列为空。
	//
	// 认领是原子的：同一条任务只会被一个 worker 拿到，且同一来源同时最多一条在跑；
	// 认领时产生租约令牌，完成写入必须带回同一个令牌（旧 worker 的迟到结果会被丢弃）。
	RunOnce(ctx context.Context) (processed bool, err error)
	// ReclaimStale 回收租约到期未结束的 running 任务：
	// 与队列里的待办同键的合并为 superseded，其余退回 pending。返回退回队列的行数。
	ReclaimStale(ctx context.Context) (reclaimed int64, err error)
	// Stats 队列深度与最近失败任务（后台可见性）；projectID 非空时只统计该工程。
	Stats(ctx context.Context, projectID string) (res *builddto.QueueStatsResp, err error)
	// List 按状态列出最近任务（req.ProjectID 非空时只列该工程）。
	List(ctx context.Context, req *builddto.ListReq) (list []*builddto.Job, err error)
	// Retry 把失败任务退回队列；projectID 非空时只允许退回该工程的任务。
	Retry(ctx context.Context, projectID, id string) error
	// RegisterExecutor 注册某来源类型的执行器（装配期调用）。
	RegisterExecutor(sourceType string, fn Executor)
	// StartWorkers 启动 n 个消费协程（含僵尸回收）。
	StartWorkers(ctx context.Context, n int)
}
