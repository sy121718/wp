package routers

import (
	"context"
	"go_wp/config"
	"go_wp/internal/builder/extensions/presets"
	mw "go_wp/pkg/middleware"
	publish "go_wp/pkg/publish"
	"strings"
	"time"

	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	artifactcontract "go_wp/internal/module/artifact/contract"
	blockcontract "go_wp/internal/module/block/contract"
	blockhttp "go_wp/internal/module/block/inbound/http"
	blockmcp "go_wp/internal/module/block/inbound/mcp"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	buildcontract "go_wp/internal/module/build/contract"
	contentcontract "go_wp/internal/module/content/contract"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contentservice "go_wp/internal/module/content/service"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	mediacontract "go_wp/internal/module/media/contract"
	mediahttp "go_wp/internal/module/media/inbound/http"
	membershipcontract "go_wp/internal/module/membership/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navsource "go_wp/internal/module/navigation/outbound/source"
	navigationservice "go_wp/internal/module/navigation/service"
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
	sysconfigcheckout "go_wp/internal/module/sysconfig/outbound/checkoutcountries"
	userhttp "go_wp/internal/module/user/inbound/http"
	workbenchhttp "go_wp/internal/module/workbench/inbound/http"
	"go_wp/internal/partition"
	"go_wp/internal/pipeline"
	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

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

	presetSetter, ok := blockSvc.(interface {
		SetPresetResolver(blockcontract.PresetResolver)
	})
	if !ok {
		panic("区块模块未实现受控预设解析器注入")
	}
	presetSetter.SetPresetResolver(presets.BusinessPresetRegistry{})
	marks.mark(portBlockPresetResolver)

	presentationSvc := presentationhttp.SetupPresentationRoutes(authorizedAPI, db, contentTemplateSvc, entityRegistry, projectService, blockSvc, collectionRegistry, publicationSvc)

	pluginSvc := pluginhttp.SetupPluginRoutes(authorizedAPI, db, adminAuthzSvc)

	marks.mark(portPluginAdminAuthz)

	collectionResolver := core.CollectionResolver(collectionRegistry)

	contentSearch, ok := contentSvc.(contentcontract.SearchPort)
	if !ok {
		panic("内容模块未实现检索端口（SearchPort）")
	}
	marks.mark(portPublicSearchContent)
	productSearch, ok := productSvc.(productcontract.SearchPort)
	if !ok {
		panic("商品模块未实现检索端口（SearchPort）")
	}
	marks.mark(portPublicSearchProduct)
	publishedLocator, ok := presentationSvc.(presentationcontract.PublishedEntityLocator)
	if !ok {
		panic("自动发布模块未实现已上线路径解析端口（PublishedEntityLocator）")
	}
	marks.mark(portPublicSearchIndex)

	if setter, ok := productSvc.(interface {
		SetPublishedEntityLocator(presentationcontract.PublishedEntityLocator)
	}); ok {
		setter.SetPublishedEntityLocator(publishedLocator)
	} else {
		panic("商品模块未提供已上线路径解析注入点（SetPublishedEntityLocator）")
	}
	marks.mark(portProductPublishedLocator)
	publicIndex, ok := presentationSvc.(presentationcontract.PublishedEntityIndex)
	if !ok {
		panic("自动发布模块未实现公开实体索引")
	}
	contenthttp.SetupContentPublicRoutes(a.router, contentservice.NewPublicSearchService(contentSearch, productSearch, publicIndex))

	if setter, ok := productSvc.(interface {
		SetArchiveInstanceEnsurer(presentationcontract.ArchiveInstanceEnsurer)
	}); ok {
		setter.SetArchiveInstanceEnsurer(presentationSvc)
	} else {
		panic("商品模块未提供归档页创建注入点（SetArchiveInstanceEnsurer）")
	}
	marks.mark(portProductArchiveEnsurer)

	pageService := pagehttp.SetupPageRoutes(authorizedAPI, db, artifactSvc, publicationSvc, projectService, blockSvc, pluginSvc, collectionResolver, navigationSvc, mediaSvc, a.adminPages, a.workbenchPages)

	if pageTools, err := pagemcp.Tools(pageService); err != nil {
		panic("页面工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(pageTools...); err != nil {
		panic("页面工具注册失败：" + err.Error())
	}

	if pageWriteTools, err := pagemcp.WriteTools(pageService); err != nil {
		panic("页面写工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(pageWriteTools...); err != nil {
		panic("页面写工具注册失败：" + err.Error())
	}

	if blockQueryTools, err := blockmcp.QueryTools(blockSvc); err != nil {
		panic("全局块工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(blockQueryTools...); err != nil {
		panic("全局块工具注册失败：" + err.Error())
	}
	if blockWriteTools, err := blockmcp.WriteTools(blockSvc, blockSvc,
		aiservice.NewToolIdempotencyStore(aimodel.NewToolIdempotencyModel(db))); err != nil {
		panic("全局块写工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(blockWriteTools...); err != nil {
		panic("全局块写工具注册失败：" + err.Error())
	}

	pageTemplateSetter, ok := pageService.(interface {
		SetStructureTemplatePort(pipeline.StructureTemplatePort)
	})
	if !ok {
		panic("page 模块未提供结构模板端口注入点（SetStructureTemplatePort）")
	}
	pageTemplateSetter.SetStructureTemplatePort(structureTemplatePortAdapter{svc: contentTemplateSvc})
	marks.mark(portPageStructureTemplates)

	if setter, ok := blockSvc.(interface {
		SetStalePropagator(func(ctx context.Context, blockID string) error)
	}); ok {

		presentationRebuilder, ok := presentationSvc.(publish.StaleRebuilder)
		if !ok {
			panic("自动发布模块未实现依赖失效重建接口（publish.StaleRebuilder）")
		}
		setter.SetStalePropagator(BlockStalePropagator(pageService, projectService, presentationSvc, presentationRebuilder))
		marks.mark(portBlockStalePropagator)
	} else {
		panic("block 模块未提供 stale 传播注入点（SetStalePropagator）")
	}

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

	a.productHandle.SetDetailTemplateModePort(a.presentationSvc)
	marks.mark(portProductDetailTemplateMode)
	a.pluginSvc = pluginSvc
	a.collectionResolver = collectionResolver
	a.pageService = pageService

	peer, ok := presentationSvc.(pageservice.I18nStalePeer)
	if !ok {

		panic("自动发布模块未实现 i18n 失效端口（I18nStalePeer）：译文 / 词条变更不会传导到自动发布实例")
	}

	setter, ok := pageService.(interface {
		SetI18nStalePeer(pageservice.I18nStalePeer)
	})
	if !ok {
		panic("page 模块未暴露 SetI18nStalePeer（i18n 失效扇出的接线点）")
	}
	setter.SetI18nStalePeer(peer)
	marks.mark(portPageI18nStalePeer)

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

	retire, ok := pageService.(projectcontract.LocaleRetirePort)
	if !ok {
		panic("页面模块未实现语言下线端口（LocaleRetirePort）")
	}
	projectService.SetLocaleRetirePort(retire)
	marks.mark(portProjectLocaleRetire)

	artifactOwnerSetter, ok := pageService.(interface {
		SetExternalArtifactOwners(func(ctx context.Context) ([]string, error))
	})
	if !ok {
		panic("页面模块未提供产物属主注入点（SetExternalArtifactOwners）")
	}
	artifactOwnerSetter.SetExternalArtifactOwners(presentationSvc.ListArtifactHashes)
	marks.mark(portPageExternalArtifactOwners)

	artifactReader, ok := a.artifactSvc.(artifactcontract.PageArtifactReader)
	if !ok {
		panic("产物模块未实现页面产物只读视图（PageArtifactReader）")
	}
	pageArtifactSetter, ok := pageService.(interface {
		SetPageArtifacts(artifactcontract.PageArtifactReader)
	})
	if !ok {
		panic("页面模块未提供页面产物只读视图注入点（SetPageArtifacts）")
	}
	pageArtifactSetter.SetPageArtifacts(artifactReader)
	marks.mark(portPageArtifactReader)

	if bpSetter, ok := pageService.(interface {
		SetBlueprints(blueprintcontract.BlueprintService)
	}); ok {
		bpSetter.SetBlueprints(a.blueprintSvc)
	} else {
		panic("页面模块未提供蓝图注入点（SetBlueprints）")
	}
	marks.mark(portPageBlueprints)

	if queueSetter, ok := pageService.(interface {
		SetBuildQueue(pagecontract.BuildQueueEnqueuer)
	}); ok {
		queueSetter.SetBuildQueue(buildSvc)
	} else {
		panic("页面模块未提供构建队列注入点（SetBuildQueue）")
	}
	marks.mark(portPageBuildQueue)

	buildSvc.RegisterExecutor("page", func(ctx context.Context, job *buildcontract.Job) error {
		return pageService.RunPageBuildJob(ctx, &pagedto.PageBuildJobReq{
			ID: job.SourceID, Lang: job.Lang, Intent: job.Intent,
			DraftVersion: job.DraftVersion, BuildInputHash: job.BuildInputHash,
		})
	})

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

	registerMenuStaleExecutor(buildSvc, pageService, presentationSvc)

	buildSvc.StartWorkers(context.Background(), 2)

	if config.RunMigrationsEnabled() {
		partition.StartScheduler(context.Background(), a.db)
	}

	builtin.SetAccessGuardLoginProbe(newVisitorLoginProbe(a.userSvc))
	marks.mark(portAccessGuardLoginProbe)

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

	if setter, ok := presentationSvc.(interface {
		SetNavigationService(navigationcontract.NavigationService)
		SetMediaProbe(func(context.Context, string) []mediacontract.VariantRef)
		SetPluginService(plugincontract.PluginService)
	}); ok {
		setter.SetNavigationService(navigationSvc)
		setter.SetMediaProbe(mediaSvc.ProbeImageVariants)
		setter.SetPluginService(pluginSvc)
	} else {
		panic("发布实例模块未提供站点装配注入点（SetNavigationService / SetMediaProbe / SetPluginService）")
	}
	marks.mark(portPresentationSiteAssembly)
	pageActionTargets, pageOK := pageService.(pagecontract.PublishedPageLocator)
	instanceActionTargets, instanceOK := presentationSvc.(presentationcontract.PublishedInstanceLocator)
	pageActionSetter, pageSetterOK := pageService.(interface {
		SetPublishedInstanceLocator(presentationcontract.PublishedInstanceLocator)
	})
	instanceActionSetter, instanceSetterOK := presentationSvc.(interface {
		SetPublishedPageLocator(pagecontract.PublishedPageLocator)
	})
	if !pageOK || !instanceOK || !pageSetterOK || !instanceSetterOK {
		panic("发布动作目标解析端口未装配")
	}
	pageActionSetter.SetPublishedInstanceLocator(instanceActionTargets)
	marks.mark(portPagePublishedInstanceLocator)
	instanceActionSetter.SetPublishedPageLocator(pageActionTargets)
	marks.mark(portPresentationPublishedPageLocator)

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

func (a *assembly) startRuntimeTasks() {
	marks := a.marks
	blockSvc := a.blockSvc
	contentSvc := a.contentSvc
	navigationSvc := a.navigationSvc
	pageService := a.pageService
	productSvc := a.productSvc
	presentationSvc := a.presentationSvc
	projectService := a.projectService

	if fixed, terr := projectService.EnsureDefaultThemes(context.Background()); terr != nil {
		logger.Scene("init").Error(terr, "默认主题补齐失败（不阻断启动）")
	} else if fixed > 0 {
		logger.Scene("init").With("count", fixed).Info("已为无主题工程补默认主题（后台风格色值）")
	}

	if marked, verr := pageService.MarkStaleByRegistryVersion(context.Background(), builder.RegistryVersion()); verr != nil {
		logger.Scene("init").Error(verr, "组件版本比对失败（不阻断启动）")
	} else if len(marked) > 0 {
		logger.Scene("init").With("count", len(marked)).With("registryVersion", builder.RegistryVersion()).
			Info("检测到组件已更新：相关页面已标记待重建并开始自动重建")

		rebuildStaleAsync("init", pageService, marked)
	}

	if marked, verr := presentationSvc.MarkStaleByRegistryVersion(context.Background(), builder.RegistryVersion()); verr != nil {
		logger.Scene("init").Error(verr, "自动发布实例组件版本比对失败（不阻断启动）")
	} else if len(marked) > 0 {
		logger.Scene("init").With("count", len(marked)).With("registryVersion", builder.RegistryVersion()).
			Info("检测到组件已更新：相关自动发布实例已标记待重建并开始自动重建")
		rebuildStaleAsync("init", presentationStaleRebuilder(presentationSvc), marked)
	}

	fanout := publish.NewFanout()
	fanout.Register(publish.SourceTypePage, pageService)

	fanout.Register(publish.SourceTypePresentation, presentationSvc)

	registeredSources := fanout.RegisteredSourceTypes()
	for _, want := range []string{publish.SourceTypePage, publish.SourceTypePresentation} {
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

	pageRebuilder, ok := pageService.(publish.StaleRebuilder)
	if !ok {
		panic("页面模块未实现依赖失效重建接口（publish.StaleRebuilder）")
	}
	fanout.SetRebuilder(publish.SourceTypePage, pageRebuilder)
	marks.mark(portPipelinePageRebuilder)
	presentationRebuilder, ok := presentationSvc.(publish.StaleRebuilder)
	if !ok {
		panic("自动发布模块未实现依赖失效重建接口（publish.StaleRebuilder）")
	}
	fanout.SetRebuilder(publish.SourceTypePresentation, presentationRebuilder)
	marks.mark(portPipelinePresentationRebuilder)
	wireActionTargetReferences(pageService, blockSvc, a.contentTemplateSvc, presentationSvc, fanout)
	marks.mark(portActionTargetReferences)
	contentSvc.SetDependencyInvalidator(fanout)
	marks.mark(portContentDependencyInvalidator)

	if setter, ok := productSvc.(interface {
		SetDependencyInvalidator(productcontract.DependencyInvalidator)
	}); ok {
		setter.SetDependencyInvalidator(fanout)
	} else {
		panic("商品模块未提供依赖失效端口注入点（SetDependencyInvalidator）")
	}
	marks.mark(portProductDependencyInvalidator)

	if starter, ok := productSvc.(interface {
		StartOutboxWorker(ctx context.Context, interval time.Duration)
	}); ok {
		starter.StartOutboxWorker(context.Background(), 0)
	} else {
		panic("商品模块未提供依赖事件消费入口（StartOutboxWorker）")
	}

	if setter, ok := a.contentTemplateSvc.(interface {
		SetInvalidator(contenttemplateservice.DependencyInvalidator)
	}); ok {
		setter.SetInvalidator(contentTemplateInvalidator{fanout: fanout})
		marks.mark(portContentTemplateInvalidator)
	}

	navigationDispatcherSetter, ok := navigationSvc.(interface {
		SetMenuStaleDispatcher(navigationcontract.MenuStaleDispatcher)
	})
	if !ok {
		panic("导航模块未提供失效派发注入点（SetMenuStaleDispatcher）")
	}
	navigationDispatcherSetter.SetMenuStaleDispatcher(pipeline.NewMenuStaleAdapter(fanout))
	marks.mark(portNavigationMenuDispatcher)

	navigationSvc.SetSourceResolver(navsource.New(pageService, contentSvc, presentationSvc, blockSvc))
	marks.mark(portNavigationSourceResolver)

	if a.navigationHandle == nil {

		panic("导航 JSON 入口句柄未装配（SetupNavigationRoutes 返回值未接上）")
	}
	contentWriter, werr := i18n.NewContentWriterDefault()
	if werr != nil {

		panic("译文存储（i18n.ContentWriter）不可用：" + werr.Error())
	}
	if _, ok := pageService.(pagecontract.PageMenuStalePort); !ok {
		panic("页面模块未实现同事务菜单标记端口（PageMenuStalePort）")
	}
	if _, ok := pageService.(navigationMenuItemStale); !ok {
		panic("页面模块未实现同事务**按菜单项**标记（navigation 契约 MenuItemStalePort）")
	}
	if _, ok := a.buildSvc.(interface {
		EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (int64, error)
	}); !ok {
		panic("构建模块未实现 menu_stale 同事务入队（MenuStaleEnqueuePort）")
	}

	if setter, ok := navigationSvc.(interface {
		SetWritePorts(navigationservice.NavigationWriteDeps)
	}); ok {
		setter.SetWritePorts(navigationWritePorts(pageService, presentationSvc, a.buildSvc))
		marks.mark(portNavigationMenuWritePorts)
	} else {
		panic("导航模块未提供写端口注入点（SetWritePorts）")
	}
	if _, ok := presentationSvc.(presentationcontract.PresentationStalePort); !ok {
		panic("自动发布实例模块未实现同事务菜单标记端口（PresentationStalePort）")
	}
	a.navigationHandle.SetTranslationWriteService(newNavigationTranslationWriter(
		a.db, navigationSvc, projectService, pageService, presentationSvc, a.buildSvc, contentWriter))
	markNavigationTranslationWriteWired(marks, pageService, presentationSvc, a.buildSvc)
}

func markNavigationTranslationWriteWired(marks wiringMarks, pageSvc pagecontract.PageService,
	presentationSvc presentationcontract.PresentationService, buildSvc buildcontract.BuildService) {
	marks.mark(portNavigationTranslationWriter)
	if _, ok := pageSvc.(pagecontract.PageMenuStalePort); ok {
		marks.mark(portNavigationPageStalePort)
	}
	if _, ok := presentationSvc.(presentationcontract.PresentationStalePort); ok {
		marks.mark(portNavigationInstanceStalePort)
	}
	if _, ok := buildSvc.(interface {
		EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (int64, error)
	}); ok {
		marks.mark(portNavigationMenuStaleEnqueue)
	}
}

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

	adminhttp.SetupAdminPages(a.adminPages, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, a.pageService, a.sysConfigDict)
	adminhttp.SetupAdminShellPages(a.router)

	blockhttp.SetupBlockPages(a.adminPages, a.blockSvc, a.projectService, a.pageService)
	mediahttp.SetupMediaPages(a.adminPages)
	pluginhttp.SetupPluginPages(a.adminPages, a.pluginSvc)
	navigationhttp.SetupNavigationPages(a.adminPages, a.navigationSvc, a.projectService, a.pageService, a.blockSvc)

	contenthttp.SetupContentPages(a.adminPages, a.contentSvc, a.projectService,
		a.contentTemplateSvc, a.pageService, a.presentationSvc)
	contenttemplatehttp.SetupContentTemplatePages(a.adminPages, a.contentTemplateSvc,
		a.projectService, a.productSvc, a.contentSvc)

	membershipAdmin, membershipAdminOK := a.membershipSvc.(membershipcontract.AssignmentAdminPort)
	if !membershipAdminOK {
		panic("会员模块未实现 AssignmentAdminPort（客户列表按等级筛选依赖它），装配缺陷")
	}
	membershipTiers, membershipTiersOK := a.membershipSvc.(membershipcontract.TierAdminPort)
	if !membershipTiersOK {
		panic("会员模块未实现 TierAdminPort（客户列表的等级下拉依赖它），装配缺陷")
	}
	userhttp.SetupCustomerPages(a.adminPages, a.userAdminSvc, a.orderSvc, a.projectService,
		a.membershipSvc, a.orderSvc, a.orderSvc, a.orderSvc, a.orderSvc,
		membershipAdmin, membershipTiers)
	producthttp.SetupProductPages(a.adminPages, a.authorizedAPI, a.productHandle, a.productSvc, a.productReader, a.projectService,
		a.contentTemplateSvc, a.presentationSvc, a.inventorySvc, a.pageService, a.contentSvc)

	producthttp.SetupProductTranslationRoutes(a.authorizedAPI, a.adminPages,
		a.productSvc, a.pageService, a.presentationSvc)

	marks.mark(portProductPageWarehouses)
	marks.mark(portProductPageTemplates)
	marks.mark(portProductPageDetailTemplate)

	marks.mark(portProductDetailTemplateWriteDeps)

	marks.mark(portAdminI18nPageMarker)
	projecthttp.SetupProjectPages(a.adminPages, a.workbenchPages, a.projectService, a.pageService, a.blockSvc, a.sysConfigDict)

	workbenchHandle := workbenchhttp.SetupWorkbenchRoutes(a.workbenchPages, a.pageService, a.projectService,
		a.blockSvc, a.pluginSvc, a.collectionResolver, a.contentTemplateSvc, a.presentationSvc,
		a.blueprintSvc, a.productSvc, nil)

	workbenchHandle.SetInstanceOverrideDeps(a.presentationSvc)
	marks.mark(portWorkbenchInstanceOverrideDeps)

	workbenchHandle.SetNavigationPicker(a.navigationSvc)
	marks.mark(portWorkbenchNavigationPicker)

	workbenchHandle.SetOverviewPorts(a.orderSvc, a.analyticsSvc, a.pageService, a.sysConfigDict)
	marks.mark(portDashboardOverview)

	marks.mark(portDashboardBlueprints)
}

func (a *assembly) runSelfCheck() {
	mustAllPortsWired(a.marks)

	a.verifyPermissionDirectory()
	logDegradedOptional(a.marks)
}

// verifyPermissionDirectory 在全部路由与工具登记后核对权限目录。
//
// 不一致时只记录并让就绪检查失败：不启动反写、不自动覆盖用户定义，
// 也不留「先启动再补库」的窗口。
func (a *assembly) verifyPermissionDirectory() {
	dir := newAdminPermDirectory(a.adminAuthzSvc)
	problems := verifyDeclaredPermissions(a.bgCtx, dir)
	if len(problems) == 0 {
		if ex := mw.Exempts(); len(ex) > 0 {
			logger.Scene("init").With("count", len(ex)).
				Info("以下路由显式豁免权限点（check-permission-gaps.sh 的 EXEMPT 名单应对应）：" + strings.Join(ex, "; "))
		}
		return
	}
	a.permProblems = problems
	logger.Scene("init").With("count", len(problems)).Error(nil,
		"权限目录与路由声明不一致，实例不就绪："+strings.Join(problems, "; "))
}

func (a *assembly) mountPublicFace() {
	producthttp.SetupProductPublicRoutes(a.router, a.productSvc)
	producthttp.SetupProductCatalogRoutes(a.router, a.productSvc)
	producthttp.SetupProductOptionsRoutes(a.router, a.productSvc)

}

func presentationStaleRebuilder(svc presentationcontract.PresentationService) publish.StaleRebuilder {
	r, ok := svc.(publish.StaleRebuilder)
	if !ok {
		panic("自动发布模块未实现依赖失效重建接口（publish.StaleRebuilder）")
	}
	return r
}
