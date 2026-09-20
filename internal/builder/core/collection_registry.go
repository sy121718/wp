package core

// collection_registry.go — 集合源注册表（装配期注册，构建期只读）。
//
// 由来：集合解析此前只有内容模块一个实现（"content:{entityType}"），新领域
// （商品）接入时只能在「改内容模块」与「装配层二选一」之间选 —— 前者把不相关
// 领域拖进内容模块，后者让两个集合源无法共存。
//
// 注册表把「集合源 → 解析器」变成装配期注册：各领域模块注册自己的集合源，
// 构建层（集合类组件 / 集合元数据接口）只认注册表，不认识具体领域模块
// （与实体类型注册表 core.EntitySourceRegistry 同一口径）。
//
// 白名单仍由各领域模块自己维护：注册表只做分发与聚合，不定义字段。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// CollectionSourceProvider 一个集合源提供方：解析 + 元数据成对出现。
//
// 成对是刻意的：只给解析不给元数据的实现会让集合类组件退回「按数据实际字段
// 判断」，白名单校验形同虚设（不变量 4）。新领域模块接入时必须两个都给。
type CollectionSourceProvider interface {
	CollectionResolver
	CollectionSchemaProvider
}

// CollectionRegistry 集合源注册表。
//
// 实现 core.CollectionResolver（按源分发解析）与 core.CollectionSchemaProvider
// （聚合各领域模块的集合源元数据），因此可直接作为编译期注入项使用。
type CollectionRegistry interface {
	CollectionResolver
	CollectionSchemaProvider
	// ResolveCollectionPage 按页取数（审计 PERF-019）。注册表**无条件**实现它：
	// 注册的解析器支持分页就转发，不支持就退化成「取一批再截断」并返回 Total = -1。
	// 因此调用方不需要对注册表本身做能力探测 —— 可选性在 provider 那一层。
	ResolveCollectionPage(ctx context.Context, source string, q CollectionQuery) (CollectionPage, error)
	// CollectionFilterOptionsProvider 集合源的**可选**筛选能力（issue #27 的筛选栏）。
	// 注册表实现它：按注册时各提供方自报的集合源，把「本工程有哪些可筛值」的请求
	// 转发给对应提供方；未注册的源 / 该源没有这项能力的，返回空选项而不是报错。
	//
	// 为什么能力要落在注册表上：组件只拿得到 ctx.Collection（装配点注入的就是注册表），
	// 而「哪个源由谁提供」只有注册表知道 —— 在组件侧对 ctx.Collection 做能力断言
	// 等于要求注册表**自己**长出业务能力，断言必然失败、筛选栏静默全空（ARCH-01）。
	CollectionFilterOptionsProvider
	// Register 注册一个集合源提供方；nil / 无集合源 / 源标识重复返回错误。
	Register(p CollectionSourceProvider) error
	// Sources 已注册的集合源标识（字典序，确定性）。
	Sources(ctx context.Context) ([]string, error)
}

// collectionRegistry CollectionRegistry 默认实现。
//
// 装配期串行注册（单线程），运行期并发只读：写用 Lock，读用 RLock；
// 索引在注册时建立，读路径不重复调用 CollectionSchemas。
type collectionRegistry struct {
	mu       sync.RWMutex
	ordered  []CollectionSourceProvider
	bySource map[string]CollectionSourceProvider
}

// NewCollectionRegistry 新建空注册表。
func NewCollectionRegistry() CollectionRegistry {
	return &collectionRegistry{bySource: map[string]CollectionSourceProvider{}}
}

// Register 实现 CollectionRegistry。
func (r *collectionRegistry) Register(p CollectionSourceProvider) error {
	if p == nil {
		return fmt.Errorf("集合源提供方为空")
	}
	// 装配期即取元数据：源标识非法 / 提供方自相矛盾在启动时暴露，
	// 而不是等某次构建渲染到一半才报错。
	schemas, err := p.CollectionSchemas(context.Background())
	if err != nil {
		return fmt.Errorf("集合源元数据不可用: %w", err)
	}
	if len(schemas) == 0 {
		return fmt.Errorf("集合源提供方未声明任何集合源")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range schemas {
		source := strings.TrimSpace(s.Source)
		if source == "" || source != s.Source {
			return fmt.Errorf("集合源标识非法：%q（不允许为空或含首尾空白）", s.Source)
		}
		if _, dup := r.bySource[source]; dup {
			return fmt.Errorf("集合源 %q 重复注册", source)
		}
		r.bySource[source] = p
	}
	r.ordered = append(r.ordered, p)
	return nil
}

// ResolveCollection 实现 core.CollectionResolver：按源分发到注册的解析器。
func (r *collectionRegistry) ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error) {
	p, err := r.providerFor(ctx, source)
	if err != nil {
		return nil, err
	}
	return p.ResolveCollection(ctx, source, filter)
}

