package pipeline

// dependency.go — 依赖 fan-out 内核（docs/03-pipeline.md §8.1/§8.2，PIPE-3）。
//
// 职责边界：
//   - 本文件只做「依赖源键 → 受影响来源 ID」的分发与编排，不认识任何业务模块；
//   - 具体反查（依赖表 JOIN 活跃/暂存指针）由各发布来源模块实现 DependencyTarget；
//   - 自动重建由各来源模块实现 StaleRebuilder（策略属来源，不属内核）。
//
// 为什么需要它：PIPE-3 之前，依赖记录只写进 Manifest（内存态），DB 依赖表
// 从未被写入，失效标记一律退化为「UPDATE pages SET stale = true」全站标记。
// 现在依赖源变更 → 按 (kind,key) 反查具体产物 → 只标记真正受影响的页面/实例。

import (
	"context"
	"sort"
	"sync"

	"go_wp/pkg/logger"
)

// 依赖类型常量（与 page_dependencies / presentation_dependencies 的 CHECK 约束一致，
// 见迁移 071 与 docs/03-pipeline.md §8.2）。
const (
	// DepKindDirectContent 单个 CMS 实体字段变化（product:{id}）。
	DepKindDirectContent = "direct_content"
	// DepKindContentCollection 集合成员变化（collection:content:{entityType}）。
	DepKindContentCollection = "content_collection"
	// DepKindContentTemplate ContentTemplate 版本变化。
	DepKindContentTemplate = "content_template"
	// DepKindMenu 菜单项变化。
	DepKindMenu = "menu"
	// DepKindMedia 媒体内容变化。
	DepKindMedia = "media"
	// DepKindBlock 全局块内容变化（页眉/页脚内联进产物）。
	DepKindBlock = "block"
	// DepKindSiteSlot 系统页面槽位（BIZ-1）换绑：产物里的链接路径烘在字节里。
	//
	// 与 DepKindBlock 的区别在于「依赖的是站点结构」而不是内容：页面文档里没有
	// 「我用了购物车槽位」这种声明，因此这条依赖只能由构建期记录（VIS-006）。
	DepKindSiteSlot = "site_slot"
	// DepKindSiteSetting 站点/工程设置变化。
	DepKindSiteSetting = "site_setting"
	// DepKindI18N 界面词条或内容译文变化（构建期取词进产物字节）。
	DepKindI18N = "i18n"
	// DepKindRuntime 非构建依赖（仅运行时 Fragment 缓存键，不触发重建）。
	DepKindRuntime = "runtime"
)

// DepKey 依赖源标识（kind + key）。key 的构造规则见 docs/03-pipeline.md §8.1 表。
type DepKey struct {
	Kind string
	Key  string
}

// DirectContentKey 单个内容实体依赖键，如 product:100。
func DirectContentKey(entityType, entityID string) DepKey {
	return DepKey{Kind: DepKindDirectContent, Key: entityType + ":" + entityID}
}

// ContentCollectionKey 内容集合依赖键，如 collection:content:product。
//
// 语义：该类型实体的**成员集合**变化（新增/删除/排序）会让所有渲染该集合的
// 产物失效——即使旧产物里还没有新实体（docs/03-pipeline.md §8.2 典型 fan-out）。
func ContentCollectionKey(entityType string) DepKey {
	return DepKey{Kind: DepKindContentCollection, Key: "collection:content:" + entityType}
}

// BlockKey 全局块依赖键，如 block:{blockID}。
// SiteSlotKey 系统页面槽位的依赖键（槽位名，如 cart / checkout）。
func SiteSlotKey(slot string) DepKey {
	return DepKey{Kind: DepKindSiteSlot, Key: slot}
}

func BlockKey(blockID string) DepKey {
	return DepKey{Kind: DepKindBlock, Key: "block:" + blockID}
}

// DependencyTarget 依赖失效目标：一个可被依赖源变更标记的发布来源
// （手工 Page / PresentationInstance 各自实现）。
//
// 来源类型（page / presentation）由注册方显式给出，不要求实现方额外暴露
// SourceType()——发布来源模块的契约只需表达「按依赖精确标记」这一件事。
type DependencyTarget interface {
	// MarkStaleByDependency 按 (kind,key) 精确标记受影响产物为 stale，
	// 返回受影响的来源 ID（页面 ID / 实例 ID）。实现必须只命中「当前活跃或
	// 暂存产物确实声明了该依赖」的来源，禁止退化为全站标记。
	MarkStaleByDependency(ctx context.Context, kind, key string) ([]string, error)
}

// targetEntry 已注册目标（来源类型 + 实现）。
type targetEntry struct {
	sourceType string
	target     DependencyTarget
}

// StaleRebuilder 自动重建端口：把被标记的来源按各自策略重建。
//
// 是否自动发布由实现决定（docs/03-pipeline.md §8.3：自动重建默认只产生
// staged Artifact；是否 publish 由内容类型的显式发布策略决定）。
type StaleRebuilder interface {
	RebuildStale(ctx context.Context, ids []string) error
}

// Fanout 依赖扇出编排：把依赖源变更分发给所有已注册目标，并可选触发自动重建。
//
// 线程安全：注册在装配期完成，失效调用可并发（内容 API 多请求并发）。
type Fanout struct {
	mu          sync.RWMutex
	targets     []targetEntry
	rebuilders  map[string]StaleRebuilder
	syncRebuild bool // 测试专用：内联重建，避免 go func 与断言竞态
}

