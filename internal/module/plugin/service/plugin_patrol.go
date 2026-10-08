package pluginservice

// 一次卸载要清三处**互相独立**的存储（见 plugin_service.go 的 Uninstall）：
//
//   ① L1 schema   plugin_<id>（PostgreSQL）
//   ② 注册行       plugin_registry（PostgreSQL）
//   ③ 存储目录     <pluginStorageRoot>/<id>（文件系统）
//
// ①②能同事务，③不能（跨库）—— 所以 Uninstall 的顺序是「先 DROP schema、再删注册行、
// 最后 RemoveAll 目录」，且 removePluginStorage 故意忽略错误（`_ = os.RemoveAll(...)`）。
// 这些取舍都合理，代价是**每一步都可能单独失败**，而失败留下的残片没有任何入口能发现：
//
//   · 孤儿 schema —— schema 还在、注册行没了（进程在 DROP 与 Delete 之间崩掉，或有人手改库）；
//     注册行没了就再也没有触发清理的入口，只能靠人。
//   · 缺 schema —— 注册行声明 schema_version > 0 但 schema 不在。插件列表看着正常，
//     直到构建装配在建表 / 查表时炸。
//   · 孤儿目录 —— 注册行删了、RemoveAll 失败（权限 / 文件占用 / 磁盘错误），
//     目录留在磁盘上，既不占后台版面也不报错。
//   · 目录缺失 —— 注册行在、目录不在（被误删 / 镜像不完整）。装配会静默跳过该插件
//     （plugin_runtime.go 的 os.Stat 分支），表现为「组件库里少了一组组件」而没有任何提示。
//
// 四类都是**数据不一致**。按 AGENTS.md「冲突与数据不一致一律打回给人」：巡检**只报告、
// 不自动清理** —— DROP SCHEMA ... CASCADE 会连表里的数据一起删，RemoveAll 会连里面的
// 资产一起删；孤儿里很可能装着真实业务数据或还没迁移走的资产，「反正没注册」不是丢它的理由。

// 为什么必须有这个文件：plugin_patrol.go 的 PatrolArtifacts 此前**没有任何调用方**，
// 而它盯的四类不一致全都是「没人会发现」的形态 —— 卸载时 DROP schema / 删注册行 /
// RemoveAll 目录三步各自可能单独失败（③ 是跨库动作，故意忽略错误），留下的残片既不占
// 后台版面也不报错：孤儿 schema 里的数据在等人发现，缺 schema 会让构建装配在建表时炸，
// 孤儿目录白占磁盘，目录缺失让装配**静默跳过**该插件（表现为「组件库里少了一组组件」
// 而没有任何提示）。没有定时驱动，这四类只能等出故障时倒查。
//
// 形状与既有调度器逐项一致（page_publish_converge.go / analytics_retention.go /
// context.Background()、目标方法报错只记日志、任何 panic 一律 recover 不拖垮进程。
//
// 这里只驱动**只读**巡检：四类不一致一律打回给人，本调度器不做任何自动清理
// （DROP SCHEMA ... CASCADE 会连着表里的数据一起删、RemoveAll 会连着里面的资产一起删，
// 孤儿里很可能装着真实业务数据 —— 「反正没注册」不是丢它的理由）。分类判据全部在

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go_wp/internal/module/plugin/dto"
	"go_wp/internal/module/plugin/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// PatrolArtifacts 巡检插件在三处存储上的一致性（只读，不修任何数据）。
func (s *Service) PatrolArtifacts(ctx context.Context) (res *plugindto.PatrolResp, err error) {
	schemas, err := s.m.ListSchemas(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.List(ctx)
	if err != nil {
		return nil, err
	}
	res = classifyPatrol(schemas, listPluginStorageDirs(), rows, storageDirExists)
	res.StorageRoot = pluginStorageRelPath()
	// 每一条不一致都留结构化日志：页面是给人看的当下状态，
	// 「什么时候开始有的」只能从日志回答。
	for _, o := range res.OrphanSchemas {
		logger.Scene("plugin-patrol").
			With("schema", o.Name).With("table_count", o.TableCount).
			Warn("发现孤儿插件 schema：注册表里没有对应插件，需人工确认后清理")
	}
	for _, id := range res.MissingSchemas {
		logger.Scene("plugin-patrol").With("plugin_id", id).
			Warn("插件注册行声明了 schema_version，但对应 schema 不存在（构建装配会失败）")
	}
	for _, id := range res.OrphanStorage {
		logger.Scene("plugin-patrol").With("plugin_id", id).
			Warn("发现孤儿插件存储目录：注册表里没有对应插件（卸载时 RemoveAll 未成功）")
	}
	for _, id := range res.MissingStorage {
		logger.Scene("plugin-patrol").With("plugin_id", id).
			Warn("插件注册行存在，但存储目录不在（装配会静默跳过该插件）")
	}
	return res, nil
}

// listPluginStorageDirs 列出插件存储根下的一级子目录名（= 插件 ID）。
//
// 根目录不存在时返回空切片而不是 error：插件一个都没装过的库本来就该没有这个目录，
// 那不是巡检失败。只取目录、不取文件 —— 根下若有人放过 README 之类的散文件，
// 那是无关噪音，报成「孤儿插件」只会稀释真正的告警。
func listPluginStorageDirs() []string {
	entries, err := os.ReadDir(pluginStorageRoot())
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out) // 输出确定性：巡检结果会进日志与页面，顺序抖动会让 diff 无意义
	return out
}

