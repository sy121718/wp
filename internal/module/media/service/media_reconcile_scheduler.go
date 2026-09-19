package mediaservice

// media_reconcile_scheduler.go —— 媒体「存储对账 + 变体补偿重放」的进程内调度。
//
// 为什么必须有这个文件：media_reconcile.go 里的两个入口此前**没有任何调用方**，
// 判定写得再准，没人驱动就等于不存在。而这两件事都只能靠时间驱动：
//
//   · 只读对账（ReconcileStorage）：文件系统与 PostgreSQL 这两处持久化永远可能对不上
//     （见 media_reconcile.go 开头的「为什么这里不能用事务」）—— 不存在任何写路径
//     能发现它们，只能靠周期性巡检把不一致找出来打回给人。
//   · 变体补偿重放（ReplayVariantBackfill）：上传路径的投递失败后只剩 asynq 自身的
//     3 次重试；重试耗尽后那条附件会**永久**停在「变体不齐」（srcset 给出打不开的地址），
//     没有任何入口会再碰它。
//
// 形状与既有调度器逐项一致（page_publish_converge.go / analytics_retention.go /
// mail_retention.go / order_expire.go）：首跑一次再按 ticker 等间隔、ctx 用
// context.Background()、目标方法报错只记日志、任何 panic 一律 recover 不拖垮进程。
//
// 这里驱动的是本模块既有的两个入口，**一行判定都不在这里**（不新造第二份真相）：
// 对账口径在 media_reconcile.go 与 media_crud.go，补偿的幂等性由 GenerateVariants
// 「先清旧记录再重建」保证。本文件只回答「谁在什么时候驱动它」。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// mediaReconcileInterval 巡检间隔。
	//
	// 只读对账按天：一轮要遍历整个存储目录 + 扫一批附件与变体行，是固定成本；而
	// 不一致的来源（进程在「落盘成功、DB 提交前」崩掉 / 磁盘故障 / 手工改库）不是
	// 分钟级事件 —— 一天一次足以让不一致在一天内可见。更密只增加空转，更疏则让
	// 报告失去「当下状态」的意义。
	//
	// 变体补偿重放与对账共用这一个 ticker（一轮里跑两个动作）：
	//   · 它满足纳入调度的两个条件 —— 文档注释（media_reconcile.go）明写**幂等**
	//     （GenerateVariants 重跑先清旧记录再重建）+ **可重放**（只处理仍不齐的那批，
	//     修好的下次不再命中），且修的是**派生的、可重新生成的**图片变体数据，
	//     不是用户原始资产；正常路径（上传时投递 + asynq 3 次重试）才是主修复通道，
	//     调度器只是它耗尽之后的兜底。
	//   · 一天一次对它是安全的：不会与上传时刚投递的那批抢同一批附件，也不会让一次
	//     失败的变体生成影响超过一天（重放本身幂等，多跑一次最坏白生成一次）。
	mediaReconcileInterval = 24 * time.Hour
	// mediaReconcileTimeout 单轮巡检的超时。
	//
	// 比 webhook / plugin 的巡检宽：对账要遍历文件系统，而队列未启用时变体补偿的
	// 降级分支会**同步**生成变体（scheduleVariants 见 media_variant_task.go），
	// 两者叠加可能是分钟级。
	mediaReconcileTimeout = 10 * time.Minute
)

// mediaReconcileRoundResult 一轮巡检的结论（只放真实计数，供日志与用例断言）。
type mediaReconcileRoundResult struct {
	// 只读对账
	reconcileErr     error
	storageRoot      string
	attachmentsTotal int64
	filesScanned     int
	variantsScanned  int
	missingFiles     int
	orphanFiles      int
	draftRows        int
	unattributed     int
	truncated        bool

	// 变体补偿重放
	backfillErr     error
	backfillScanned int
	backfillFixed   int
	backfillSkipped int
}

// inconsistent 本轮只读对账发现的不一致条数（三类清单之和）。
//
// 「无法归属」的文件（存量随机名）不计入：它不是不一致，只是认不出归属 ——
// 把它算进来会让每一轮都报「有不一致」，真不一致就被淹掉了。
func (r mediaReconcileRoundResult) inconsistent() int {
	return r.missingFiles + r.orphanFiles + r.draftRows
}

// runMediaReconcileRound 跑一轮巡检：先只读对账，再幂等变体补偿重放。
//
// 两个动作**各自 recover**：一个只读、一个会改（投递任务），任一个 panic 都不该让
// 另一个在本轮被跳过 —— 否则「一个动作的 bug」会伪装成「另一个动作也正常」。
func runMediaReconcileRound(ctx context.Context, svc *Service) (res mediaReconcileRoundResult) {
	if svc == nil {
		return res
	}
	func() {
		defer catchMediaActionPanic(&res.reconcileErr)
		report, err := svc.ReconcileStorage(ctx, nil)
		if err != nil {
			res.reconcileErr = err
			return
		}
		res.storageRoot = report.StorageRoot
		res.attachmentsTotal = report.AttachmentsTotal
		res.filesScanned = report.FilesScanned
		res.variantsScanned = report.VariantsScanned
		res.missingFiles = len(report.MissingFiles)
		res.orphanFiles = len(report.OrphanFiles)
		res.draftRows = len(report.DraftRows)
		res.unattributed = report.UnattributedFiles
		res.truncated = report.Truncated
	}()
	func() {
		defer catchMediaActionPanic(&res.backfillErr)
		report, err := svc.ReplayVariantBackfill(ctx, nil)
		if err != nil {
			res.backfillErr = err
			return
		}
		res.backfillScanned = report.Scanned
		res.backfillFixed = report.Fixed
		res.backfillSkipped = report.Skipped
	}()
	return res
}

