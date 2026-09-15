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
}

// lockInstance 取某实体的实例级锁（按 entity 维度：创建与重建互斥同一把）。
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