// ResolveCollectionPage 实现可选能力 CollectionPager：按页取数（审计 PERF-019）。
//
// 注册的解析器没实现分页时**不报错**，而是退化成「取一批再自行截断」，并返回
// Total = -1 告诉调用方「拿不到总量」—— 调用方据此退到游标式提示。
// 这里的取舍是：能力缺失只该让翻页退化，不该让整块列表渲染不出来。
func (r *collectionRegistry) ResolveCollectionPage(ctx context.Context, source string, q CollectionQuery) (CollectionPage, error) {
	p, err := r.providerFor(ctx, source)
	if err != nil {
		return CollectionPage{}, err
	}
	if pager, ok := p.(CollectionPager); ok {
		return pager.ResolveCollectionPage(ctx, source, q)
	}
	items, rerr := p.ResolveCollection(ctx, source, q.Filter)
	if rerr != nil {
		return CollectionPage{}, rerr
	}
	if q.Offset > 0 {
		if q.Offset >= len(items) {
			items = nil
		} else {
			items = items[q.Offset:]
		}
	}
	if q.Limit > 0 && len(items) > q.Limit {
		items = items[:q.Limit]
	}
	return CollectionPage{Items: items, Total: -1}, nil
}

// CollectionFilterOptions 实现 core.CollectionFilterOptionsProvider：按**注册时的集合源**
// 找到提供方，再问它能不能给筛选选项。
//
// 两步是刻意的，也是 ARCH-01 的根因所在：
//   - 「哪个源由谁提供」只认 Register 时提供方自报的集合源（bySource 表，装配期建立、
//     构建期只读），不是对某个对象做能力断言碰运气；
//   - 「这个源有没有筛选能力」才做能力探测，与 ResolveCollectionPage 对分页能力的
//     处理同一口径：可选性在提供方那一层，注册表无条件实现入口。
//
// 未注册的源、以及源没有筛选能力的（如 content:article）一律返回**空选项 + nil**：
// 筛选栏是可选装饰，缺能力只该让它不渲染，不该让整页构建失败（与集合元数据 /
// 分页退化的取舍一致）。提供方自身报错（如工程 ID 缺失）原样上抛，由调用方决定降级。
func (r *collectionRegistry) CollectionFilterOptions(ctx context.Context, source, projectID string) (CollectionFilterOptions, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return CollectionFilterOptions{}, nil
	}
	p, ok := r.lookup(source)
	if !ok {
		return CollectionFilterOptions{}, nil
	}
	provider, ok := p.(CollectionFilterOptionsProvider)
	if !ok {
		return CollectionFilterOptions{}, nil
	}
	return provider.CollectionFilterOptions(ctx, source, projectID)
}

// CollectionSchemas 实现 core.CollectionSchemaProvider：聚合全部集合源元数据。
//
// 顺序按源标识字典序（与注册顺序无关）：工作台下拉与构建期报错文案都依赖它，
// 同一批注册换个装配顺序不该改变输出字节（不变量 5 延伸）。
func (r *collectionRegistry) CollectionSchemas(ctx context.Context) ([]CollectionSchema, error) {
	r.mu.RLock()
	providers := make([]CollectionSourceProvider, len(r.ordered))
	copy(providers, r.ordered)
	r.mu.RUnlock()

	out := make([]CollectionSchema, 0, len(r.bySource))
	for _, p := range providers {
		schemas, err := p.CollectionSchemas(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, schemas...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out, nil
}

// Sources 实现 CollectionRegistry。
func (r *collectionRegistry) Sources(ctx context.Context) ([]string, error) {
	schemas, err := r.CollectionSchemas(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Source)
	}
	return out, nil
}

// providerFor 按源标识取提供方；未注册返回错误并列出可用源（报错即排查线索）。
func (r *collectionRegistry) providerFor(ctx context.Context, source string) (CollectionSourceProvider, error) {
	if p, ok := r.lookup(source); ok {
		return p, nil
	}
	available, err := r.Sources(ctx)
	if err != nil {
		return nil, err
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("未知集合源 %q（当前没有已注册的集合源）", source)
	}
	return nil, fmt.Errorf("未知集合源 %q（可用：%s）", source, strings.Join(available, "、"))
}

// lookup 源 → 提供方（读锁；空源直接未命中）。
func (r *collectionRegistry) lookup(source string) (CollectionSourceProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.bySource[source]
	return p, ok
}

// 编译期断言：注册表同时提供解析、元数据与筛选选项（派发）三个契约。
var (
	_ CollectionResolver              = (*collectionRegistry)(nil)
	_ CollectionSchemaProvider        = (*collectionRegistry)(nil)
	_ CollectionFilterOptionsProvider = (*collectionRegistry)(nil)
	_ CollectionRegistry              = (*collectionRegistry)(nil)
)

// buildProjectKey 站点工程 ID 在构建上下文里的键（私有类型，避免与其它包冲突）。
type buildProjectKey struct{}

// WithBuildProjectID 把本次构建的站点工程 ID 放进上下文。
//
// 集合解析器只拿得到 context（CollectionResolver.ResolveCollection 契约里没有
// RenderContext），而商品这类领域数据是分工程的：不给工程 ID 就会把别的站点
// 的商品渲染进本页。构建层在 Compile 时统一注入，解析器按需读取。
func WithBuildProjectID(ctx context.Context, projectID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, buildProjectKey{}, strings.TrimSpace(projectID))
}

// BuildProjectID 取上下文里的站点工程 ID（未设置 / 为空返回空串 = 不限工程）。
func BuildProjectID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(buildProjectKey{}).(string); ok {
		return v
	}
	return ""
}
