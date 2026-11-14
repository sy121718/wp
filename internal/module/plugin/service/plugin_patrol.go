package pluginservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go_wp/internal/module/plugin/dto"
	"go_wp/internal/module/plugin/model"
	"go_wp/pkg/lifecycle"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

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
	sort.Strings(out)
	return out
}

func storageDirExists(row *pluginmodel.Entity) bool {
	if row == nil || strings.TrimSpace(row.StoragePath) == "" {
		return false
	}
	st, err := os.Stat(row.StoragePath)
	return err == nil && st.IsDir()
}

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

	for _, dir := range storageDirs {
		if _, ok := registered[dir]; !ok {
			res.OrphanStorage = append(res.OrphanStorage, dir)
		}
	}

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

func pluginStorageRelPath() string { return filepath.ToSlash(pluginStorageRoot()) }

const (
	pluginPatrolInterval = 24 * time.Hour

	pluginPatrolTimeout = 5 * time.Minute
)

type pluginPatrolRoundResult struct {
	err            error
	orphanSchemas  int
	missingSchemas int
	orphanStorage  int
	missingStorage int
	storageRoot    string
}

func (r pluginPatrolRoundResult) inconsistent() int {
	return r.orphanSchemas + r.missingSchemas + r.orphanStorage + r.missingStorage
}

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

// runPluginPatrolLoop 用共享的最小周期原语启动巡检循环，返回 quit 与 wait。
//
// quit 停止新轮；wait 等在途的一轮结束后返回 —— 调用方必须在关闭数据库之前 wait。
// 循环 ctx 从 parent 派生：应用 ctx 取消后循环退出，不再启动新轮。
func runPluginPatrolLoop(parent context.Context, round func(context.Context), interval time.Duration) (stop func(), wait func()) {
	if interval <= 0 {
		interval = pluginPatrolInterval
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	task := lifecycle.PeriodicTask{
		Interval: interval,
		Timeout:  pluginPatrolTimeout,
		Round:    round,
		OnPanic: func(r any) {
			logger.Scene("plugin-patrol").Error(fmt.Errorf("panic: %v", r),
				"插件产物巡检单轮 panic（已收敛，下一轮照常；不影响进程）")
		},
	}
	waitFn := task.Start(ctx)
	return cancel, waitFn
}

func safePluginPatrolRound(round func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("plugin-patrol").Error(fmt.Errorf("panic: %v", r),
				"插件产物巡检单轮 panic（已收敛，下一轮照常；不影响进程）")
		}
	}()
	round()
}

// StartPluginPatrolScheduler 启动巡检；返回的 stop 取消循环并等待在途一轮结束。
func StartPluginPatrolScheduler(ctx context.Context, svc *Service) func() {
	if utils.IsTestProcess() {
		return func() {}
	}
	return startPluginPatrolScheduler(ctx, svc, pluginPatrolInterval)
}

// StartPluginPatrolSchedulerWithInterval 与上同，只是显式指定间隔。
func StartPluginPatrolSchedulerWithInterval(ctx context.Context, svc *Service, interval time.Duration) func() {
	return startPluginPatrolScheduler(ctx, svc, interval)
}

func startPluginPatrolScheduler(ctx context.Context, svc *Service, interval time.Duration) func() {
	if svc == nil {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stop, wait := runPluginPatrolLoop(ctx, func(runCtx context.Context) {
		start := time.Now()
		res := runPluginPatrolRound(runCtx, svc)
		logPluginPatrolRound(res, time.Since(start))
	}, interval)
	return func() {
		stop()
		wait()
	}
}

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