// storageDirExists 注册行的存储目录是否真的在磁盘上。
//
// 判据用「目录」而不是「路径存在」：路径被换成同名文件同样是坏状态，
// 而装配路径（plugin_runtime.go）判的也是 IsDir，两处口径必须一致 ——
// 口径不一致的代价是巡检说「正常」而装配仍然跳过，那种巡检比没有更坏。
func storageDirExists(row *pluginmodel.Entity) bool {
	if row == nil || strings.TrimSpace(row.StoragePath) == "" {
		return false
	}
	st, err := os.Stat(row.StoragePath)
	return err == nil && st.IsDir()
}

// classifyPatrol 把三处存储对账成四类不一致（纯函数，便于单测）。
//
// 抽成纯函数是因为**分类错了不会报错**：最坏的一种是把正在使用的插件报成孤儿，
// 然后有人照着报告去 DROP 它或删目录。storageExists 以函数注入，
// 让这条判据在测试里可控（不碰真实文件系统）。
//
// 只对 schema_version > 0 的注册行要求 schema：schema_version = 0 的插件按设计就没有
// L1 数据结构（纯组件 / 纯模板插件），把它算成「缺 schema」会制造一整片假告警，
// 而假告警会让真告警一起被忽略。
func classifyPatrol(
	schemas []pluginmodel.SchemaInfo,
	storageDirs []string,
	rows []*pluginmodel.Entity,
	storageExists func(*pluginmodel.Entity) bool,
) (res *plugindto.PatrolResp) {
	res = &plugindto.PatrolResp{
		OrphanSchemas:  []plugindto.SchemaInfo{},
		MissingSchemas: []string{},
		OrphanStorage:  []string{},
		MissingStorage: []string{},
	}

	registered := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if r != nil {
			registered[r.PluginID] = struct{}{}
		}
	}

	// 孤儿 schema：用 schema 名**反推**插件 ID（去掉前缀），而不是拿 PluginID 去拼 schema 名 ——
	// 拼名字只能验证「注册行对应的 schema 在不在」，反推才能发现**多出来的** schema。
	for _, sc := range schemas {
		id := strings.TrimPrefix(sc.Name, schemaPrefix)
		if _, ok := registered[id]; !ok {
			res.OrphanSchemas = append(res.OrphanSchemas, plugindto.SchemaInfo{Name: sc.Name, TableCount: sc.TableCount})
		}
	}

	present := make(map[string]struct{}, len(schemas))
	for _, sc := range schemas {
		present[sc.Name] = struct{}{}
	}
	for _, r := range rows {
		if r == nil || r.SchemaVersion <= 0 {
			continue
		}
		if _, ok := present[schemaNameFor(r.PluginID)]; !ok {
			res.MissingSchemas = append(res.MissingSchemas, r.PluginID)
		}
	}

	// 孤儿目录：根下有一级目录、注册表里没有对应 ID。
	for _, dir := range storageDirs {
		if _, ok := registered[dir]; !ok {
			res.OrphanStorage = append(res.OrphanStorage, dir)
		}
	}

	// 目录缺失：注册行在、目录不在（或路径不是目录）。
	for _, r := range rows {
		if r == nil {
			continue
		}
		if !storageExists(r) {
			res.MissingStorage = append(res.MissingStorage, r.PluginID)
		}
	}
	return res
}

// pluginStorageRelPath 存储根的展示用相对路径（页面报给人的是路径，不是 ID）——
// 这里只做展示消歧，实际读写一律走 pluginStorageRoot()。
func pluginStorageRelPath() string { return filepath.ToSlash(pluginStorageRoot()) }

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
