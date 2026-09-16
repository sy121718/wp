package routers

// assembly_publish.go — 装配的发布侧与运行时侧：发布实例、插件宿主、页面模块，
// 以及它们之间的端口注入、启动期后台任务、后台页面挂载与公开端点。
//
// 与 assembly.go 同属审计 CQ-008 的拆分：切段不改顺序，注释逐字保留。

import (
	"context"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	admincontract "go_wp/internal/module/admin/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	buildcontract "go_wp/internal/module/build/contract"
	contentcontract "go_wp/internal/module/content/contract"
	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navsource "go_wp/internal/module/navigation/outbound/source"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagehttp "go_wp/internal/module/page/inbound/http"
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginhttp "go_wp/internal/module/plugin/inbound/http"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationhttp "go_wp/internal/module/presentation/inbound/http"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	projectservice "go_wp/internal/module/project/service"
	runtimefragment "go_wp/internal/module/runtimefragment"
	userhttp "go_wp/internal/module/user/inbound/http"
	"go_wp/internal/partition"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

// buildPublishingModules 装配发布侧主干：自动发布实例、插件宿主、页面模块，
// 以及它们与访问面片段之间的共享解析器（集合解析器）。
//
// 顺序要求写在各段注释里：插件必须先于 page（page 构建路径依赖插件装配素材），
// 片段端口必须在 page 装配之前接好（page 的构建期组件要靠它取集合）。
func (a *assembly) buildPublishingModules() {
	marks := a.marks
	db := a.db
	authorizedAPI := a.authorizedAPI
	adminAuthzSvc := a.adminAuthzSvc
	artifactSvc := a.artifactSvc
	blockSvc := a.blockSvc
	collectionRegistry := a.collectionRegistry
	contentSvc := a.contentSvc
	contentTemplateSvc := a.contentTemplateSvc
	entityRegistry := a.entityRegistry
	mediaSvc := a.mediaSvc
	navigationSvc := a.navigationSvc
	productSvc := a.productSvc
	projectService := a.projectService
	publicationSvc := a.publicationSvc

	// 自动发布实例（内容实体驱动，复用编译/存储/激活管线；实例行需 project_id 外键）。
	// blockSvc 注入用于内容模板内部的全局块引用展开（页眉/页脚等，构建期内联）。
	presentationSvc := presentationhttp.SetupPresentationRoutes(authorizedAPI, db, contentTemplateSvc, entityRegistry, projectService, blockSvc, collectionRegistry, publicationSvc)

	// 插件模块（page 构建路径依赖其装配素材，须先于 page 装配）。
	// plugin 是外部插件宿主：注入 admin 权限上下文契约，供插件运行时读取当前用户权限。
	pluginSvc := pluginhttp.SetupPluginRoutes(authorizedAPI, db, adminAuthzSvc)
	// 插件宿主的权限上下文契约由 SetupPluginRoutes 内部注入（可选端口）。
	marks.mark(portPluginAdminAuthz)
	// 集合解析注入：注册表即 core.CollectionResolver（按源分发到内容 / 商品解析器，
	// 同时实现 CollectionSchemaProvider 供组件按白名单严格校验）。
	collectionResolver := core.CollectionResolver(collectionRegistry)
	// 商品列表片段（issue #27）：访问面的筛选/排序/分页局部刷新出口需要同一份集合解析器 ——
	// 片段渲染调 builder.RenderNodeHTML 复用构建期组件，取数自然也要走同一个注册表，
	// 否则「点筛选得到的」与「静态产物里的」会是两批数据。
	runtimefragment.SetCollectionResolver(collectionResolver)
	marks.mark(portRuntimeFragCollectionResolver)
	// 商品构建期数据源（issue #35）：组件直连受限接口，不再只靠按名路由。
	// ProductService 嵌入了 ProductDataSource，装配处拿到的契约天然能传。
	runtimefragment.SetProductDataSource(productSvc)
	marks.mark(portRuntimeFragProductDataSource)
	// 站内搜索片段（BIZ-2）：两条检索端口 + 一条「实体 → 已上线路径」解析端口。
	//
	// 检索端口是「按消费方收窄」的又一例：访问面片段只需要「按关键词取一批」这一条
	// 只读能力，而 content / product 的服务契约各自带着全部写方法。路径端口拿的是
	// 自动发布模块的只读面 —— 搜索结果要给链接，而链接必须指向**真实已上线**的页面
	// （未发布 / 查不到就不给链接，绝不输出死链）。三条都 fail-fast：漏接的表现是
	// 「搜索永远没有结果」，比启动时报错隐蔽得多。
	contentSearch, ok := contentSvc.(contentcontract.SearchPort)
	if !ok {
		panic("内容模块未实现检索端口（SearchPort）")
	}
	runtimefragment.SetContentSearchProvider(contentSearch)
	marks.mark(portRuntimeFragContentSearch)
	productSearch, ok := productSvc.(productcontract.SearchPort)
	if !ok {
		panic("商品模块未实现检索端口（SearchPort）")
	}
	runtimefragment.SetProductSearchProvider(productSearch)
	marks.mark(portRuntimeFragProductSearch)
	publishedLocator, ok := presentationSvc.(presentationcontract.PublishedEntityLocator)
	if !ok {
		panic("自动发布模块未实现已上线路径解析端口（PublishedEntityLocator）")
	}
	runtimefragment.SetPublishedEntityLocator(publishedLocator)
	marks.mark(portRuntimeFragPublishedLocator)
	// 商品侧同一端口（集合项 url 字段）：原写法是「命中即注入、未命中静默跳过」，
	// 跳过的表现是「商品集合项的 url 全空、列表页商品没有链接」—— 静默降级（审计 CQ-019）。
	// 改为断言：装配缺陷在启动时炸掉。
	if setter, ok := productSvc.(interface {
		SetPublishedEntityLocator(presentationcontract.PublishedEntityLocator)
	}); ok {
		setter.SetPublishedEntityLocator(publishedLocator)
	} else {
		panic("商品模块未提供已上线路径解析注入点（SetPublishedEntityLocator）")
	}
	marks.mark(portProductPublishedLocator)
	// 归档页按需创建（审计 EDT-004）：分类新建 / 改名时让对应归档页跟上。
	// 与上面的已上线定位端口同一方向（product 拿收窄接口），注入点在 presentation
	// 装配之后 —— 端口本身就是 presentation 的服务。
	// 同一手法（审计 CQ-019）：静默跳过时「分类改名后归档页不跟上」，线上仍是旧路径，
	// 且没有任何日志或报错 —— 只可能是装配缺陷，故断言而非跳过。
	if setter, ok := productSvc.(interface {
		SetArchiveInstanceEnsurer(presentationcontract.ArchiveInstanceEnsurer)
	}); ok {
		setter.SetArchiveInstanceEnsurer(presentationSvc)
	} else {
		panic("商品模块未提供归档页创建注入点（SetArchiveInstanceEnsurer）")
	}
	marks.mark(portProductArchiveEnsurer)
	// 片段缓存失效回调（PERF-002，审计 CQ-019）：静默跳过时改价后前台继续显示过期价，
	// 且缓存里那份 HTML 看不出任何异常。注入源是包级函数，恒可得，故断言。
	if setter, ok := productSvc.(interface {
		SetFragmentCacheBumper(func(context.Context, string))
	}); ok {
		setter.SetFragmentCacheBumper(runtimefragment.BumpFragmentCacheVersion)
	} else {
		panic("商品模块未提供片段缓存失效注入点（SetFragmentCacheBumper）")
	}
	marks.mark(portProductFragmentCacheBumper)
	// navigationSvc 注入 page 装配：core.nav 绑定菜单位置时构建期解析菜单项。
	pageService := pagehttp.SetupPageRoutes(authorizedAPI, db, artifactSvc, publicationSvc, projectService, blockSvc, pluginSvc, collectionResolver, navigationSvc, mediaSvc)

	a.presentationSvc = presentationSvc
	a.pluginSvc = pluginSvc
	a.collectionResolver = collectionResolver
	a.pageService = pageService
}

// wirePublishingPorts 发布侧的端口注入：页面与发布实例互相接线、构建队列执行器注册、
// 片段层的只读端口注入。
//
// 这一段几乎全是「收窄端口 + fail-fast 断言」：每个端口漏接的后果都是静默的
// （禁用语言不下线、产物属主漏报、退货片段恒不可用、内容改了不重建），
// 因此一律断言而不是静默跳过。
func (a *assembly) wirePublishingPorts() {
	marks := a.marks
	buildSvc := a.buildSvc
	mediaSvc := a.mediaSvc
	navigationSvc := a.navigationSvc
	pageService := a.pageService
	pluginSvc := a.pluginSvc
	productSvc := a.productSvc
	projectService := a.projectService
	presentationSvc := a.presentationSvc

	// 语言下线端口（审计 I18N-017）：禁用语言时 project 需要把该语言的路由下掉，
	// 而「这个语言有哪些已激活路径」只有 page 知道 —— 方向 project → 端口 → page。
	// 反向（project 直接 import page）会成环：page 本来就依赖 project。
	// 原写法是「命中即注入、未命中静默跳过」（审计 CQ-019）：跳过的表现是
	// **禁用语言成功但该语言站点仍在线上** —— 运营以为下掉了，其实没有。
	// page.Service 有编译期断言保证实现该契约，故断言成本为零。
	retire, ok := pageService.(projectcontract.LocaleRetirePort)
	if !ok {
		panic("页面模块未实现语言下线端口（LocaleRetirePort）")
	}
	projectService.SetLocaleRetirePort(retire)
	marks.mark(portProjectLocaleRetire)
	// 主题包资产端口（审计 VIS-014）：主题导出要读**跨模块**的块与页面，而 project 模块
	// 不认识 block/page 的任何包 —— 能力经适配器注入（适配层在 project 侧，依赖方向是
	// 「实现方依赖调用方契约」，与 orderstock 同一手法）。未注入时导出/导入返回 503
	// ErrThemeBundlePortUnavailable：明确拒绝并说明「端口未装配」，而不是静默降级成只导令牌。
	// 注入点定义在具体 service 上而不是 projectcontract.ProjectService：它是**装配期 setter**，
	// 不属于运行时契约（契约只放消费方调用的业务能力）。与库存那条（SetInventoryService）、
	// 商品那条（SetAvailabilityPort）同一手法：断言具体类型，拿不到就是装配缺陷，当场炸掉。
	projectConcrete, projectOK := projectService.(*projectservice.Service)
	if !projectOK {
		panic("project 模块装配返回的不是具体 service（无法注入主题包资产端口）")
	}
	projectConcrete.SetThemeBundleAssetPort(projectservice.NewThemeBundleAssetPort(a.blockSvc, pageService))
	marks.mark(portProjectThemeBundleAssets)
	// 产物磁盘对账的属主清单（IDX-015）：自动发布实例与手工页面共用同一个 artifacts 根，
	// 反向对账必须同时问两个模块「这些磁盘目录是不是你产出的」。漏接的后果不是报错而是
	// **误报**：实例产物全被列成孤儿，一份看不出真假的对账结果比没有对账更糟。
	artifactOwnerSetter, ok := pageService.(interface {
		SetExternalArtifactOwners(func(ctx context.Context) ([]string, error))
	})
	if !ok {
		panic("页面模块未提供产物属主注入点（SetExternalArtifactOwners）")
	}
	artifactOwnerSetter.SetExternalArtifactOwners(presentationSvc.ListArtifactHashes)
	marks.mark(portPageExternalArtifactOwners)
	// 蓝图契约接线（审计 VIS-010）：「从蓝图新建页面」此前只有能力没有调用方 ——
	// routes.go 在这里长期挂着一行 _ = blueprintSvc，新建页面流程始终传的是空文档。
	// 蓝图是「用完即弃」的初始化输入：NewPage 那一刻复制 AST 并重生成节点 ID，
	// 之后改蓝图不会影响已建页面，也不参与构建期。
	if bpSetter, ok := pageService.(interface {
		SetBlueprints(blueprintcontract.BlueprintService)
	}); ok {
		bpSetter.SetBlueprints(a.blueprintSvc)
	} else {
		panic("页面模块未提供蓝图注入点（SetBlueprints）")
	}
	marks.mark(portPageBlueprints)
	// 构建队列接线（审计 DB-007）：
	//   - page 把超出单次上限的自动重建交给队列（端口定义在 page 契约里，方向是 page ← build）；
	//   - 执行器在这里注册（build 模块不认识任何业务来源，它只知道「有个函数能做这个 type」）。
	if queueSetter, ok := pageService.(interface {
		SetBuildQueue(pagecontract.BuildQueueEnqueuer)
	}); ok {
		queueSetter.SetBuildQueue(buildSvc)
	} else {
		panic("页面模块未提供构建队列注入点（SetBuildQueue）")
	}
	marks.mark(portPageBuildQueue)
	buildSvc.RegisterExecutor("page", func(ctx context.Context, job *buildcontract.Job) error {
		_, err := pageService.Build(ctx, &pagedto.BuildReq{ID: job.SourceID})
		return err
	})
	// presentation 的自动重建接线（PERF-020）：失效扇出不再在触发进程里持实例锁
	// 同步重建（进程内锁在多实例部署下拦不住两个实例同时重建同一实例），改为入队，
	// 由队列消费侧的 SKIP LOCKED claim 保证同一实例同一时刻只被一个 worker 重建。
	if pqSetter, ok := presentationSvc.(interface {
		SetBuildQueue(presentationcontract.BuildQueueEnqueuer)
	}); ok {
		pqSetter.SetBuildQueue(buildSvc)
	} else {
		panic("自动发布实例模块未提供构建队列注入点（SetBuildQueue）")
	}
	marks.mark(portPresentationBuildQueue)
	buildSvc.RegisterExecutor("presentation", func(ctx context.Context, job *buildcontract.Job) error {
		rebuilder, ok := presentationSvc.(interface {
			RebuildInstance(ctx context.Context, instanceID string) error
		})
		if !ok {
			panic("自动发布实例模块未提供实例重建执行体（RebuildInstance）")
		}
		return rebuilder.RebuildInstance(ctx, job.SourceID)
	})
	// worker 数固定 2：单页构建是 CPU + IO 混合，把并发调高只是在同一台机器上互相抢资源。
	// 多实例部署下每个实例各起 2 个是安全的 —— 取任务走 SKIP LOCKED，同一条任务只被一个 worker 拿到。
	buildSvc.StartWorkers(context.Background(), 2)
	// 分区维护（审计 DB-004）：三张只增表（page_views / inventory_stock_movements /
	// master_data_changes）按月分区，启动时补齐未来分区、之后每日一次。
	// 不启动它不会立刻出错（数据落 DEFAULT 分区），但分区裁剪与整块归档的收益就没了。
	partition.StartScheduler(context.Background(), a.db)
	// 系统页面槽位解析器接给片段层（BIZ-1）：购物车片段的「去结算」、结算结果的
	// 「查看订单」都要按槽位取路径。传的是 pageService —— 它嵌入了只读的
	// SitePageResolver，发布 / 删除 / 改 URL 那部分能力传不进片段层。
	runtimefragment.SetSitePageResolver(pageService)
	marks.mark(portRuntimeFragSitePageResolver)
	runtimefragment.SetFragmentProject(projectService)
	marks.mark(portRuntimeFragProject)
	// 访客订单片段（BIZ-1）：片段端点按访客会话取自己的订单。传的是 orderSvc ——
	// 它嵌入了只读的 VisitorOrderReader，写路径（建单 / 状态流转 / 优惠码管理）
	// 那部分能力传不进片段层。归属校验在 order 模块的 SQL 条件里，不在这层。
	runtimefragment.SetVisitorOrderReader(a.orderSvc)
	marks.mark(portRuntimeFragVisitorOrderReader)
	// 访客退货片段（RMA）：orderSvc 嵌入了收窄的 VisitorReturnPort（只读申请面，
	// 拿不到「后台审核 / 入库 / 退款」）。此端口此前**从未被任何地方注入** ——
	// 退货申请片段因此恒返回「退货功能暂不可用」（审计 CQ-019：静默降级窗口）。
	runtimefragment.SetVisitorReturnProvider(a.orderSvc)
	marks.mark(portRuntimeFragVisitorReturn)
	// 访客身份解析中间件：片段端点需要知道「这个请求是谁」。
	// 它与后台的 SessionAuthMiddleware 是两套身份（不同 cookie、不同存储），
	// 挂在片段组上只做「尽力解析」，未登录不阻断 —— 必须登录的能力自己渲染引导文案。
	runtimefragment.SetVisitorIdentityMiddleware(userhttp.VisitorIdentityMiddleware(a.userSvc))
	marks.mark(portRuntimeFragVisitorIdentity)
	// 账号中心片段（资料 / 偏好 / 改密码 / 登录设备）：只注入**收窄后的**只读端口 ——
	// 片段层拿不到注册、改密码、踢出设备这些写能力，越权防护靠接口形状。
	runtimefragment.SetVisitorAccountPort(a.userSvc)
	marks.mark(portRuntimeFragVisitorAccount)
	// 页面 / 自动发布两条构建路径同样接上（issue #35）：装配处拿到的 ProductService
	// 嵌入了 ProductDataSource，直接传即可（受限接口，写方法传不出去）。
	if setter, ok := pageService.(interface {
		SetProductDataSource(productcontract.ProductDataSource)
	}); ok {
		setter.SetProductDataSource(productSvc)
	} else {
		panic("页面模块未提供商品数据源注入点（SetProductDataSource）")
	}
	marks.mark(portPageProductDataSource)
	if setter, ok := presentationSvc.(interface {
		SetProductDataSource(productcontract.ProductDataSource)
	}); ok {
		setter.SetProductDataSource(productSvc)
	} else {
		panic("发布实例模块未提供商品数据源注入点（SetProductDataSource）")
	}
	marks.mark(portPresentationProductDataSource)
	// 自动发布详情页与手工页面共用站点级装配（EDT-003）：导航 / 槽位 / 响应式图片。
	if setter, ok := presentationSvc.(interface {
		SetNavigationService(navigationcontract.NavigationService)
		SetSitePageResolver(pagecontract.SitePageResolver)
		SetMediaProbe(func(context.Context, string) []int)
		SetPluginService(plugincontract.PluginService)
	}); ok {
		setter.SetNavigationService(navigationSvc)
		setter.SetSitePageResolver(pageService)
		setter.SetMediaProbe(mediaSvc.ProbeImageVariants)
		setter.SetPluginService(pluginSvc)
	} else {
		panic("发布实例模块未提供站点装配注入点（SetNavigationService / SetSitePageResolver / SetMediaProbe / SetPluginService）")
	}
	marks.mark(portPresentationSiteAssembly)
}

// startRuntimeTasks 启动期的一次性后台任务与运行时接线：默认主题补齐、组件版本比对、
// 依赖扇出、导航来源解析。
//
// 这些任务全部「失败不阻断启动」—— 少一次提示不影响任何功能，
// 但成功时必须留下可见日志（否则运维无从判断是否执行过）。
func (a *assembly) startRuntimeTasks() {
	marks := a.marks
	blockSvc := a.blockSvc
	contentSvc := a.contentSvc
	navigationSvc := a.navigationSvc
	pageService := a.pageService
	presentationSvc := a.presentationSvc
	projectService := a.projectService

	// 默认主题补齐（启动时一次，幂等）：本能力上线前建的工程没有任何主题，
	// 页面因此一直没有主题可继承 —— 继承链「主题 → 页面 → 组件」的起点缺失。
	// 失败不阻断启动（不影响已有工程，新建工程仍会即时获得默认主题）。
	if fixed, terr := projectService.EnsureDefaultThemes(context.Background()); terr != nil {
		logger.Scene("init").Error(terr, "默认主题补齐失败（不阻断启动）")
	} else if fixed > 0 {
		logger.Scene("init").With("count", fixed).Info("已为无主题工程补默认主题（后台风格色值）")
	}
	// 组件注册表版本比对（启动时一次）：
	// 组件是编译进二进制的（Go 实现 + embed 模板），部署新组件后没有任何运行时事件
	// 能提示「已有产物由旧组件产出」。这里比对产物元数据里的 registry_version 与本进程
	// 当前指纹（builder.RegistryVersion），把差异页面标记为待重建。
	//
	// 只标记、不重建：启动时全量构建会拖住启动链，且对「只想先看一眼」的部署是意外
	// 副作用；重建由运维经 RebuildStale 触发，或由后续编辑/发布自然覆盖。
	// 失败不阻断启动（少一次提示不影响任何功能）。
	if marked, verr := pageService.MarkStaleByRegistryVersion(context.Background(), builder.RegistryVersion()); verr != nil {
		logger.Scene("init").Error(verr, "组件版本比对失败（不阻断启动）")
	} else if len(marked) > 0 {
		logger.Scene("init").With("count", len(marked)).With("registryVersion", builder.RegistryVersion()).
			Info("检测到组件已更新：相关页面已标记待重建（可经 RebuildStale 重建）")
	}
	// 依赖 fan-out（PIPE-3，docs/03-pipeline.md §8.2）：内容实体变更 → 按依赖表
	// 反查受影响产物 → 精确标记 stale（不再是全站标记）→ 自动重建。
	//
	// 装配顺序要求：page 服务必须先装配完成（作为失效目标与重建实现），
	// 再由内容服务持有扇出端口；presentation 侧待其 DB 持久化对齐后接入同一 Fanout。
	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePage, pageService)
	// SetRebuilder 对 nil 是**静默 no-op**（「未绑定时只标记不重建」）—— 若 pageService
	// 没实现 StaleRebuilder，内容保存会照常成功、stale 也照常标记，只是永远不重建：
	// 线上内容停在旧版本，没有任何报错（审计 CQ-019）。故先断言再注入。
	pageRebuilder, ok := pageService.(pipeline.StaleRebuilder)
	if !ok {
		panic("页面模块未实现依赖失效重建接口（pipeline.StaleRebuilder）")
	}
	fanout.SetRebuilder(pipeline.SourceTypePage, pageRebuilder)
	marks.mark(portPipelinePageRebuilder)
	contentSvc.SetDependencyInvalidator(fanout)
	marks.mark(portContentDependencyInvalidator)

	// 导航来源实体解析（page/article/product/category/block → 标题 + URL）：
	// 依赖 page/content/presentation/block 契约，故在它们全部装配完成后注入。
	navigationSvc.SetSourceResolver(navsource.New(pageService, contentSvc, presentationSvc, blockSvc))
	marks.mark(portNavigationSourceResolver)
}

