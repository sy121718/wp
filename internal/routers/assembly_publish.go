package routers

// assembly_publish.go — 装配的发布侧与运行时侧：发布实例、插件宿主、页面模块，
// 以及它们之间的端口注入、启动期后台任务、后台页面挂载与公开端点。
//
// 与 assembly.go 同属审计 CQ-008 的拆分：切段不改顺序，注释逐字保留。

import (
	"context"
	"go_wp/config"
	"time"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	blockcontract "go_wp/internal/module/block/contract"
	blockhttp "go_wp/internal/module/block/inbound/http"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	buildcontract "go_wp/internal/module/build/contract"
	contentcontract "go_wp/internal/module/content/contract"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	mediacontract "go_wp/internal/module/media/contract"
	mediahttp "go_wp/internal/module/media/inbound/http"
	membershipcontract "go_wp/internal/module/membership/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navsource "go_wp/internal/module/navigation/outbound/source"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagehttp "go_wp/internal/module/page/inbound/http"
	pagemcp "go_wp/internal/module/page/inbound/mcp"
	pageservice "go_wp/internal/module/page/service"
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginhttp "go_wp/internal/module/plugin/inbound/http"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationhttp "go_wp/internal/module/presentation/inbound/http"
	productcontract "go_wp/internal/module/product/contract"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectcontract "go_wp/internal/module/project/contract"
	projecthttp "go_wp/internal/module/project/inbound/http"
	runtimefragment "go_wp/internal/module/runtimefragment"
	sysconfigcheckout "go_wp/internal/module/sysconfig/outbound/checkoutcountries"
	userhttp "go_wp/internal/module/user/inbound/http"
	workbenchhttp "go_wp/internal/module/workbench/inbound/http"
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
	a.fragDeps.CollectionResolver = collectionResolver
	marks.mark(portRuntimeFragCollectionResolver)
	// 商品构建期数据源（issue #35）：组件直连受限接口，不再只靠按名路由。
	// ProductService 嵌入了 ProductDataSource，装配处拿到的契约天然能传。
	a.fragDeps.ProductDataSource = productSvc
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
	a.fragDeps.ContentSearchProvider = contentSearch
	marks.mark(portRuntimeFragContentSearch)
	productSearch, ok := productSvc.(productcontract.SearchPort)
	if !ok {
		panic("商品模块未实现检索端口（SearchPort）")
	}
	a.fragDeps.ProductSearchProvider = productSearch
	marks.mark(portRuntimeFragProductSearch)
	publishedLocator, ok := presentationSvc.(presentationcontract.PublishedEntityLocator)
	if !ok {
		panic("自动发布模块未实现已上线路径解析端口（PublishedEntityLocator）")
	}
	a.fragDeps.PublishedEntityLocator = publishedLocator
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
	pageService := pagehttp.SetupPageRoutes(authorizedAPI, db, artifactSvc, publicationSvc, projectService, blockSvc, pluginSvc, collectionResolver, navigationSvc, mediaSvc, a.adminPages, a.workbenchPages)
	// 页面工具（page_find）：注册点在这里而不是与其它工具并列 —— pageService
	// 是这一层的局部变量（它依赖构建 / 发布链路，装配顺序靠后），主装配函数里拿不到。
	// 依赖收窄到 PageQueryReader（只有 ListPageTitles）：手里没有发布 / 回滚 / 删除，
	// 「AI 顺手把一个页面下线了」不会在某次改动里变得可能。
	if pageTools, err := pagemcp.Tools(pageService); err != nil {
		panic("页面工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(pageTools...); err != nil {
		panic("页面工具注册失败：" + err.Error())
	}

	// 结构模板（页眉 / 页脚）→ page 构建路径的模板解析端口。
	//
	// 断言 + fail-fast（与相邻端口同一判据）：漏接的表现是「主题里绑定了结构模板，
	// 站点上仍是旧块（或干脆没有页眉页脚）」—— 构建期按回退路径静默完成，日志里什么都没有。
	pageTemplateSetter, ok := pageService.(interface {
		SetStructureTemplatePort(pipeline.StructureTemplatePort)
	})
	if !ok {
		panic("page 模块未提供结构模板端口注入点（SetStructureTemplatePort）")
	}
	pageTemplateSetter.SetStructureTemplatePort(structureTemplatePortAdapter{svc: contentTemplateSvc})
	marks.mark(portPageStructureTemplates)

	// block ↔ page 的装配期接线（原在 dashboard 的 NewHandle 内，页面回迁后移到装配层）：
	// 块内容变更/删除后的 stale 传播，与删除前的引用检查。两个闭包只依赖 page/project 契约。
	if setter, ok := blockSvc.(interface {
		SetStalePropagator(func(ctx context.Context, blockID string) error)
	}); ok {
		// presentation 的重建器单独取：契约接口只声明 MarkStaleByDependency，RebuildStale 是
		// pipeline 端口。断言而不是「断言失败就只标记」—— 静默降级会退化成
		// 「改了页眉块、自动发布实例永远停在旧字节且无报错」（与下面扇出注册处同一取舍）。
		presentationRebuilder, ok := presentationSvc.(pipeline.StaleRebuilder)
		if !ok {
			panic("自动发布模块未实现依赖失效重建接口（pipeline.StaleRebuilder）")
		}
		setter.SetStalePropagator(BlockStalePropagator(pageService, projectService, presentationSvc, presentationRebuilder))
		marks.mark(portBlockStalePropagator)
	} else {
		panic("block 模块未提供 stale 传播注入点（SetStalePropagator）")
	}
	// 删除保护的引用检查（审计 ARCH-02）：五条来源在 BlockReferenceChecker 内合并。
	// 断言而不是「命中即跳过」：漏接的表现是「删除保护整体失效或只覆盖一部分」，
	// 而它不会让任何测试或启动日志变红 —— 只会在块被删掉后的下一次构建才暴露。
	if checker, ok := blockSvc.(interface {
		SetReferenceUsageChecker(func(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error))
	}); ok {
		checker.SetReferenceUsageChecker(BlockReferenceChecker(blockSvc, pageService, projectService, presentationSvc, contentTemplateSvc))
		marks.mark(portBlockReferenceUsageChecker)
	} else {
		panic("block 模块未提供引用明细注入点（SetReferenceUsageChecker）")
	}
	if wired, ok := blockSvc.(interface{ RequireWiring() }); ok {
		wired.RequireWiring()
	} else {
		panic("block 模块未提供装配自检入口（RequireWiring）")
	}

	a.presentationSvc = presentationSvc
	a.pluginSvc = pluginSvc
	a.collectionResolver = collectionResolver
	a.pageService = pageService

	// 收敛积压的只读观测接入 /readyz（pendingReceipts 字段，见 readyz_receipts.go）。
	// i18n 失效扇出（翻译底座）：page.MarkStaleForI18n 是四条 i18n 保存路径的既有入口，
	// 自动发布实例由这里挂上去 —— 否则「改了译文，商品页仍是旧字节」且无任何报错。
	// 类型断言而不是静态依赖：两个模块互不 import，装配点负责接线（同文件其它端口的写法）。
	// 断言到页面模块的端口类型本身（而不是内联旧接口）：端口现在要求 …Tx 变体 ——
	// 只有能在调用方事务里标记的来源才能接上来，把「接了但事务各写各的」变成编译期不成立。
	peer, ok := presentationSvc.(pageservice.I18nStalePeer)
	if !ok {
		// 就地断言而不是静默跳过（第五批收口）：漏接的表现是「改了译文，商品页仍旧字节」
		// 而且**没有任何报错** —— 比相邻那条观测缺失更该炸，所以用同一判据。
		// 同时登记到 wiring 的 required-port 清单（见 portPageI18nStalePeer），
		// 让「这条端口没接上」在装配末尾的 mustAllPortsWired 里也能被一次报出来。
		panic("自动发布模块未实现 i18n 失效端口（I18nStalePeer）：译文 / 词条变更不会传导到自动发布实例")
	}
	// pageService 在装配里是契约接口，具体 setter 用断言取（同文件其它端口写法）。
	setter, ok := pageService.(interface {
		SetI18nStalePeer(pageservice.I18nStalePeer)
	})
	if !ok {
		panic("page 模块未暴露 SetI18nStalePeer（i18n 失效扇出的接线点）")
	}
	setter.SetI18nStalePeer(peer)
	marks.mark(portPageI18nStalePeer)
	// 断言而不是静默跳过：漏接的表现是 /readyz 里少了这个来源 —— 运维会把它读成
	// 「这里没有积压」，而实际是「没人在看」。待收敛回执本身就是「线上与库不一致」的窗口，
	// 让它在观测层静默消失是最不该有的降级（审计 CQ-019 的同一判据）。
	presentationObserver, ok := presentationSvc.(pendingReceiptBacklog)
	if !ok {
		panic("自动发布模块未实现待收敛回执观测（PendingReceiptBacklog）")
	}
	a.pendingReceipts.register("presentation", presentationObserver)
	pageObserver, ok := pageService.(pendingReceiptBacklog)
	if !ok {
		panic("页面模块未实现待收敛回执观测（PendingReceiptBacklog）")
	}
	a.pendingReceipts.register("page", pageObserver)
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
	// 执行器按**任务上下文**执行（审计 ARCH-04）：此前只传 SourceID，溢出重建于是退化成
	// 「裸调 Build(ID)」—— 语言集合与旧发布范围全部丢失，第 21 个之后的页面停在默认语言的
	// 暂存态，且与同步路径（RebuildStale）结果不一致。现在把任务冻结的 lang / intent /
	// 输入版本交给页面模块的单页重建编排（与同步路径同一份实现）。
	buildSvc.RegisterExecutor("page", func(ctx context.Context, job *buildcontract.Job) error {
		return pageService.RunPageBuildJob(ctx, &pagedto.PageBuildJobReq{
			ID: job.SourceID, Lang: job.Lang, Intent: job.Intent,
			DraftVersion: job.DraftVersion, BuildInputHash: job.BuildInputHash,
		})
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
	//
	// 建分区是 DDL（需要 public schema 的 CREATE），业务角色（go_wp_app）会被拒 ——
	// 所以它与结构迁移同一条线：database.run_migrations=false（业务角色启动）时一并
	// 跳过，改由运维侧用管理连接定期跑 go run cmd/main.go -migrate-only（已含分区补齐）。
	if config.RunMigrationsEnabled() {
		partition.StartScheduler(context.Background(), a.db)
	}
	// 系统页面槽位解析器接给片段层（BIZ-1）：购物车片段的「去结算」、结算结果的
	// 「查看订单」都要按槽位取路径。传的是 pageService —— 它嵌入了只读的
	// SitePageResolver，发布 / 删除 / 改 URL 那部分能力传不进片段层。
	a.fragDeps.SitePageResolver = pageService
	marks.mark(portRuntimeFragSitePageResolver)
	a.fragDeps.FragmentProject = projectService
	marks.mark(portRuntimeFragProject)
	// 访客订单片段（BIZ-1）：片段端点按访客会话取自己的订单。传的是 orderSvc ——
	// 它嵌入了只读的 VisitorOrderReader，写路径（建单 / 状态流转 / 优惠码管理）
	// 那部分能力传不进片段层。归属校验在 order 模块的 SQL 条件里，不在这层。
	a.fragDeps.VisitorOrderReader = a.orderSvc
	marks.mark(portRuntimeFragVisitorOrderReader)
	// 访客订单地址里的国家/地区名（迁移 501 存的是代码快照）：同一份系统字典只读口，
	// 与后台订单页共用实例（缓存也只有一份）。未注入时片段照常渲染、国家显示代码 ——
	// 所以它不是 required-port，这里的注入不是为了「能力可用」而是为了「显示是人话」。
	a.fragDeps.CountryLabelReader = a.sysConfigDict
	marks.mark(portRuntimeFragCountryLabel)
	// 访客退货片段（RMA）：orderSvc 嵌入了收窄的 VisitorReturnPort（只读申请面，
	// 拿不到「后台审核 / 入库 / 退款」）。此端口此前**从未被任何地方注入** ——
	// 退货申请片段因此恒返回「退货功能暂不可用」（审计 CQ-019：静默降级窗口）。
	a.fragDeps.VisitorReturnProvider = a.orderSvc
	marks.mark(portRuntimeFragVisitorReturn)
	// 访客身份解析中间件：片段端点需要知道「这个请求是谁」。
	// 它与后台的 SessionAuthMiddleware 是两套身份（不同 cookie、不同存储），
	// 挂在片段组上只做「尽力解析」，未登录不阻断 —— 必须登录的能力自己渲染引导文案。
	a.fragDeps.VisitorIdentityMiddleware = userhttp.VisitorIdentityMiddleware(a.userSvc)
	marks.mark(portRuntimeFragVisitorIdentity)
	// 账号中心片段（资料 / 偏好 / 改密码 / 登录设备）：只注入**收窄后的**只读端口 ——
	// 片段层拿不到注册、改密码、踢出设备这些写能力，越权防护靠接口形状。
	a.fragDeps.VisitorAccountPort = a.userSvc
	marks.mark(portRuntimeFragVisitorAccount)
	// 访问面守卫的登录态探针（PIPE-6 AccessGuard，「登录用户可见」）。
	// 传的是收窄后的 UserService（只用到 ResolveVisitorID 一条只读能力）：
	// 守卫拿不到注册 / 改密码 / 踢设备，越权防护靠接口形状。
	// 它只在「该页确实配了 members 守卫」时才被调用，绝大多数静态请求零 Redis 访问。
	builtin.SetAccessGuardLoginProbe(newVisitorLoginProbe(a.userSvc))
	marks.mark(portAccessGuardLoginProbe)
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
	// 结算表单的国家下拉（core.checkoutForm）：从 sys_area 字典取一次（进程内缓存），
	// 两条构建路径共用**同一个来源实例** —— 各建一个会出现「手工页有国家、自动发布页没有」
	// 这种只在某一类页面上暴露的分叉。
	//
	// 装配时机不查库：来源是惰性的，首次构建到含国家字段的页面时才读字典，
	// 失败返回空清单并记日志（组件在「表单里有国家字段」时构建失败，真因在日志里）。
	checkoutCountrySource := sysconfigcheckout.New(a.sysConfigDict)
	if setter, ok := pageService.(interface {
		SetCheckoutCountries(func(ctx context.Context, lang string) []core.CheckoutCountry)
	}); ok {
		setter.SetCheckoutCountries(checkoutCountrySource.Countries)
	} else {
		panic("页面模块未提供结算国家清单注入点（SetCheckoutCountries）")
	}
	marks.mark(portPageCheckoutCountries)
	if setter, ok := presentationSvc.(interface {
		SetCheckoutCountries(func(ctx context.Context, lang string) []core.CheckoutCountry)
	}); ok {
		setter.SetCheckoutCountries(checkoutCountrySource.Countries)
	} else {
		panic("发布实例模块未提供结算国家清单注入点（SetCheckoutCountries）")
	}
	marks.mark(portPresentationCheckoutCountries)
	// 自动发布详情页与手工页面共用站点级装配（EDT-003）：导航 / 槽位 / 响应式图片。
	if setter, ok := presentationSvc.(interface {
		SetNavigationService(navigationcontract.NavigationService)
		SetSitePageResolver(pagecontract.SitePageResolver)
		SetMediaProbe(func(context.Context, string) []mediacontract.VariantRef)
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

	// 换图失效通知（媒体变体的缓存与失效）：media 拿到「谁引用了这张图」后回调引用方
	// 标记待重建。变体文件名带内容指纹（这是 /storage 敢给 immutable 长缓存的前提），
	// 换图产出**一组新文件名**、旧文件按设计保留 —— 没有这一步，旧 URL 会一直返回旧字节，
	// 换图对访客等于没发生（访客端 srcset 选中的多数是变体而不是 src）。
	//
	// 类型断言而不是静态依赖：media 不认识 page / presentation（依赖方向相反），
	// 由装配点接线。每一处漏接都当场炸，并并入 wiring 的 required-port 清单
	//（见 wiringManifest 的 media.SetStaleMarker 两条）。
	pageMarker, ok := pageService.(mediacontract.StaleMarker)
	if !ok {
		panic("页面模块未实现媒体换图失效端口（mediacontract.StaleMarker）：换图后已发布页面永不更新")
	}
	presentationMarker, ok := presentationSvc.(mediacontract.StaleMarker)
	if !ok {
		panic("发布实例模块未实现媒体换图失效端口（mediacontract.StaleMarker）：换图后自动发布详情页永不更新")
	}
	mediaSetter, ok := mediaSvc.(interface {
		SetStaleMarkers(...mediacontract.StaleMarker)
	})
	if !ok {
		panic("媒体模块未提供换图失效通知注入点（SetStaleMarkers）")
	}
	mediaSetter.SetStaleMarkers(pageMarker, presentationMarker)
	marks.mark(portMediaStaleMarkerPage)
	marks.mark(portMediaStaleMarkerPresentation)
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
	productSvc := a.productSvc
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
			Info("检测到组件已更新：相关页面已标记待重建并开始自动重建")
		// 自动重建（异步）：部署新组件后全站产物都是旧组件渲染的字节，这一批**没有别的事件
		// 会再来触发**（组件是编译进二进制的，不存在「下次保存」）—— 只标记的结果就是
		// 一直躺在待重建清单里等一个不存在的人工入口。异步执行，不拖启动链。
		// 单次上限与溢出入队由 RebuildStale 自己处理（超限部分交给构建队列）。
		rebuildStaleAsync("init", pageService, marked)
	}
	// 自动发布实例的同一条启动收敛（报告 ARCH-03）：它同样保存 registry_version，
	// 但此前没有任何入口据此比对 —— 组件升级后商品详情页一直是旧字节，后台看不到 stale、
	// 日志里也没有提示（保存版本号本身不触发任何判定）。判据与上面逐字一致：
	// 只看语言账本（presentation_publications）当前指向的产物，历史产物行不参与。
	// 失败同样不阻断启动。
	if marked, verr := presentationSvc.MarkStaleByRegistryVersion(context.Background(), builder.RegistryVersion()); verr != nil {
		logger.Scene("init").Error(verr, "自动发布实例组件版本比对失败（不阻断启动）")
	} else if len(marked) > 0 {
		logger.Scene("init").With("count", len(marked)).With("registryVersion", builder.RegistryVersion()).
			Info("检测到组件已更新：相关自动发布实例已标记待重建并开始自动重建")
		rebuildStaleAsync("init", presentationStaleRebuilder(presentationSvc), marked)
	}
	// 依赖 fan-out（PIPE-3，docs/03-pipeline.md §8.2）：内容实体变更 → 按依赖表
	// 反查受影响产物 → 精确标记 stale（不再是全站标记）→ 自动重建。
	//
	// 装配顺序要求：page 服务必须先装配完成（作为失效目标与重建实现），
	// 再由内容服务持有扇出端口；presentation 侧待其 DB 持久化对齐后接入同一 Fanout。
	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePage, pageService)
	// 自动发布实例同样是依赖失效目标（审计 AR2-001）：它实现了 MarkStaleByDependency 与
	// RebuildStale，但此前**没有被注册进扇出** —— 页面上引用的文章 / 商品更新时，手工 Page
	// 会重建、自动发布的详情页不会：数据库里的内容 revision 已经变了，访问面继续提供旧字节，
	// 全程没有任何报错。注册后 presentation 侧的失效按同一套语义走（PRES-020 之后是入队，
	// 不在触发进程里同步重建）。
	fanout.Register(pipeline.SourceTypePresentation, presentationSvc)
	// 漏注册一个发布来源不会报错（审计 AR2-001）：它只是永远不参与失效标记与自动重建，
	// 表现是「内容更新了、那一类页面停在旧版本」，日志里什么都没有。所以这里显式断言
	// 期望的来源都在 —— 把「静默少接一个」变成启动即失败，而不是等到线上内容陈旧才被发现。
	registeredSources := fanout.RegisteredSourceTypes()
	for _, want := range []string{pipeline.SourceTypePage, pipeline.SourceTypePresentation} {
		found := false
		for _, got := range registeredSources {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			panic("依赖扇出缺少发布来源 " + want + "：该来源的内容更新既不会被标记也不会自动重建（审计 AR2-001）")
		}
	}
	// SetRebuilder 对 nil 是**静默 no-op**（「未绑定时只标记不重建」）—— 若 pageService
	// 没实现 StaleRebuilder，内容保存会照常成功、stale 也照常标记，只是永远不重建：
	// 线上内容停在旧版本，没有任何报错（审计 CQ-019）。故先断言再注入。
	pageRebuilder, ok := pageService.(pipeline.StaleRebuilder)
	if !ok {
		panic("页面模块未实现依赖失效重建接口（pipeline.StaleRebuilder）")
	}
	fanout.SetRebuilder(pipeline.SourceTypePage, pageRebuilder)
	marks.mark(portPipelinePageRebuilder)
	presentationRebuilder, ok := presentationSvc.(pipeline.StaleRebuilder)
	if !ok {
		panic("自动发布模块未实现依赖失效重建接口（pipeline.StaleRebuilder）")
	}
	fanout.SetRebuilder(pipeline.SourceTypePresentation, presentationRebuilder)
	marks.mark(portPipelinePresentationRebuilder)
	contentSvc.SetDependencyInvalidator(fanout)
	marks.mark(portContentDependencyInvalidator)
	// 商品写路径的静态产物失效（审计 ARCH-01）：与 content 侧同一手法 —— 商品模块只声明
	// 窄端口（productcontract.DependencyInvalidator），扇出实现在发布内核。
	// 断言而非「命中即跳过」：漏接的表现是「改了商品，站点静态产物永不更新」，
	// 而它不会让任何测试变红、日志里也只有事件堆积（事件表有自己的诊断查询）。
	if setter, ok := productSvc.(interface {
		SetDependencyInvalidator(productcontract.DependencyInvalidator)
	}); ok {
		setter.SetDependencyInvalidator(fanout)
	} else {
		panic("商品模块未提供依赖失效端口注入点（SetDependencyInvalidator）")
	}
	marks.mark(portProductDependencyInvalidator)
	// outbox 消费者：商品写事务里落的事件在这里被领取 → 扇出 → 精确标记 + 自动重建。
	// 首跑一次再按间隔轮询（进程重启后积压的事件立刻被消化）。
	if starter, ok := productSvc.(interface {
		StartOutboxWorker(ctx context.Context, interval time.Duration)
	}); ok {
		starter.StartOutboxWorker(context.Background(), 0)
	} else {
		panic("商品模块未提供依赖事件消费入口（StartOutboxWorker）")
	}
	// 内容模板 → 依赖失效接线（触发链）：模板产生新版本 / 切换生效后，按 content_template:{id}
	// 反查引用它的页面与自动发布实例并标记 stale。
	//
	// 不接线 = 前面登记的依赖行永远不会被反查：改了页眉模板，站点仍是旧字节且没有任何报错
	//（本批的核心价值点，接线漏掉在测试里也不会报错）。
	// 契约接口不暴露 setter，故用类型断言取（同文件其它可选端口的写法）。
	// 扇出外面包一层影响面回执适配器：Fanout.Invalidate 丢弃受影响集合，直接接它
	// 就看不到「这次模板换代波及哪些页面 / 实例」—— 而那正是运营改全站页眉时最需要的回执。
	if setter, ok := a.contentTemplateSvc.(interface {
		SetInvalidator(contenttemplateservice.DependencyInvalidator)
	}); ok {
		setter.SetInvalidator(contentTemplateInvalidator{fanout: fanout})
		marks.mark(portContentTemplateInvalidator)
	}
	// 导航变更 → 依赖失效（审计遗留缺口：DepKindMenu 有常量、无发射点）：
	// navigation 侧只表达「哪个工程哪个位置变了」，键构造（pipeline.MenuKey）与扇出
	// 都在发布内核，这里用适配器把两端接起来。键带工程 ID —— 导航是工程级资源，
	// 位置名只有 header/footer，不带工程会让「A 工程改页眉导航」误标 B 工程的页面。
	//
	// 断言 + fail-fast（与 block 的 SetStalePropagator 同一手法）：漏接的表现是
	// 「改导航后已发布页面永远不更新」，页眉/页脚全站可见却没有任何报错。
	navigationDispatcherSetter, ok := navigationSvc.(interface {
		SetMenuStaleDispatcher(navigationcontract.MenuStaleDispatcher)
	})
	if !ok {
		panic("导航模块未提供失效派发注入点（SetMenuStaleDispatcher）")
	}
	navigationDispatcherSetter.SetMenuStaleDispatcher(pipeline.NewMenuStaleAdapter(fanout))
	marks.mark(portNavigationMenuDispatcher)

	// 导航来源实体解析（page/article/product/category/block → 标题 + URL）：
	// 依赖 page/content/presentation/block 契约，故在它们全部装配完成后注入。
	navigationSvc.SetSourceResolver(navsource.New(pageService, contentSvc, presentationSvc, blockSvc))
	marks.mark(portNavigationSourceResolver)
}

// mountAdminPages 后台页面路由（编辑器外壳依赖 page/block/plugin 契约，置于 API 装配之后）。
//
// admin 六领域 CRUD 契约：SetAdminRoutes 返回的 AuthzContextService 动态类型即合并后的
// *Service（同实现全部六接口），此处匿名接口断言获得管理面 CRUD 能力并交给页面装配，
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
	// 各模块后台页面统一在这里接线。两种形态并存（见各模块 admin_pages 文件）：
	//   - 页面只依赖本模块 svc：Setup 追加页面组参数，在模块 Setup 内自注册；
	//   - 页面依赖晚装配契约（content / contenttemplate / product / user / project）：
	//     独立入口，契约齐备后在这里调用。
	// 中间件链（Session + CSRF + 权限上下文）由两个页面组承担：adminPages（/admin 前缀）
	// 与 workbenchPages（根级前缀，编辑器与仪表盘首页），均由 assembly.go 创建。
	// pageService 一并传入：文案词条页改完词条要标记站点待重建（词条在构建期烘进产物字节，
	// 漏接 = 改了文案站点不更新且无报错，与页面 / 商品 / 导航翻译、站点设置同一动作）。
	adminhttp.SetupAdminPages(a.adminPages, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, a.pageService, a.sysConfigDict)
	adminhttp.SetupAdminShellPages(a.router)

	// pageService 作为可选第 4 参传入：块的「待重建影响面」要经 page 的只读反查（引用数 +
	// stale 页面清单）。不接也能编译，但页面会显示「页面能力未装配」且引用数一律「未知」——
	// 那不是 0，是「没能力回答」，所以宁可不接也不能假装查过（见 block_page_impact.go）。
	blockhttp.SetupBlockPages(a.adminPages, a.blockSvc, a.projectService, a.pageService)
	mediahttp.SetupMediaPages(a.adminPages)
	pluginhttp.SetupPluginPages(a.adminPages, a.pluginSvc)
	navigationhttp.SetupNavigationPages(a.adminPages, a.navigationSvc, a.projectService, a.pageService, a.blockSvc)

	contenthttp.SetupContentPages(a.adminPages, a.contentSvc, a.projectService,
		a.contentTemplateSvc, a.pageService, a.presentationSvc)
	contenttemplatehttp.SetupContentTemplatePages(a.adminPages, a.contentTemplateSvc,
		a.projectService, a.productSvc, a.contentSvc)
	// 客户管理页：中间两个参数是 BIZ-3 的会员展示端口（读等级 + 错误文案出口），
	// 详情页的「会员等级」块只读展示；等级与权益按「工程 + 客户」解析，
	// 与同一页的订单摘要用同一个选定工程。
	// 最后两个参数是客户域的订单侧聚合：区间增长（客户概览页用）与分段取 id
	//（列表按「新客 / 回头客 / 复购」筛选用）。两者都在 orderSvc 上。
	// 客户列表的「会员等级」筛选要两个只读端口（按等级反查归属 / 列出可选等级），
	// 它们都在 MembershipService 上：断言失败是装配缺陷（少一个方法），当场 panic
	// 而不是传 nil —— 传 nil 会让筛选静默失效，而页面看起来完全正常。
	membershipAdmin, membershipAdminOK := a.membershipSvc.(membershipcontract.AssignmentAdminPort)
	if !membershipAdminOK {
		panic("会员模块未实现 AssignmentAdminPort（客户列表按等级筛选依赖它），装配缺陷")
	}
	membershipTiers, membershipTiersOK := a.membershipSvc.(membershipcontract.TierAdminPort)
	if !membershipTiersOK {
		panic("会员模块未实现 TierAdminPort（客户列表的等级下拉依赖它），装配缺陷")
	}
	userhttp.SetupCustomerPages(a.adminPages, a.userAdminSvc, a.orderSvc, a.projectService,
		a.membershipSvc, a.membershipFacing, a.orderSvc, a.orderSvc, a.orderSvc, a.orderSvc,
		membershipAdmin, membershipTiers)
	producthttp.SetupProductPages(a.adminPages, a.productSvc, a.projectService,
		a.contentTemplateSvc, a.presentationSvc, a.inventorySvc, a.pageService, a.contentSvc)
	projecthttp.SetupProjectPages(a.adminPages, a.workbenchPages, a.projectService, a.pageService, a.blockSvc, a.sysConfigDict)

	// 编辑器平台（workbench 模块）：仪表盘首页 + /workbench/* 全部路由。
	// contentStore 传 nil：未注入时模块内部惰性回退默认实现（与原行为一致）。
	workbenchHandle := workbenchhttp.SetupWorkbenchRoutes(a.workbenchPages, a.pageService, a.projectService,
		a.blockSvc, a.pluginSvc, a.collectionResolver, a.contentTemplateSvc, a.presentationSvc,
		a.blueprintSvc, a.productSvc, nil)
	// 实例编辑模式（docs/04-C）：?instance= 画布改覆盖文档，保存走 SaveOverrideDocument。
	workbenchHandle.SetInstanceOverrideDeps(a.presentationSvc)
	marks.mark(portWorkbenchInstanceOverrideDeps)
	// 检查器的「具体菜单项」下拉（nav 组件 Props.Navigation，ct=entityref,navigation）。
	workbenchHandle.SetNavigationPicker(a.navigationSvc)
	marks.mark(portWorkbenchNavigationPicker)
	// 概览页的跨模块只读数据：订单聚合（KPI / 趋势 / 榜单）、访问统计（浏览量）、
	// 页面类型（判断哪些浏览发生在文章页上）。三者在此处都已装配完毕
	//（buildAPIAndCoreCRUD → buildIdentityAndCommerce → wireRuntimeAccessFace → 本步）。
	workbenchHandle.SetOverviewPorts(a.orderSvc, a.analyticsSvc, a.pageService)
	marks.mark(portDashboardOverview)
	// 蓝图（审计 VIS-010）已作为 workbench Setup 的参数传入，端口标记保留。
	marks.mark(portDashboardBlueprints)
}

// runSelfCheck 装配自检（审计 CQ-019）：必需端口逐个核对，缺失即 fail-fast 并
// **报出端口名与后果**（一次报出全部缺失项，清单见 wiring.go 的 wiringManifest）；
// 可选端口未接入写进启动日志 —— 降级必须可见，而不是只在代码注释里写一句「未注入时降级」。
func (a *assembly) runSelfCheck() {
	mustAllPortsWired(a.marks)
	// 必需端口全部到位之后才提交片段层依赖快照：顺序保证「自检不过 → 快照根本不提交」，
	// 不会留下一个「装了一半」的包级依赖供后续代码误读。
	runtimefragment.SetDependencies(a.fragDeps)
	// 权限点声明同步（审计 SEC-011）：路由装配完成后，把声明表幂等 upsert 进库
	// （只补缺失的权限点与超管策略，不动人工的角色 / 用户授权，见 internal/permission/sync.go）。
	syncDeclaredPermissions(a.db)
	logDegradedOptional(a.marks)
}

// mountPublicFace 公开端点与兜底路由：运行时片段端点 + 未匹配路由处理。
func (a *assembly) mountPublicFace() {
	// 运行时片段端点（0-D，公开路由：capability 白名单 + 认证策略在 handler 内）。
	runtimefragment.SetupFragmentRoutes(a.router)

	// 未匹配路由的兜底**不在这里设**：访问面挂在根上（站点独占域名根），
	// NoRoute 由 setupStaticFace 一次性装成「站点中间件链 → 静态文件 → 站点 404」。
	// 在这里再设一次会把它整个覆盖掉（gin 的 NoRoute 是单槽位），
	// 表现为首页与全部站内页 404 —— 而控制面看着一切正常。
}

// presentationStaleRebuilder 取自动发布实例的重建器。
//
// 为什么需要它：presentation 契约只声明了 MarkStaleByDependency，RebuildStale 属于
// pipeline 端口；而「标记了却不重建」正是审计 CQ-019 记的那类静默降级
// （内容陈旧、日志干净）。断言失败即 panic —— 装配期缺件应当早失败。
func presentationStaleRebuilder(svc presentationcontract.PresentationService) pipeline.StaleRebuilder {
	r, ok := svc.(pipeline.StaleRebuilder)
	if !ok {
		panic("自动发布模块未实现依赖失效重建接口（pipeline.StaleRebuilder）")
	}
	return r
}
