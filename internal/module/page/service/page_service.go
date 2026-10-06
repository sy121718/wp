package pageservice

import (
	"context"
	"errors"
	"sync/atomic"

	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	mediacontract "go_wp/internal/module/media/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	plugincontract "go_wp/internal/module/plugin/contract"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/pipeline"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/pkg/i18n"

	"gorm.io/gorm"

	productcontract "go_wp/internal/module/product/contract"
)

var _ pagecontract.PageService = (*Service)(nil)

// Service Page 草稿、修订与发布业务服务。
//
// 发布链路持有 pipeline 内核（内存态可由数据库重建）与相邻模块契约，
// 不直接依赖 GORM 之外的模块实现。
type Service struct {
	model     *pagemodel.Model
	project   projectcontract.ProjectService
	artifacts artifactcontract.ArtifactService
	// pageArtifacts 页面产物元数据的只读视图（artifact 契约的另一个窄接口）。
	// 与 artifacts 是**同一个对象**的两种能力：ArtifactService 管记录/GC/状态，
	// PageArtifactReader 管「按 id 反查 hash / 产物挂在哪张页面」这类 page 模块要的读。
	pageArtifacts artifactcontract.PageArtifactReader
	// buildQueue 构建队列端口（审计 DB-007）：超出单次上限的自动重建交给它。
	// 未注入 = 没有队列，超出部分保持 stale 并记告警（既有行为）。
	buildQueue pagecontract.BuildQueueEnqueuer
	routes     pubcontract.PublicationService
	blocks     blockcontract.BlockService
	// blueprints 蓝图契约（审计 VIS-010）：新建页面时把蓝图 AST 复制成初始文档。
	// 用完即弃 —— 页面创建之后与蓝图再无关系（改蓝图不传播、不参与构建期）。
	blueprints blueprintcontract.BlueprintService
	plugins    plugincontract.PluginService
	content    core.CollectionResolver
	// productDS 商品构建期数据源（issue #35）：页面里的商品列表组件直连它（受限接口），
	// 未注入时组件回退按名路由。可选依赖不进构造参数，与其它端口同模式。
	productDS productcontract.ProductDataSource
	// navigation 公开站点导航契约：core.nav 绑定菜单位置时构建期解析菜单项。
	navigation navigationcontract.NavigationService
	// media 媒体契约：构建期探测图片变体，输出响应式 srcset（访客零查询）。
	media mediacontract.MediaService
	// structureTemplates 结构模板解析端口（页眉 / 页脚绑定结构模板时构建期取文档）。
	//
	// 消费者侧最窄端口（pipeline.StructureTemplatePort）：本模块不需要 contenttemplate
	// 的 DTO / 版本表 / 类型校验，只要「一份文档」。未注入时模板槽位一律回退块绑定 ——
	// 存量站点行为与改造前逐字节一致（见 page_assemble.go 的装配注释）。
	structureTemplates pipeline.StructureTemplatePort
	// contentStore 内容译文读取端口（多语言 P5b）：为 nil 时用 pkg/i18n 默认存储
	// （sys_translation 表 + 默认数据库）。测试经 SetContentTranslationStore 注入
	// 隔离 schema 的存储，用于验证「块内文本进候选集合 + 每页每语言一次查库」。
	contentStore i18n.ContentStore

	publisher *pipeline.Publisher
	store     *pipeline.LocalStore
	// publication 访问面激活存储（active 目录符号链接）：页面删除时必须按路径
	// 解除激活，否则「DB 路由已清、符号链接还在」会让已删内容继续可访问。
	publication *pipeline.LocalPublicationStore
	// externalArtifactOwners 其它模块的产物 hash 清单（IDX-015 反向对账用）。
	// 自动发布实例的产物与本模块共用一个 artifacts 根：不注入就只能把它们误报成孤儿。
	// 用 setter 注入而不是构造参数 —— page 不能反向依赖 presentation（那是依赖成环）。
	externalArtifactOwners func(ctx context.Context) ([]string, error)
	// publishWindowFault 是发布链「访问面已切换、数据库尚未落定」窗口的故障注入点
	// （审计 AR2-002 的故障注入测试）。生产恒为 nil。
	//
	// 为什么需要它：这个窗口恰好是崩溃恢复协议唯一无法用静态断言覆盖的分支 ——
	// 真实崩溃会连进程一起终止，子进程方案又无法保证终止点落在两次调用之间。
	// 注入后主链按「状态不可判定」收敛：保留 pending 回执、把错误上抛，由启动恢复
	// 按符号链接的实际指向补齐或回滚。
	// i18nPeer 文案词条 / 内容译文变更时的其它发布来源端口（可空）：见 page_lang.go
	// 的 I18nStalePeer —— 商品详情页（自动发布实例）等来源同样在构建期取词注入字节，
	// 由 page 的 MarkStaleForI18n 统一扇出，避免每个 i18n 保存路径各写一次、漏一处就静默失效。
	i18nPeer I18nStalePeer
	//
	// 三个入口共用这一个字段（发布 / 改 URL / 回滚），命中点都在各自的 DB 事务**内部**
	// 且位于第一条写之后 —— 要证明的是「半截写随事务一起回滚」，而不是「还没开始写」。
	// 测试见 public/test/page/unit/page_publish_ledger_test.go 与
	// page_url_rollback_ledger_test.go（改 URL / 回滚的收敛与幂等）。
	publishWindowFault func() error

	// convergeWake 发布回执收敛的进程内快通道（容量 1）。
	//
	// 写路径在**事务提交之后**非阻塞地推一下，正常路径毫秒级收敛；
	// 通道满时丢弃信号，由定时器兜底（详见 page_publish_converge.go）。
	convergeWake chan struct{}
	// lastConvergeAt 最近一次收敛运行时刻（Unix 秒；0 = 本进程还没跑过）。
	//
	// 进程内观测值，供 PendingReceiptStatus 与健康检查判断「本实例的收敛还在跑」；
	// 多实例部署下每个实例各记各的，不做全局真源（这是进程健康信号，不是业务状态）。
	lastConvergeAt atomic.Int64

	// checkoutCountries 结算表单国家下拉的来源（装配期注入，构建期按本页语言取一份）。
	//
	// 是**函数**而不是一份切片：国家选项与语言有关（sys_area 的中文名 / 英文名两列），
	// 而编译是逐语言的 —— 注入一份冻结的切片会让英文站点拿到中文国名。
	// 未注入时结算表单若含国家字段会在构建期明确报错（见 core.checkoutForm）。
	checkoutCountries func(ctx context.Context, lang string) []core.CheckoutCountry
}