// mountAdminPages 后台页面路由（编辑器外壳依赖 page/block/plugin 契约，置于 API 装配之后）。
//
// admin 六领域 CRUD 契约：SetAdminRoutes 返回的 AuthzContextService 动态类型即合并后的
// *Service（同实现全部六接口），此处匿名接口断言获得管理面 CRUD 能力注入 dashboard，
// 供 /admin 六领域管理页（管理员/角色/菜单/权限/部门/数据权限）消费；不使用 GET/POST 之外的动词。
func (a *assembly) mountAdminPages() {
	marks := a.marks
	adminAuthzSvc := a.adminAuthzSvc
	adminCRUD := adminAuthzSvc.(interface {
		admincontract.AdminService
		admincontract.RoleService
		admincontract.PermService
		admincontract.MenuService
		admincontract.DeptService
		admincontract.RuleService
	})
	dashHandle := dashboardhttp.SetupDashboardRoutes(a.router, a.pageService, a.projectService, a.blockSvc, a.pluginSvc, a.collectionResolver,
		adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminAuthzSvc, a.navigationSvc, a.productSvc, a.presentationSvc, a.contentTemplateSvc,
		// 文章管理页（INF-1）：content 契约在注入片段端口时已拿到，这里复用同一个实例。
		a.contentSvc,
		a.inventorySvc, a.masterdataSvc, a.mailSvc,
		// 订单管理页（BIZ-1）：orderSvc 是在前面装配订单模块时拿到的契约
		//（它同时提供访客查询与优惠码能力，后台页只用查询与状态流转那几条）。
		a.orderSvc,
		// 访问统计页（BIZ-8）：只读聚合（按天 / 按路径 + 时间范围筛选 + 分页）。
		a.analyticsSvc,
		// 客户管理页：用户模块的后台面（收窄到四条方法，见 usercontract.CustomerAdminPort）。
		// 它同时也是「访客面 /user/*」那套 service 的同一个实例 —— 两个面共用实现，
		// 但页面拿到的接口里只有「读客户 + 停用启用 + 解除锁定」。
		a.userAdminSvc)
	// 蓝图（审计 VIS-010）：新建页面表单的「从蓝图开始」下拉需要蓝图列表。
	// 未注入时页面表单不显示该下拉（建页照常走空白草稿），因此这里是可选端口。
	if dashHandle != nil {
		dashHandle.SetBlueprints(a.blueprintSvc)
		marks.mark(portDashboardBlueprints)
	}
}

// runSelfCheck 装配自检（审计 CQ-019）：必需端口逐个核对，缺失即 fail-fast 并
// **报出端口名与后果**（一次报出全部缺失项，清单见 wiring.go 的 wiringManifest）；
// 可选端口未接入写进启动日志 —— 降级必须可见，而不是只在代码注释里写一句「未注入时降级」。
func (a *assembly) runSelfCheck() {
	mustAllPortsWired(a.marks)
	// 权限点声明同步（审计 SEC-011）：路由装配完成后，把声明表幂等 upsert 进库
	// （只补缺失的权限点与超管策略，不动人工的角色 / 用户授权，见 internal/permission/sync.go）。
	syncDeclaredPermissions(a.db)
	logDegradedOptional(a.marks)
}

// mountPublicFace 公开端点与兜底路由：运行时片段端点 + 未匹配路由处理。
func (a *assembly) mountPublicFace() {
	// 运行时片段端点（0-D，公开路由：capability 白名单 + 认证策略在 handler 内）。
	runtimefragment.SetupFragmentRoutes(a.router)

	// 未匹配路由返回 404；访问面（/site）优先返回站点自定义 404 页，见 notFoundHandler。
	a.router.NoRoute(notFoundHandler())
}
