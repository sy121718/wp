package pluginservice

// plugin_patrol.go — 插件「三处产物」的对账巡检（只读）。
//
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

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	plugindto "go_wp/internal/module/plugin/dto"
	pluginmodel "go_wp/internal/module/plugin/model"
	"go_wp/pkg/logger"
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