// NewService 创建 Page 服务；同时初始化本地产物根（GO_WP_ARTIFACT_ROOT 可覆盖，
// 默认与访问面一致，经 pipeline.DefaultArtifactRoot/ActiveRoot 单源取值）。
// 构建注入装配感知编译器：页眉/页脚块内联（方案 C）+ 插件组件（docs/06）
// + 集合内容解析（docs/06 §9）。
func NewService(model *pagemodel.Model, artifacts artifactcontract.ArtifactService,
	routes pubcontract.PublicationService, project projectcontract.ProjectService,
	blocks blockcontract.BlockService, plugins plugincontract.PluginService,
	content core.CollectionResolver, navigation navigationcontract.NavigationService,
	media mediacontract.MediaService) *Service {
	store := &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()}
	publication := &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}
	s := &Service{
		model:       model,
		artifacts:   artifacts,
		routes:      routes,
		project:     project,
		blocks:      blocks,
		plugins:     plugins,
		content:     content,
		navigation:  navigation,
		media:       media,
		store:       store,
		publication: publication,
		// 容量 1：同一时刻只留一个待处理信号，多次写入合并成一次收敛。
		convergeWake: make(chan struct{}, 1),
	}
	// 页面产物只读视图从同一个 artifacts 实例派生：它们本来就是同一个对象的两种能力
	// （ArtifactService ∋ 记录/GC/状态，PageArtifactReader ∋ 按 id 反查 hash / 产物挂在哪张
	// 页面）。派生而不是再要一个构造参数，是为了不让二十多个建服务的地方各漏接一次 ——
	// 漏接的表现是「构建期依赖归属校验失败」，而它与调用方要验的东西毫无关系。
	if r, ok := artifacts.(artifactcontract.PageArtifactReader); ok {
		s.pageArtifacts = r
	}
	// 依赖提供者：把文案词条资源版本号写进 Manifest.dependencies
	// （DependencyKind=i18n，改文案触发重建，docs/06-D §10.4）。
	s.publisher = pipeline.NewPublisher(store, publication, pipeline.WithDependencies(s.buildDependencies))
	s.publisher.SetCompile(s.assembleCompile)
	return s
}

