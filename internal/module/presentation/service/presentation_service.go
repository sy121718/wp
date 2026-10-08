package presentationservice

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

// 与 page 模块同名方法同义（page_lang.go §MarkStaleForI18n）：两者都是「构建期取词注入
// 字节」的产物，词条或译文一变，已发布产物就过期。缺这条的表现是「改了译文，商品页
// 还是旧字节」，而且日志里什么都没有（本项目反复吃过这类静默失效）。
//
// 逐工程扇出（DB-009 第三批）：presentation_instances 带 FORCE 策略，调用方（后台
// 翻译页）没有工程上下文，因此「全站」必须由 service 层逐工程拼出来；不做无作用域的全表 UPDATE。

// 搜索片段拿到的只有实体 id：cms 内容与商品的「有没有线上页面」不在它们自己的表上，
// 而在这里（presentation_instances 的 active 指针）。这条收窄端口把那个事实读出来，
// 顺带给调用方一个**可用的路径** —— 没有它，搜索结果就只能是不带链接的一堆标题。

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	mediacontract "go_wp/internal/module/media/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
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
	// checkoutCountries 结算表单国家下拉的来源（装配期注入，构建期按本实例语言取一份）。
	// 与 page 路径同一条来源函数（同一个适配器实例）：两条发布路径共用同一份清单，
	// 各解一次迟早会出现「手工页有国家、自动发布页没有」的分叉。
	checkoutCountries func(ctx context.Context, lang string) []core.CheckoutCountry
	// navigation 公开站点菜单（EDT-003）：core.nav 构建期解析。
	navigation navigationcontract.NavigationService
	// sitePages 系统页面槽位（EDT-003）：购物车/登录等链接烘进详情页产物。
	sitePages pagecontract.SitePageResolver
	// mediaProbe 响应式图片变体探测（EDT-003）；nil 时不输出 srcset。
	mediaProbe func(ctx context.Context, url string) []mediacontract.VariantRef
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
	// instanceLocks 实例分片互斥锁：串行化同一实例上「会改动实例状态」的那几段 ——
	// 发布会话的冻结段与提交段（版本分配 MAX+1 / 快照与产物行落库 / 访问面激活 /
	// 指针推进）、创建时的幂等检查与建行、改 URL 的占用预检、产物指针回滚整段。
	//
	// ⚠️ **不再覆盖各语言的编译与产物落盘**（PERF-01）：那一段现在在锁外跑，
	// 完整语义与所保护的不变量见 presentation_publish_plan.go 的文件头。
	instanceLocks [instanceLockStripes]sync.Mutex
	// buildQueue 自动重建任务的入队端口（PERF-020）：非空时 RebuildStale 只入队
	// 不同步重建 —— 进程内分片锁在多实例部署下拦不住两个实例同时重建同一实例，
	// 队列消费侧的 SKIP LOCKED claim 才是跨实例互斥的落点；nil 时回退原同步重建。
	buildQueue presentationcontract.BuildQueueEnqueuer

	// convergeWake 发布回执收敛的进程内快通道（容量 1）。
	//
	// 主链写路径在**事务落定之后**非阻塞地推一下，正常路径毫秒级收敛；
	// 通道满时丢弃信号，由定时器兜底（详见 ConvergePendingReceipts）。
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

// lockInstance 取某实体的实例级锁（按 entity 维度：创建 / 重建 / 改 URL / 回滚互斥同一把）。
//
// 谁在等它、它保护哪些不变量：presentation_publish_plan.go 的文件头（PERF-01 收窄后的
// 覆盖范围 —— 编译与产物落盘已在锁外，锁只覆盖冻结与提交两段）。
//
// ⚠️ **前提：单实例部署**（2026-09-19 用户确认）。这把锁是**进程内**的（分片 sync.Mutex），
// 它挡得住同一进程里的并发写路径，挡不住**两个进程**同时处理同一个实体 —— 多实例部署下
// 提交段仍会交错，产物指针与路由可能互相覆盖。
// 唯一的跨实例互斥落点是数据库：构建任务队列消费侧的 SKIP LOCKED claim（见本文件 buildQueue
// 的注释与 RebuildStale 的入队分支），它只覆盖**自动重建**，创建/改 URL 这两条同步路径不在队列里。
// 提交段另有一道**数据库侧**兜底（PERF-01）：提交前用冻结指纹比对实例行，别的进程刚提交过
// 也会被检出并触发重新冻结（见 presentation_publish_plan.go 的 commitPublish），它把
// 「交错提交」的窗口压到「指纹校验 → 落库」之间，但不构成严格互斥。
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

// 双轨能力端口断言（迁移 281 / 282）：让「presentation 提不提供这条端口」在编译期成立 ——
// 后台商品详情页按 presentationcontract.DetailTemplateModePort 直接注入，缺一条方法即构建失败。
var _ presentationcontract.DetailTemplateModePort = (*Service)(nil)

// 依赖扇出契约断言（pipeline.Fanout 的失效目标 + 自动重建实现）。
var _ pipeline.DependencyTarget = (*Service)(nil)
var _ pipeline.StaleRebuilder = (*Service)(nil)