// NewFanout 构造空扇出编排。
func NewFanout() *Fanout {
	return &Fanout{rebuilders: map[string]StaleRebuilder{}}
}

// Register 注册一个依赖失效目标（sourceType 为 page / presentation）。
func (f *Fanout) Register(sourceType string, t DependencyTarget) {
	if f == nil || t == nil || sourceType == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets = append(f.targets, targetEntry{sourceType: sourceType, target: t})
}

// RegisteredSourceTypes 返回已注册的失效来源类型（按注册顺序）。
//
// 为什么需要它：**漏注册一个发布来源不会报错**。该来源既不参与失效标记、也不参与
// 自动重建，表现是「内容更新了，但那一类页面永远停在旧版本」，日志里什么都没有 ——
// 审计 AR2-001 的 presentation 就是这样漏掉的（它的 MarkStaleByDependency 与
// RebuildStale 都写好了，只是没人把它注册进来）。装配期必须能断言「我期望的来源都在」，
// 否则连「少接了一个」这件事本身都无从发现。
func (f *Fanout) RegisteredSourceTypes() []string {
	if f == nil {
		return nil
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]string, 0, len(f.targets))
	for _, e := range f.targets {
		out = append(out, e.sourceType)
	}
	return out
}

// SetSyncRebuild 测试专用：为 true 时 RebuildStale 在当前 goroutine 执行（默认异步）。
func (f *Fanout) SetSyncRebuild(v bool) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncRebuild = v
}

// SetRebuilder 为某来源类型绑定自动重建实现（未绑定时只标记不重建）。
//
// 注意 r 为 nil 时**静默返回**（审计 CQ-019 点名的静默降级窗口）：调用方以为接上了，
// 实际表现是「内容变更照常标记 stale，但永远不自动重建」—— 线上内容停在旧版本
// 且没有任何报错。装配方（routes.go）因此先断言提供方实现了 StaleRebuilder 再调用。
func (f *Fanout) SetRebuilder(sourceType string, r StaleRebuilder) {
	if f == nil || sourceType == "" || r == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rebuilders == nil {
		f.rebuilders = map[string]StaleRebuilder{}
	}
	f.rebuilders[sourceType] = r
}

// Invalidate 单个依赖源键失效（content 等来源模块的出端口，见 content/contract）。
//
// 永不返回错误：依赖扇出是内容写入的**后置副作用**，失败只记日志，
// 不能因为「标记/重建失败」让已经成功的内容保存返回失败。
func (f *Fanout) Invalidate(ctx context.Context, kind, key string) {
	f.InvalidateKeys(ctx, DepKey{Kind: kind, Key: key})
}

// InvalidateKeys 批量失效并返回受影响来源 ID（sourceType → 去重排序后的 ID）。
//
// 顺序：先全部标记，再按来源类型触发一次自动重建（同一来源的多个依赖键
// 合并为一次重建，避免同一页面被重复构建）。
func (f *Fanout) InvalidateKeys(ctx context.Context, keys ...DepKey) map[string][]string {
	affected := map[string][]string{}
	if f == nil {
		return affected
	}
	f.mu.RLock()
	targets := append([]targetEntry(nil), f.targets...)
	rebuilders := make(map[string]StaleRebuilder, len(f.rebuilders))
	for k, v := range f.rebuilders {
		rebuilders[k] = v
	}
	syncRebuild := f.syncRebuild
	f.mu.RUnlock()

	seen := map[string]map[string]bool{}
	for _, key := range keys {
		if key.Kind == "" || key.Key == "" || key.Kind == DepKindRuntime {
			continue // runtime 依赖不触发重建（docs/03-pipeline.md §8.2）
		}
		for _, entry := range targets {
			ids, err := entry.target.MarkStaleByDependency(ctx, key.Kind, key.Key)
			if err != nil {
				logger.Scene("dependency").With("kind", key.Kind).With("key", key.Key).
					With("source", entry.sourceType).Error(err, "依赖失效标记失败（已降级，不影响内容写入）")
				continue
			}
			if len(ids) == 0 {
				continue
			}
			st := entry.sourceType
			if seen[st] == nil {
				seen[st] = map[string]bool{}
			}
			for _, id := range ids {
				if id == "" || seen[st][id] {
					continue
				}
				seen[st][id] = true
				affected[st] = append(affected[st], id)
			}
		}
	}

	for st, ids := range affected {
		sort.Strings(ids)
		r := rebuilders[st]
		if r == nil {
			continue
		}
		rebuildIDs := append([]string(nil), ids...)
		rebuilder := r
		sourceType := st
		runRebuild := func() {
			bg := context.Background()
			if err := rebuilder.RebuildStale(bg, rebuildIDs); err != nil {
				logger.Scene("dependency").With("source", sourceType).With("count", len(rebuildIDs)).
					Error(err, "依赖失效后的自动重建失败（已标记 stale，等待人工/下次触发）")
			}
		}
		if syncRebuild {
			runRebuild()
		} else {
			// 异步重建：内容写入不应被整站 Build+Publish 拖住 HTTP 请求。
			go runRebuild()
		}
	}
	return affected
}
