package pluginservice

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

func runPluginPatrolLoop(round func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = pluginPatrolInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {

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

func safePluginPatrolRound(round func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("plugin-patrol").Error(fmt.Errorf("panic: %v", r),
				"插件产物巡检单轮 panic（已收敛，下一轮照常；不影响进程）")
		}
	}()
	round()
}

func StartPluginPatrolScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
	}
	startPluginPatrolScheduler(svc, pluginPatrolInterval)
}

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