// SourceType 实现 pipeline.DependencyTarget：本服务是自动发布实例来源。
func (s *Service) SourceType() string { return pipeline.SourceTypePresentation }

// SetProductDataSource 注入商品构建期数据源（issue #35，装配期调用）。
func (s *Service) SetProductDataSource(ds productcontract.ProductDataSource) { s.productDS = ds }

// SetCheckoutCountries 注入结算表单国家下拉的来源（core.checkoutForm 的构建期输入）。
//
// 与 page 路径同一形状与同一条判据：取到空清单不注入，由组件在「表单里有国家字段」时
// 显式失败（真因在装配层适配器的日志里）。未注入本身按必需端口断言。
func (s *Service) SetCheckoutCountries(fn func(ctx context.Context, lang string) []core.CheckoutCountry) {
	s.checkoutCountries = fn
}

// SetNavigationService 注入公开站点导航（EDT-003，装配期调用）。
func (s *Service) SetNavigationService(nav navigationcontract.NavigationService) { s.navigation = nav }

// SetSitePageResolver 注入系统页面槽位解析（EDT-003，装配期调用）。
func (s *Service) SetSitePageResolver(r pagecontract.SitePageResolver) { s.sitePages = r }

// SetMediaProbe 注入响应式图片变体探测（EDT-003，装配期调用）。
func (s *Service) SetMediaProbe(probe func(ctx context.Context, url string) []mediacontract.VariantRef) {
	s.mediaProbe = probe
}

// SetPluginService 注入插件装配（EDT-003，装配期调用）。
func (s *Service) SetPluginService(plugins plugincontract.PluginService) { s.plugins = plugins }

// MarkStaleForI18n 把各工程内的全部自动发布实例标记为待重建。
func (s *Service) MarkStaleForI18n(ctx context.Context) (err error) {
	if s.project == nil {
		return nil
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return err
	}
	at := time.Now().UTC()
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		if _, merr := s.m.MarkStaleForI18n(ctx, p.ID, at); merr != nil {
			return merr
		}
	}
	return nil
}

// MarkStaleForI18nTx 在**调用方的事务**内标记该工程的实例（page 侧 I18nStalePeer 的 Tx 变体）。
//
// 与 MarkStaleForI18n 是同一批失效判定，差别是事务边界：page 的 MarkStaleForI18n 把
// 「pages 标记 + 本来源标记」放进同一个事务（同库跨模块的写，AGENTS.md「写操作的事务与
// 回滚」），任一步失败整体回滚 —— 不再停在「页面已标、实例未标」的半截状态上等下一次
// 词条保存。本方法**不枚举工程**：工程由调用方给定（它自己逐工程扇出），作用域由 model
// 在传入的 tx 上设置。
//
// 与 MarkStaleForI18n 的另一个差别：不检查 s.project —— 那个检查存在的原因是自足版本
// 要靠 project 契约枚举工程；这里工程已是入参，缺契约不该把标记静默变成 no-op。
// tx 为 nil / 工程非法交给 model 报错（rls.ScopeTx 会拒非事务句柄），不在这一层吞掉。
func (s *Service) MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string) (err error) {
	if s == nil || s.m == nil {
		return errors.New("presentation: 实例仓储未装配，无法标记实例失效")
	}
	if _, merr := s.m.MarkStaleForI18nTx(ctx, tx, projectID, time.Now().UTC()); merr != nil {
		return merr
	}
	return nil
}

// 编译期断言：本 service 提供访问面解析线上路径需要的只读能力。
var _ presentationcontract.PublishedEntityLocator = (*Service)(nil)

// PublishedEntityPaths 按实体 id 批量解析已上线详情页的线上路径。
//
// 实现只做两件事：形状过滤（uuid）与查询下推；「已上线」的判定在 SQL 条件里
// （active_artifact_id IS NOT NULL），不在这里二次过滤。
func (s *Service) PublishedEntityPaths(ctx context.Context, projectID, entityType, lang string, entityIDs []string) (map[string]string, error) {
	ids := normalizeLocatorIDs(entityIDs)
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	if strings.TrimSpace(lang) == "" {
		lang = pipeline.DefaultLocale(ctx, s.project, projectID)
	}
	return s.m.ListActiveURLPaths(ctx, projectID, entityType, lang, ids)
}

// normalizeEntityIDs 去空、去重、**形状过滤**（uuid），保持首次出现顺序。
//
// 形状过滤是必需的而不是防御性洁癖：实体 id 来自片段 URL（任何人可构造），
// 非 uuid 的字符串带进 entity_id（uuid 列）的比较会让 PostgreSQL 直接报
// invalid input syntax for type uuid（SQLSTATE 22P02）—— 一个搜索请求就能把页面打成 500。
// 非法 id 在这里被丢弃，调用方按「查不到」处理（与真的没发布同一条路）。
func normalizeLocatorIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, err := uuid.Parse(id); err != nil {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
