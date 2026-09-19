// Package presentationservice presentation 模块业务实现（0-A2）。
//
// 自动发布：内容实体 + ContentTemplate → 派生快照 → 同一 Publish Compiler
// → ArtifactStore → PublicationStore（与手工 Page 共享管线，docs/02 §3）。
//
// DDL 对齐（本轮修复）：实例/快照/产物/依赖四张表按生产 DDL 读写
// （presentation_instances 用 stale + staged/active_artifact_id 指针，
// 不再有 status / artifact_hash 列）；project_id / template_id 为 NOT NULL
// 外键，创建时经 project 契约解析工程、经 ResolveTemplate 取模板 ID。
//
// MVP 取舍（开发阶段）：DocumentSnapshot 保存模板 AST（含 binding 节点），
// 编译时经 ContentResolver 解析为字面量——而非领域模型 §3.3 的「快照已
// 解析为字面量」。收益：复用 builder.WithContentResolver 注入，避免遍历
// AST 预解析的组件耦合；实体更新后 Rebuild 重编译即得新数据。
package presentationservice

import (
	"context"
	"hash/fnv"
	"sync"
	"sync/atomic"

	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/internal/builder/core"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"

	productcontract "go_wp/internal/module/product/contract"
)

// systemCreator 产物行 created_by 的占位（NOT NULL uuid 列不接受空串）。
const systemCreator = "00000000-0000-0000-0000-000000000000"

// maxAutoRebuildInstances 单次依赖失效触发的自动重建上限（与 page 侧同口径）。
const maxAutoRebuildInstances = 20

// instanceLockStripes 实例级并发锁的分片数。
// 用固定分片而非「按 ID 建锁」：既保证同实例互斥，又不随实例数累积锁对象。
const instanceLockStripes = 64

// Service presentation 模块业务实现。
type Service struct {
	m         *presentationmodel.Model
	templates contenttemplatecontract.ContentTemplateService
	registry  core.EntitySourceRegistry
	project   projectcontract.ProjectService
	// blocks 全局块契约：内容模板内部可用 core.globalref 引用页眉/页脚等区块，
	// 构建期由这里内联展开（与手工 Page 路径同一机制，docs/02-D §1.2）。
	blocks blockcontract.BlockService
	// collection 集合源解析器（装配期注入，可空）：模板内的集合类组件
	// （core.cardstack 绑定 content:product 等）在构建期展开为静态列表数据。
	// 未注入时集合绑定节点构建期显式报错（不静默产出空列表）。
	collection core.CollectionResolver
	// productDS 商品构建期数据源（issue #35）：模板里的商品组件直连受限接口。
	productDS productcontract.ProductDataSource
	// navigation 公开站点菜单（EDT-003）：core.nav 构建期解析。
	navigation navigationcontract.NavigationService
	// sitePages 系统页面槽位（EDT-003）：购物车/登录等链接烘进详情页产物。
	sitePages pagecontract.SitePageResolver
	// mediaProbe 响应式图片变体探测（EDT-003）；nil 时不输出 srcset。
	mediaProbe func(ctx context.Context, url string) []int
	// plugins 启用插件装配（EDT-003）：CompositeSet + PluginResolver + ExtraCSS。
	plugins plugincontract.PluginService
	// contentStore 内容译文读取端口（P5b）：nil 时用 pkg/i18n 默认存储。
	contentStore i18n.ContentStore
	store        *pipeline.LocalStore
	publication  *pipeline.LocalPublicationStore
	// routes URL 占用登记契约（publication 模块）。
	//
	// 详情页实例的路径此前只切换访问面符号链接、从不登记 page_routes：页面侧
	// 占用预检看不见详情页，详情页也看不见页面，两边可以先后激活同一路径，
	// 后者直接覆盖前者的线上内容且全程无报错。本次补上登记，改 URL 也才有
	// 「占用预检 + 旧路径 301」的落点。可空（单元测试 / 降级装配）：为 nil 时
	// 只做实例表内的占用预检。
	routes pubcontract.PublicationService
	// instanceLocks 实例分片互斥锁：并发构建同一实例时串行化
	// 「构建 → 落库 → 激活」整段序列，避免产物版本号 MAX+1 竞态与
	// active_artifact_id 指针交错覆盖（线上内容与 DB 指针分裂）。
	instanceLocks [instanceLockStripes]sync.Mutex
	// buildQueue 自动重建任务的入队端口（PERF-020）：非空时 RebuildStale 只入队
	// 不同步重建 —— 进程内分片锁在多实例部署下拦不住两个实例同时重建同一实例，
	// 队列消费侧的 SKIP LOCKED claim 才是跨实例互斥的落点；nil 时回退原同步重建。
	buildQueue presentationcontract.BuildQueueEnqueuer

	// convergeWake 发布回执收敛的进程内快通道（容量 1）。
	//
	// 主链写路径在**事务落定之后**非阻塞地推一下，正常路径毫秒级收敛；
	// 通道满时丢弃信号，由定时器兜底（详见 presentation_converge.go）。
	convergeWake chan struct{}
	// lastConvergeAt 最近一次收敛运行时刻（Unix 秒；0 = 本进程还没跑过）。
	//
	// 进程内观测值，供 PendingReceiptStatus 判断「本实例的收敛还在跑」；
	// 多实例部署下每个实例各记各的，不做全局真源（这是进程健康信号，不是业务状态）。
	lastConvergeAt atomic.Int64
	// converging 本进程正在进行中的收敛轮数（并发触发时可能 > 1）。
	//
	// 收敛的批次重跑（convergeInstanceBatch → RebuildInstance）会重新走一遍主链写路径，
	// 那条路径失败时按正常口径推快通道信号 —— 而它此刻正是被收敛驱动的，推回去就成了
	// 「收敛 → 重放 → 重跑批次 → 推信号 → 立刻再收敛」的自激热循环。
	// 用这个计数把「恢复驱动的写入」与「用户驱动的写入」区分开：收敛进行中不推信号
	// （非阻塞信号本就可丢，下一轮由 ticker 兜底）。
	//
	// 用计数而不是 bool：收敛可能被定时器与写路径快通道同时触发（也允许用例直接调），
	// bool 会被先结束的那一轮提前清掉，后一轮恢复驱动的写入又开始推信号。
	converging atomic.Int32
}