// catchMediaActionPanic 把单个动作的 panic 收敛成该动作的错误（defer 用）。
func catchMediaActionPanic(target *error) {
	if r := recover(); r != nil {
		*target = fmt.Errorf("panic: %v", r)
	}
}

// runMediaReconcileLoop 调度循环本体：先跑一次，再按 interval 等间隔重复。
//
// 抽成「接受一轮动作」的形状是为了可测：目标方法要数据库与文件系统，模块内单测不碰
// 这些依赖，但**调度语义**（首跑 / 等间隔 / 单轮 panic 不致命）与动作内容无关，
// 可以独立断言 —— 这三件事写反了的表现都是「看起来在跑、其实什么都没做」。
//
// 返回的 stop 关闭后循环退出；生产装配不调用它（进程退出即结束），测试用它收尾。
func runMediaReconcileLoop(round func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = mediaReconcileInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		// 首跑：与样板一致，先跑一次再等 ticker（不是先等一个间隔）。
		safeMediaReconcileRound(round)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				safeMediaReconcileRound(round)
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// safeMediaReconcileRound 执行一轮并把 panic 收敛成日志。
//
// goroutine 里未被 recover 的 panic 会直接终止整个进程，巡检任务没有这种权力：
// 一轮动作的 bug 只该让这一轮没有结论，下一个间隔照常再来。
func safeMediaReconcileRound(round func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("media").Error(fmt.Errorf("panic: %v", r),
				"媒体存储巡检单轮 panic（已收敛，下一轮照常；不影响进程）")
		}
	}()
	round()
}

// StartMediaReconcileScheduler 启动媒体存储巡检调度（进程内 goroutine + ticker）。
func StartMediaReconcileScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startMediaReconcileScheduler(svc, mediaReconcileInterval)
}

// StartMediaReconcileSchedulerWithInterval 同上，但可注入间隔。
//
// 给用例用：注入远大于用例时长的间隔可以证成「首跑确实发生在启动时」，
// 注入毫秒级间隔可以证成「等间隔确实在驱动」——两者都不是单个断言能覆盖的。
func StartMediaReconcileSchedulerWithInterval(svc *Service, interval time.Duration) {
	startMediaReconcileScheduler(svc, interval)
}

func startMediaReconcileScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	runMediaReconcileLoop(func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), mediaReconcileTimeout)
		defer cancel()
		res := runMediaReconcileRound(ctx, svc)
		logMediaReconcileRound(res, time.Since(start))
	}, interval)
}

// logMediaReconcileRound 每轮记**一条**结构化日志（真实计数 + 耗时）。
//
// 与 service 内部日志的分工：ReconcileStorage 的 logReconcile 与 ReplayVariantBackfill
// 的逐条日志回答「这次动作做了什么」（带逐条明细），这一条回答「这一轮调度跑完了没、
// 两组动作各自的结论是什么」。两者读的是同一份报告结构体的字段，没有第二份判据。
//
// 分档：动作失败 → Error（结论不完整，下一轮重试）；只读对账发现不一致 → Warn
// （数据打回给人处理，本任务不自动修复）；重放了变体补偿 → Info（那是**幂等的派生数据**
// 自动修复，不是「要人处理的不一致」——把两者混成一个告警级别会让真不一致被淹掉）；
// 否则 Info。这里**只报告、绝不自动删文件**：对账的不一致一律打回给人
// （自动删会把「可能只是这次没扫到」变成不可逆的数据丢失），
// 唯一被自动处理的只有变体那个派生数据的幂等重放。
func logMediaReconcileRound(res mediaReconcileRoundResult, cost time.Duration) {
	entry := logger.Scene("media").
		With("storage_root", res.storageRoot).
		With("attachments_total", res.attachmentsTotal).
		With("files_scanned", res.filesScanned).
		With("variants_scanned", res.variantsScanned).
		With("missing_files", res.missingFiles).
		With("orphan_files", res.orphanFiles).
		With("draft_rows", res.draftRows).
		With("unattributed_files", res.unattributed).
		With("truncated", res.truncated).
		With("backfill_scanned", res.backfillScanned).
		With("backfill_fixed", res.backfillFixed).
		With("backfill_skipped", res.backfillSkipped).
		With("cost_ms", cost.Milliseconds())
	if err := errors.Join(res.reconcileErr, res.backfillErr); err != nil {
		entry.Error(err, "媒体存储巡检调度：本轮有动作失败，结论不完整（下一轮重试）")
		return
	}
	switch {
	case res.inconsistent() > 0:
		entry.Warn("媒体存储巡检完成：发现不一致，数据打回给人处理，本任务不自动修复")
	case res.backfillFixed > 0:
		entry.Info("媒体存储巡检完成：未发现不一致，已重新投递变体生成（幂等补偿）")
	default:
		entry.Info("媒体存储巡检完成：未发现不一致")
	}
}
