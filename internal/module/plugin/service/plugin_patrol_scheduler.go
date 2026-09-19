package pluginservice

// plugin_patrol_scheduler.go —— 插件「三处产物」对账巡检的进程内调度。
//
// 为什么必须有这个文件：plugin_patrol.go 的 PatrolArtifacts 此前**没有任何调用方**，
// 而它盯的四类不一致全都是「没人会发现」的形态 —— 卸载时 DROP schema / 删注册行 /
// RemoveAll 目录三步各自可能单独失败（③ 是跨库动作，故意忽略错误），留下的残片既不占
// 后台版面也不报错：孤儿 schema 里的数据在等人发现，缺 schema 会让构建装配在建表时炸，
// 孤儿目录白占磁盘，目录缺失让装配**静默跳过**该插件（表现为「组件库里少了一组组件」
// 而没有任何提示）。没有定时驱动，这四类只能等出故障时倒查。
//
// 形状与既有调度器逐项一致（page_publish_converge.go / analytics_retention.go /
// mail_retention.go / order_expire.go）：首跑一次再按 ticker 等间隔、ctx 用
// context.Background()、目标方法报错只记日志、任何 panic 一律 recover 不拖垮进程。
//
// 这里只驱动**只读**巡检：四类不一致一律打回给人，本调度器不做任何自动清理
// （DROP SCHEMA ... CASCADE 会连着表里的数据一起删、RemoveAll 会连着里面的资产一起删，
// 孤儿里很可能装着真实业务数据 —— 「反正没注册」不是丢它的理由）。分类判据全部在
// plugin_patrol.go 的 classifyPatrol，本文件一行判定都不复制。

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// pluginPatrolInterval 巡检间隔。
	//
	// 按天：四类不一致只可能由「卸载中断 / 手工删目录 / 手工改库 / 镜像不完整」产生，
	// 都是低频事件，而巡检本身要读一次 information_schema（全库 schema 列表）+ 一次
	// 插件注册表 + 一次存储目录列举，是固定成本。一天一次足以让残片在一天内可见，
	// 更密只增加空转。
	pluginPatrolInterval = 24 * time.Hour
	// pluginPatrolTimeout 单轮巡检的超时。
	pluginPatrolTimeout = 5 * time.Minute
)

// pluginPatrolRoundResult 一轮巡检的结论（四类计数 + 存储根，供日志与用例断言）。
type pluginPatrolRoundResult struct {
	err            error
	orphanSchemas  int
	missingSchemas int
	orphanStorage  int
	missingStorage int
	storageRoot    string
}

// inconsistent 四类不一致的总数。
func (r pluginPatrolRoundResult) inconsistent() int {
	return r.orphanSchemas + r.missingSchemas + r.orphanStorage + r.missingStorage
}

// runPluginPatrolRound 跑一轮巡检：调既有的 PatrolArtifacts，把 panic 收敛成本轮失败。
func runPluginPatrolRound(ctx context.Context, svc *Service) (res pluginPatrolRoundResult) {
	if svc == nil {
		return res
	}
	defer func() {
		if r := recover(); r != nil {
			res.err = fmt.Errorf("panic: %v", r)
		}
	}()
	report, err := svc.PatrolArtifacts(ctx)
	if err != nil {
		res.err = err
		return res
	}
	res.storageRoot = report.StorageRoot
	res.orphanSchemas = len(report.OrphanSchemas)
	res.missingSchemas = len(report.MissingSchemas)
	res.orphanStorage = len(report.OrphanStorage)
	res.missingStorage = len(report.MissingStorage)
	return res
}

// runPluginPatrolLoop 调度循环本体：先跑一次，再按 interval 等间隔重复。
//
// 抽成「接受一轮动作」的形状是为了可测：目标方法要数据库与文件系统，模块内单测不碰
// 这些依赖，但调度语义（首跑 / 等间隔 / 单轮 panic 不致命）与动作内容无关 ——
// 这三件事写反了的表现都是「看起来在跑、其实什么都没做」。
// 返回的 stop 关闭后循环退出（生产不调用，测试用它收尾）。
func runPluginPatrolLoop(round func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = pluginPatrolInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		// 首跑：与样板一致，先跑一次再等 ticker（不是先等一个间隔）。
		safePluginPatrolRound(round)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				safePluginPatrolRound(round)
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// safePluginPatrolRound 执行一轮并把 panic 收敛成日志。
//
// goroutine 里未被 recover 的 panic 会直接终止整个进程，巡检任务没有这种权力：
// 一轮动作的 bug 只该让这一轮没有结论，下一个间隔照常再来。
func safePluginPatrolRound(round func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("plugin-patrol").Error(fmt.Errorf("panic: %v", r),
				"插件产物巡检单轮 panic（已收敛，下一轮照常；不影响进程）")
		}
	}()
	round()
}

// StartPluginPatrolScheduler 启动插件产物巡检调度（进程内 goroutine + ticker）。
func StartPluginPatrolScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startPluginPatrolScheduler(svc, pluginPatrolInterval)
}

// StartPluginPatrolSchedulerWithInterval 同上，但可注入间隔（用例用）。
func StartPluginPatrolSchedulerWithInterval(svc *Service, interval time.Duration) {
	startPluginPatrolScheduler(svc, interval)
}

func startPluginPatrolScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	runPluginPatrolLoop(func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), pluginPatrolTimeout)
		defer cancel()
		res := runPluginPatrolRound(ctx, svc)
		logPluginPatrolRound(res, time.Since(start))
	}, interval)
}

// logPluginPatrolRound 每轮记**一条**结构化日志（四类真实计数 + 存储根 + 耗时）。
//
// 与 service 内部日志的分工：PatrolArtifacts 对每一条不一致各记一行 Warn（那是给
// 「这一条是什么」用的，页面是给人看的当下状态，日志回答「什么时候开始有的」），
// 这一条回答「这一轮巡检跑完了没、四类各有几条、耗时多少」—— 两者读同一份
// PatrolResp，没有第二份判据。
//
// 分档：动作失败 → Error（结论不完整，下一轮重试）；存在任何一类不一致 → Warn
// 并写明「数据打回给人处理，本任务不自动修复」；否则 Info。
func logPluginPatrolRound(res pluginPatrolRoundResult, cost time.Duration) {
	entry := logger.Scene("plugin-patrol").
		With("storage_root", res.storageRoot).
		With("orphan_schemas", res.orphanSchemas).
		With("missing_schemas", res.missingSchemas).
		With("orphan_storage", res.orphanStorage).
		With("missing_storage", res.missingStorage).
		With("cost_ms", cost.Milliseconds())
	switch {
	case res.err != nil:
		entry.Error(res.err, "插件产物巡检调度：本轮动作失败，结论不完整（下一轮重试）")
	case res.inconsistent() > 0:
		entry.Warn("插件产物巡检完成：发现数据不一致，数据打回给人处理，本任务不自动修复")
	default:
		entry.Info("插件产物巡检完成：三处存储一致")
	}
}