// lockInstance 取某实体的实例级锁（按 entity 维度：创建与重建互斥同一把）。
//
// ⚠️ **前提：单实例部署**（2026-09-19 用户确认）。这把锁是**进程内**的（分片 sync.Mutex），
// 它挡得住同一进程里的并发创建/重建，挡不住**两个实例**同时处理同一个实体 —— 多实例部署下
// 「构建 → 落库 → 激活」会交错，产物指针与路由可能互相覆盖。
// 唯一的跨实例互斥落点是数据库：构建任务队列消费侧的 SKIP LOCKED claim（见本文件 buildQueue
// 的注释与 RebuildStale 的入队分支），它只覆盖**自动重建**，创建/改 URL 这两条同步路径不在队列里。
// 将来要支持多实例，必须把这两条路径也换成 DB 锁（行锁或 advisory lock）+ 幂等重放，
// 而不是把这里的 mutex 换成别的进程内原语。
func (s *Service) lockInstance(entityType, entityID string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(entityType + ":" + entityID))
	return &s.instanceLocks[h.Sum32()%instanceLockStripes]
}

// NewService 构造（依赖 contenttemplate 契约 + 实体类型注册表 + project 契约
// + block 契约 + pipeline 内核）。blocks 为 nil 时引用区块的模板构建期显式报错，
// 不静默产出占位结构（构建期必须失败优于线上出现空块）。
func NewService(m *presentationmodel.Model,
	templates contenttemplatecontract.ContentTemplateService,
	registry core.EntitySourceRegistry,
	project projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	routes pubcontract.PublicationService) *Service {
	return &Service{
		m:           m,
		templates:   templates,
		registry:    registry,
		project:     project,
		blocks:      blocks,
		store:       &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()},
		publication: &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()},
		routes:      routes,
		// 容量 1：同一时刻只留一个待处理信号，多次写入合并成一次收敛。
		convergeWake: make(chan struct{}, 1),
	}
}

// SetCollectionResolver 注入集合源解析器（装配期调用，与其它可选依赖同模式：
// 不进构造参数）。传入 nil 表示模板不支持集合绑定。
func (s *Service) SetCollectionResolver(r core.CollectionResolver) { s.collection = r }

// SetContentTranslationStore 注入内容译文读取端口（测试用；生产走默认存储）。
func (s *Service) SetContentTranslationStore(store i18n.ContentStore) {
	s.contentStore = store
}

// newContentTranslator 构造本次编译的内容译文取词器（与 page 路径同源）。
//
// projectID 为本次编译所属工程（审计 I18N-009）：译文按工程隔离，未覆盖时回落全局行。
func (s *Service) newContentTranslator(ctx context.Context, projectID, lang string, hashes []string) *i18n.ContentTranslator {
	if s != nil && s.contentStore != nil {
		return i18n.NewContentTranslatorScoped(ctx, projectID, s.contentStore, lang, hashes)
	}
	return i18n.NewContentTranslatorScoped(ctx, projectID, nil, lang, hashes)
}

// 编译期契约断言。
var _ presentationcontract.PresentationService = (*Service)(nil)

// 依赖扇出契约断言（pipeline.Fanout 的失效目标 + 自动重建实现）。
var _ pipeline.DependencyTarget = (*Service)(nil)
var _ pipeline.StaleRebuilder = (*Service)(nil)

// SourceType 实现 pipeline.DependencyTarget：本服务是自动发布实例来源。
func (s *Service) SourceType() string { return pipeline.SourceTypePresentation }

// SetProductDataSource 注入商品构建期数据源（issue #35，装配期调用）。
func (s *Service) SetProductDataSource(ds productcontract.ProductDataSource) { s.productDS = ds }

// SetNavigationService 注入公开站点导航（EDT-003，装配期调用）。
func (s *Service) SetNavigationService(nav navigationcontract.NavigationService) { s.navigation = nav }

// SetSitePageResolver 注入系统页面槽位解析（EDT-003，装配期调用）。
func (s *Service) SetSitePageResolver(r pagecontract.SitePageResolver) { s.sitePages = r }

// SetMediaProbe 注入响应式图片变体探测（EDT-003，装配期调用）。
func (s *Service) SetMediaProbe(probe func(ctx context.Context, url string) []int) {
	s.mediaProbe = probe
}

// SetPluginService 注入插件装配（EDT-003，装配期调用）。
func (s *Service) SetPluginService(plugins plugincontract.PluginService) { s.plugins = plugins }