// SetStructureTemplatePort 注入结构模板解析端口（装配期调用，与其它可选依赖同模式）。
//
// 未注入（或某套模板解析失败）时结构槽位回退块绑定：漏接的表现是「主题里配了
// 页眉结构模板，站点上却还是旧块」—— 不会报错，故装配层按必需端口断言（见 assembly_publish.go）。
// SetPageArtifacts 注入页面产物元数据只读视图（装配期调用一次，与 artifacts 同源）。
//
// 未注入时依赖归属校验与孤儿对账会以「契约缺失」显式失败，而不是退回直读 page_artifacts
// —— 那张表属 artifact 模块，读它的列名等于把列名变成跨模块接口。
func (s *Service) SetPageArtifacts(reader artifactcontract.PageArtifactReader) {
	s.pageArtifacts = reader
}

func (s *Service) SetStructureTemplatePort(port pipeline.StructureTemplatePort) {
	s.structureTemplates = port
}

// SetContentTranslationStore 注入内容译文读取端口（测试用；生产走 pkg/i18n 默认存储）。
func (s *Service) SetContentTranslationStore(store i18n.ContentStore) {
	s.contentStore = store
}

// newContentTranslator 构造本次编译的内容译文取词器（一次批量查询 + 内存索引）。
//
// 注入端口优先（测试），否则用 pkg/i18n 默认存储（sys_translation）。
// 取词语义与缓存行为完全由 pkg/i18n 决定，本层不做二次缓存（docs/06-D §7.7）。
//
// projectID 为本次编译所属工程（审计 I18N-009）：译文按工程隔离，未覆盖时回落全局行。
func (s *Service) newContentTranslator(ctx context.Context, projectID, lang string, hashes []string) *i18n.ContentTranslator {
	if s != nil && s.contentStore != nil {
		return i18n.NewContentTranslatorScoped(ctx, projectID, s.contentStore, lang, hashes)
	}
	return i18n.NewContentTranslatorScoped(ctx, projectID, nil, lang, hashes)
}

// getExistingPage 按 id 定位未删除页面（逐工程独立作用域探测，DB-009 第四批），
// 统一映射未找到错误。
//
// 为什么不能直查：pages 带 FORCE 策略，GetByID(ctx, id, 空工程) 在换非超级角色后一律
// 返回 ErrRecordNotFound —— 于是构建 / 发布 / 回滚 / 改 URL 全部报「页面不存在」。
// 调用方（后台 API）手上只有 pageId，归属只能由本层逐工程探测确定；拿到页面之后，
// page.ProjectID 就是后续每一步写入的作用域来源。
func (s *Service) getExistingPage(ctx context.Context, id string) (page *pagemodel.PageEntity, err error) {
	page, err = s.locatePageInProjects(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	return page, nil
}

// SetProductDataSource 注入商品构建期数据源（issue #35，装配期调用）。
func (s *Service) SetProductDataSource(ds productcontract.ProductDataSource) { s.productDS = ds }

// SetCheckoutCountries 注入结算表单国家下拉的来源（core.checkoutForm 的构建期输入）。
//
// 传函数而不是切片：国家清单与语言有关，而编译是逐语言的（见字段注释）。
// 未注入的后果可见：页面里有结算表单 + 国家字段时构建失败（不是静默出一个空国家下拉），
// 因此装配层按必需端口断言。
func (s *Service) SetCheckoutCountries(fn func(ctx context.Context, lang string) []core.CheckoutCountry) {
	s.checkoutCountries = fn
}

// SetBlueprints 注入蓝图契约（装配期调用，审计 VIS-010）。
//
// 未注入时「从蓝图建页」会明确报错而不是静默建空页：空页在后台看起来像新建成功，
// 要等编辑者打开画布才发现什么都没有。
func (s *Service) SetBlueprints(bp blueprintcontract.BlueprintService) { s.blueprints = bp }

// SetPublishWindowFault 注入「访问面已切换、数据库尚未写入」窗口的故障（见字段注释；测试用）。
func (s *Service) SetPublishWindowFault(fn func() error) {
	if s == nil {
		return
	}
	s.publishWindowFault = fn
}
