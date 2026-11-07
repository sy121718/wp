package routers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"go_wp/config"
	"go_wp/pkg/mcp"
	"go_wp/internal/middleware/adminctx"
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	aicontract "go_wp/internal/module/ai/contract"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimcp "go_wp/internal/module/ai/inbound/mcp"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticshttp "go_wp/internal/module/analytics/inbound/http"
	analyticsmcp "go_wp/internal/module/analytics/inbound/mcp"
	artifactcontract "go_wp/internal/module/artifact/contract"
	artifacthttp "go_wp/internal/module/artifact/inbound/http"
	blockcontract "go_wp/internal/module/block/contract"
	blockhttp "go_wp/internal/module/block/inbound/http"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprinthttp "go_wp/internal/module/blueprint/inbound/http"
	buildcontract "go_wp/internal/module/build/contract"
	buildhttp "go_wp/internal/module/build/inbound/http"
	carthttp "go_wp/internal/module/cart/inbound/http"
	mockpaypal "go_wp/internal/module/cart/outbound/mockpaypal"
	cartservice "go_wp/internal/module/cart/service"
	commentcontract "go_wp/internal/module/comment/contract"
	commenthttp "go_wp/internal/module/comment/inbound/http"
	commentmcp "go_wp/internal/module/comment/inbound/mcp"
	captcharouter "go_wp/internal/module/common/captcha/router"
	contentcontract "go_wp/internal/module/content/contract"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contentmcp "go_wp/internal/module/content/inbound/mcp"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	inventorymcp "go_wp/internal/module/inventory/inbound/mcp"
	inventorymodel "go_wp/internal/module/inventory/model"
	mediaatt "go_wp/internal/module/inventory/outbound/mediaatt"
	orderstock "go_wp/internal/module/inventory/outbound/orderstock"
	purchasestock "go_wp/internal/module/inventory/outbound/purchasestock"
	inventoryservice "go_wp/internal/module/inventory/service"
	mailcontract "go_wp/internal/module/mail/contract"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmcp "go_wp/internal/module/mail/inbound/mcp"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatahttp "go_wp/internal/module/masterdata/inbound/http"
	mediacontract "go_wp/internal/module/media/contract"
	mediahttp "go_wp/internal/module/media/inbound/http"
	mediamcp "go_wp/internal/module/media/inbound/mcp"
	membershipcontract "go_wp/internal/module/membership/contract"
	membershiphttp "go_wp/internal/module/membership/inbound/http"
	membershipmcp "go_wp/internal/module/membership/inbound/mcp"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navigationmcp "go_wp/internal/module/navigation/inbound/mcp"
	ordercontract "go_wp/internal/module/order/contract"
	orderhttp "go_wp/internal/module/order/inbound/http"
	ordermcp "go_wp/internal/module/order/inbound/mcp"
	orderservice "go_wp/internal/module/order/service"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmcp "go_wp/internal/module/product/inbound/mcp"
	projectcontract "go_wp/internal/module/project/contract"
	projecthttp "go_wp/internal/module/project/inbound/http"
	projectmcp "go_wp/internal/module/project/inbound/mcp"
	pubcontract "go_wp/internal/module/publication/contract"
	pubhttp "go_wp/internal/module/publication/inbound/http"
	purchasecontract "go_wp/internal/module/purchase/contract"
	purchasehttp "go_wp/internal/module/purchase/inbound/http"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfighttp "go_wp/internal/module/sysconfig/inbound/http"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	sysconfigi18nvalues "go_wp/internal/module/sysconfig/outbound/i18nvalues"
	sysconfigservice "go_wp/internal/module/sysconfig/service"
	usercontract "go_wp/internal/module/user/contract"
	userhttp "go_wp/internal/module/user/inbound/http"
	usermcp "go_wp/internal/module/user/inbound/mcp"
	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookhttp "go_wp/internal/module/webhook/inbound/http"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/response"
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/migrations"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// verifyDeclaredPermissions 在路由与工具登记完成后核对权限目录：
// 每条已声明的绑定（含页面复用的 AuthObject/AuthMethod）必须在库里存在、启用，
// 且路径/方法与登记事实一致。不一致只报告并让就绪检查失败，不再启动反写。
//
// 这样既没有「先启动再写回」的窗口，也不会在冲突时自动挑一条覆盖用户定义。
func verifyDeclaredPermissions(ctx context.Context, dir aicontract.PermDirectory) []string {
	specs := mw.Snapshot()
	if len(specs) == 0 {
		return []string{"没有任何已声明的路由绑定：装配缺陷"}
	}
	codes := make([]string, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Perm == "" {
			continue
		}
		if _, dup := seen[spec.Perm]; dup {
			continue
		}
		seen[spec.Perm] = struct{}{}
		codes = append(codes, spec.Perm)
	}
	if dir == nil {
		return []string{"权限目录端口未注入：不能核对权限点落库"}
	}
	entries, err := dir.List(ctx, codes)
	if err != nil {
		return []string{"读取权限目录失败：" + err.Error()}
	}
	byCode := make(map[string]aicontract.PermEntry, len(entries))
	for _, e := range entries {
		byCode[e.Code] = e
	}
	var problems []string
	for _, spec := range specs {
		if spec.Perm == "" {
			continue
		}
		obj, act := spec.AuthObject, spec.AuthMethod
		if obj == "" {
			obj = spec.Path
		}
		if act == "" {
			act = spec.Method
		}
		e, ok := byCode[spec.Perm]
		if !ok {
			problems = append(problems, spec.Perm+" 未在权限目录中登记（"+act+" "+obj+"）")
			continue
		}
		if e.Status != 1 {
			problems = append(problems, spec.Perm+" 在权限目录中已禁用（"+act+" "+obj+"）")
			continue
		}
		if e.Path != obj || e.Method != act {
			problems = append(problems, spec.Perm+" 目录记的是 "+e.Method+" "+e.Path+
				"，注册处通过的是 "+act+" "+obj)
		}
	}
	return problems
}

type assembly struct {
	router *gin.Engine
	marks  wiringMarks

	// bgCtx 是装配阶段启动的后台循环共用的 ctx：进程收尾时取消它，循环立刻停。
	bgCtx context.Context

	db *gorm.DB

	pendingReceipts pendingReceiptRegistry

	api *gin.RouterGroup

	authorizedAPI *mw.RouteGroup

	sysConfigSvc sysconfigcontract.Service

	sysConfigDict sysconfigcontract.DictReader

	adminPages *gin.RouterGroup

	workbenchPages *gin.RouterGroup

	adminAuthzSvc admincontract.AuthzContextService

	// permProblems 是启动期权限目录核对出的不一致项；非空时 /readyz 返回 not_ready。
	permProblems []string

	mediaSvc       mediacontract.MediaService
	projectService projectcontract.ProjectService
	blockSvc       blockcontract.BlockService
	artifactSvc    artifactcontract.ArtifactService
	publicationSvc pubcontract.PublicationService
	buildSvc       buildcontract.BuildService
	blueprintSvc   blueprintcontract.BlueprintService
	navigationSvc  navigationcontract.NavigationService

	navigationHandle *navigationhttp.Handle

	collectionRegistry core.CollectionRegistry
	contentSvc         contentcontract.ContentService
	entityRegistry     core.EntitySourceRegistry

	toolRegistry       *mcp.Registry
	contentTemplateSvc contenttemplatecontract.ContentTemplateService
	masterdataSvc      masterdatacontract.MasterDataService
	inventorySvc       inventorycontract.InventoryService
	purchaseSvc        purchasecontract.PurchaseService
	productSvc         productcontract.ProductService

	productReader productcontract.ProductPageReader
	productHandle *producthttp.ProductHandle

	mailSvc      mailcontract.MailService
	userSvc      usercontract.UserService
	userAdminSvc usercontract.CustomerAdminPort

	orderSvc ordercontract.OrderService

	membershipSvc membershipcontract.MembershipService
	analyticsSvc  analyticscontract.AnalyticsService

	presentationSvc    presentationcontract.PresentationService
	pluginSvc          plugincontract.PluginService
	collectionResolver core.CollectionResolver
	pageService        pagecontract.PageService

	availabilityLookup productcontract.VariantAvailabilityLookupPort

	commentSvc commentcontract.CommentService
}

// adminSideEffectStarter 是 admin 模块提供的副作用重放入口。
//
// 装配端只认这个最小接口：ctx 取消即停，interval<=0 用模块自己的默认间隔。
type adminSideEffectStarter interface {
	StartSideEffectWorker(ctx context.Context, interval time.Duration)
}

type dataRuleSnapshotPort interface {
	LoadDataRuleSnapshot(ctx context.Context) error

	StartDataRuleSnapshotAutoRefresh(ctx context.Context)

	DeptSubtreeIDsFromSnapshot(deptID uint64) []uint64
}

func (a *assembly) buildFoundation(ready func() error) {
	router := a.router
	// 业务组件的视图构建必须在首次编译/组件缓存之前登记。
	pipeline.RegisterBusinessViews()
	// 请求事实由应用装配提供：框架的错误日志只问「这次请求的主体是谁」。
	text.SetRequestUserID(func(c *gin.Context) int64 {
		return int64(builtin.CurrentUserID(c))
	})

	router.HTMLRender = templates.NewJetHTMLRender("internal/templates", gin.Mode() != gin.ReleaseMode)

	// 直接 c.HTML 与共享视图出口使用同一套请求取词与缓冲失败接线。
	// 失败按注册的路由协议处理；模板路径和内部原文只进日志。
	router.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) {
		view.FailScene(c, "template", err)
	}))
	router.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})
	// 统一成功出口：默认“操作成功”，自定义消息只消费明确消息事实。
	router.Use(view.UseSuccess(view.NewSuccessWriter()))

	// 唯一公共 UI 资产源：pkg 公共资产 + 应用专用资产，两层按逻辑键合成。
	// 开发与生产都经同一复合 FS，不再有「release 用 embed、debug 用磁盘」两套来源。
	staticFS, err := templates.EmbeddedStaticFS()
	if err != nil {
		panic("静态资源装配失败: " + err.Error())
	}
	router.Group("/static", builtin.StaticGzipMiddleware(), builtin.StaticCacheMiddleware()).StaticFS("/", staticFS)

	storage := router.Group("/storage",
		func(c *gin.Context) {
			c.Header("X-Content-Type-Options", "nosniff")
			c.Next()
		},
		builtin.StorageCacheMiddleware(),
	)
	storage.StaticFS("/", builtin.NoDirListFS("public/storage"))

	setupStaticFace(router)

	router.GET("/livez", func(c *gin.Context) {
		c.JSON(http.StatusOK, response.Response{
			Code:    http.StatusOK,
			Message: "ok",
			Data:    gin.H{"status": "alive"},
		})
	})

	router.GET("/readyz", func(c *gin.Context) {
		data := gin.H{
			"status":          "ready",
			"pendingReceipts": a.pendingReceipts.snapshot(c.Request.Context()),
		}
		if len(a.permProblems) > 0 {
			data["status"] = "not_ready"
			data["permissionProblems"] = a.permProblems
			c.JSON(http.StatusServiceUnavailable, response.Response{
				Code:    http.StatusServiceUnavailable,
				Message: "权限目录与路由声明不一致",
				Data:    data,
			})
			return
		}
		if ready != nil {
			if err := ready(); err != nil {
				data["status"] = "not_ready"
				c.JSON(http.StatusServiceUnavailable, response.Response{
					Code:    http.StatusServiceUnavailable,
					Message: err.Error(),
					Data:    data,
				})
				return
			}
		}
		c.JSON(http.StatusOK, response.Response{
			Code:    http.StatusOK,
			Message: "ok",
			Data:    data,
		})
	})

	db, err := database.GetDB()
	if err != nil {
		logger.Scene("init").Error(err, "数据库未就绪，业务路由未装配")
		return
	}
	a.db = db

	sysConfigSvc := sysconfigservice.NewService(sysconfigmodel.NewSysConfigModel(db), func() {
		i18n.Invalidate()
		if a.pageService != nil {
			if merr := a.pageService.MarkStaleForI18n(context.Background()); merr != nil {
				logger.Scene("sysconfig").Error(merr, "系统配置保存后标记全站待重建失败")
			}
		}
	})
	i18n.SetValueLoader(sysconfigi18nvalues.New(sysConfigSvc).Load)
	a.marks.mark(portI18nValueLoader)

	a.sysConfigSvc = sysConfigSvc
	a.sysConfigDict = sysConfigSvc

	if !config.RunMigrationsEnabled() {

		if err := casbin.ReloadPolicy(); err != nil {
			logger.Scene("init").With("err", err).Warn("业务权限策略重载失败（Casbin 未初始化时忽略）")
		}
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		logger.Scene("init").Error(err, "业务权限 seed 失败")

		return
	}
	if err := casbin.ReloadPolicy(); err != nil {
		logger.Scene("init").With("err", err).Warn("业务权限策略重载失败（Casbin 未初始化时忽略）")
	}
}

func (a *assembly) tools() *mcp.Registry {
	if a.toolRegistry == nil {
		a.toolRegistry = mcp.NewRegistry()
	}
	return a.toolRegistry
}

func (a *assembly) buildAPIAndCoreCRUD() {
	router := a.router
	marks := a.marks
	db := a.db

	api := router.Group("/api")
	captcharouter.SetupCaptchaRoutes(api)

	adminAuthzSvc := adminhttp.SetupAdminRoutes(mw.NewRouteGroup(api, mw.RouteProtected), db)
	a.adminAuthzSvc = adminAuthzSvc

	// 管理员副作用任务的重放循环：策略重载与会话吊销跨库/跨进程，PG 回滚不了它们，
	// 只能靠持久任务 + 自己的 worker 补做。装配阶段启动一次，挂在进程级可取消 ctx 上。
	if starter, ok := adminAuthzSvc.(adminSideEffectStarter); ok {
		starter.StartSideEffectWorker(a.bgCtx, 0)
		marks.mark(portAdminSideEffectWorker)
	}

	snapPort, ok := adminAuthzSvc.(dataRuleSnapshotPort)
	if !ok {
		panic("admin 模块未实现数据权限快照端口（装配缺陷：部门快照无法注入中间件）")
	}
	snapPort.StartDataRuleSnapshotAutoRefresh(context.Background())
	builtin.SetDataRuleDeptResolver(snapPort.DeptSubtreeIDsFromSnapshot)
	marks.mark(portDataRuleDeptResolver)

	// 后台页面组显式声明 HTML 页面协议：组内页面 GET 失败走 HTML 失败出口。
	a.adminPages = router.Group("/admin",
		view.HTMLPage,
		builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
		adminctx.PermContextMiddleware(adminAuthzSvc))

	a.workbenchPages = router.Group("",
		view.HTMLPage,
		builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
		adminctx.PermContextMiddleware(adminAuthzSvc))

	if v, err := config.GetViper(); err == nil && strings.EqualFold(v.GetString("server.mode"), "debug") {
		router.GET("/admin/dev-login", adminhttp.DevLoginHandler(db))
	}

	authorizedAPI := mw.NewRouteGroup(
		api.Group("", builtin.SessionAuthMiddleware(), builtin.ActorContextMiddleware(),
			builtin.CSRFMiddleware(), builtin.CasbinMiddleware()), mw.RouteProtected)
	a.api = api
	a.authorizedAPI = authorizedAPI

	sysconfighttp.SetupSysConfigRoutes(authorizedAPI, a.adminPages, a.sysConfigSvc)

	aihttp.SetupAIRoutes(authorizedAPI, a.adminPages, db, a.tools(), router, a.sysConfigSvc, newAdminPermDirectory(adminAuthzSvc))

	mediaSvc := mediahttp.SetupMediaRoutes(authorizedAPI, db)
	projectService := projecthttp.SetupProjectRoutes(authorizedAPI, db, a.sysConfigDict)
	blockSvc := blockhttp.SetupBlockRoutes(authorizedAPI, db, projectService)
	artifactSvc := artifacthttp.SetupArtifactRoutes(authorizedAPI, db)
	publicationSvc := pubhttp.SetupPublicationRoutes(authorizedAPI, db)

	buildSvc := buildhttp.SetupBuildRoutes(authorizedAPI, db)

	blueprintSvc := blueprinthttp.SetupBlueprintRoutes(authorizedAPI, db)

	navigationSvc, navigationHandle := navigationhttp.SetupNavigationRoutes(authorizedAPI, db, projectService)

	collectionRegistry := core.NewCollectionRegistry()

	contentSvc := contenthttp.SetupContentRoutes(authorizedAPI, db, collectionRegistry)

	marks.mark(portContentContentStore)
	if provider, ok := contentSvc.(core.CollectionSourceProvider); !ok {
		panic("内容模块未实现集合源契约（CollectionResolver + CollectionSchemaProvider）")
	} else if err := collectionRegistry.Register(provider); err != nil {
		panic("内容集合源注册失败: " + err.Error())
	} else {
		marks.mark(portContentCollectionSource)
	}

	entityRegistry := core.NewEntitySourceRegistry()
	if err := contentSvc.RegisterEntityTypes(entityRegistry); err != nil {
		panic("实体类型注册失败: " + err.Error())
	}

	contentTemplateSvc := contenttemplatehttp.SetupContentTemplateRoutes(authorizedAPI, db, projectService, entityRegistry)

	masterdataSvc := masterdatahttp.SetupMasterDataRoutes(authorizedAPI, a.adminPages, db, projectService)

	inventorySvc := inventoryhttp.SetupInventoryRoutes(authorizedAPI, a.adminPages, db, projectService)

	if concrete, ok := inventorySvc.(*inventoryservice.Service); ok {
		concrete.SetAttachmentResolver(mediaatt.New(a.mediaSvc))
	}

	productSvc, productReader, productHandle := producthttp.SetupProductRoutes(authorizedAPI, db, projectService, masterdataSvc)

	// 主数据变更记录在 SetupProductRoutes 里已经作为构造参数注入
	// （product_router.go:47-49 → productservice.NewService(..., changes)），
	// 不再是 setter 后置注入 —— mark 跟着真实注入方式走，放在构造成功之后。
	marks.mark(portProductMasterDataChanges)

	inventoryhttp.SetProductCatalog(productSvc)
	marks.mark(portInventoryProductCatalog)

	variantStockRefs, ok := productSvc.(productcontract.VariantWarehouseStockPort)
	if !ok {
		panic("商品模块未实现变体货行引用端口（VariantWarehouseStockPort）")
	}
	if setter, ok := inventorySvc.(interface {
		SetVariantWarehouseStocks(productcontract.VariantWarehouseStockPort)
	}); ok {
		setter.SetVariantWarehouseStocks(variantStockRefs)
	} else {
		panic("库存模块未提供变体货行引用注入点（SetVariantWarehouseStocks）")
	}
	marks.mark(portInventoryVariantStockRefs)

	marks.mark(portProductContentStore)

	purchaseLookup, ok := inventorySvc.(inventorycontract.ProductStockLookup)
	if !ok {
		panic("库存模块未实现库存只读端口（ProductStockLookup）")
	}
	purchaseBatch, ok := inventorySvc.(inventorycontract.BatchStockWriter)
	if !ok {
		panic("库存模块未实现带批次的库存变动端口（BatchStockWriter）")
	}
	purchaseStock := purchasestock.New(inventorySvc, purchaseLookup, purchaseBatch)
	purchaseSvc := purchasehttp.SetupPurchaseRoutes(authorizedAPI, a.adminPages, db, projectService, purchaseStock)

	purchaseVariants, ok := productSvc.(productcontract.VariantOptionsReader)
	if !ok {
		panic("purchase.SetVariantCatalog: 商品契约未实现 VariantOptionsReader（两处接口不同步）")
	}
	purchaseBareSKUs, ok := inventorySvc.(inventorycontract.StockSKUReader)
	if !ok {
		panic("purchase.SetVariantCatalog: 库存契约未实现 StockSKUReader（两处接口不同步）")
	}
	purchasehttp.SetVariantCatalog(purchasestock.NewCatalog(purchaseVariants, purchaseBareSKUs))
	marks.mark(portPurchaseVariantCatalog)

	a.mediaSvc = mediaSvc
	a.projectService = projectService
	a.blockSvc = blockSvc
	a.artifactSvc = artifactSvc
	a.publicationSvc = publicationSvc
	a.buildSvc = buildSvc
	a.blueprintSvc = blueprintSvc
	a.navigationSvc = navigationSvc
	a.navigationHandle = navigationHandle
	a.collectionRegistry = collectionRegistry
	a.contentSvc = contentSvc
	a.entityRegistry = entityRegistry
	a.contentTemplateSvc = contentTemplateSvc
	a.masterdataSvc = masterdataSvc
	a.inventorySvc = inventorySvc
	a.purchaseSvc = purchaseSvc
	a.productSvc = productSvc
	a.productReader = productReader
	a.productHandle = productHandle
}

func (a *assembly) buildIdentityAndCommerce() {
	marks := a.marks
	db := a.db
	authorizedAPI := a.authorizedAPI
	router := a.router
	productSvc := a.productSvc
	inventorySvc := a.inventorySvc

	mailSvc := mailhttp.SetupMailRoutes(authorizedAPI, db, a.adminPages)

	marks.mark(portMailCipherSecret)

	mailSender, mailSenderOK := mailSvc.(mailcontract.TransactionalSender)
	if !mailSenderOK {
		panic("邮箱模块未实现 TransactionalSender（事务发送契约），装配缺陷")
	}

	userSvc := userhttp.SetupUserRoutes(router, db, mailSender, "go_wp")

	userAdminSvc, userAdminOK := userSvc.(usercontract.CustomerAdminPort)
	if !userAdminOK {
		panic("用户模块未实现 CustomerAdminPort（后台客户管理契约），装配缺陷")
	}
	userhttp.SetupCustomerAdminRoutes(authorizedAPI, userAdminSvc)

	membershipSvc := membershiphttp.SetupMembershipRoutes(authorizedAPI, a.adminPages, db, a.projectService)
	a.membershipSvc = membershipSvc

	mailhttp.SetupTrackingRoutes(router, mailSvc)

	webhookSvc := webhookhttp.SetupWebhookRoutes(authorizedAPI, db)
	marks.mark(portWebhookCipherSecret)

	webhookDispatcher, webhookOK := webhookSvc.(webhookcontract.Dispatcher)
	if !webhookOK {
		panic("webhook 模块未实现 Dispatcher（事件派发契约），装配缺陷")
	}

	invConcrete, ok := inventorySvc.(*inventoryservice.Service)
	if !ok {
		panic("库存模块装配返回的不是具体 service（无法注入商品用例）")
	}

	orderSvc := orderhttp.SetupOrderRoutes(authorizedAPI, db, productSvc, orderstock.New(invConcrete), userSvc, webhookDispatcher, a.projectService, a.adminPages, orderstock.NewWarehouseSource(a.inventorySvc), a.sysConfigDict)

	if concrete, ok := orderSvc.(*orderservice.Service); ok {

		a.marks.mark(portOrderShipmentModels)

		a.marks.mark(portOrderReturnWarehouseSource)
		if lookup, lok := a.inventorySvc.(inventorycontract.ProductStockLookup); lok {
			concrete.SetWarehouseResolver(orderstock.NewWarehouseResolver(lookup))
			a.marks.mark(portOrderWarehouseResolver)

			if refs, rok := a.productSvc.(productcontract.VariantWarehouseStockPort); rok {
				concrete.SetShipStockProbe(orderstock.NewShipProbe(lookup, refs))
				a.marks.mark(portOrderShipStockProbe)
			}
		}

		concrete.SetStockRowLocator(orderstock.New(invConcrete))
		a.marks.mark(portOrderStockRowLocator)
	}

	if alerts, ok := a.inventorySvc.(inventorycontract.StockAlertReader); ok {
		orderhttp.SetSalesStockAlerts(func(ctx context.Context, projectID string) (int, error) {
			got, err := alerts.StockAlert(ctx, projectID)
			if err != nil || got == nil {
				return 0, err
			}
			return int(got.Alert), nil
		})
	}

	if orderTools, err := ordermcp.Tools(orderSvc); err != nil {
		panic("订单模块工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(orderTools...); err != nil {
		panic("订单工具注册失败：" + err.Error())
	}

	if overviewTools, err := ordermcp.OverviewTools(orderSvc); err != nil {
		panic("订单概览工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(overviewTools...); err != nil {
		panic("订单概览工具注册失败：" + err.Error())
	}

	if projectTools, err := projectmcp.Tools(a.projectService); err != nil {
		panic("站点工程工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(projectTools...); err != nil {
		panic("站点工程工具注册失败：" + err.Error())
	}

	if userAdminOK {
		if customerTools, err := usermcp.QueryTools(userAdminSvc); err != nil {
			panic("客户工具装配失败：" + err.Error())
		} else if err := a.tools().RegisterAll(customerTools...); err != nil {
			panic("客户工具注册失败：" + err.Error())
		}
		customerWriter, ok := userAdminSvc.(usercontract.CustomerWriter)
		if !ok {
			panic("user: 后台服务未实现 CustomerWriter（装配期接线错误）")
		}
		if customerWriteTools, err := usermcp.WriteTools(customerWriter); err != nil {
			panic("客户写工具装配失败：" + err.Error())
		} else if err := a.tools().RegisterAll(customerWriteTools...); err != nil {
			panic("客户写工具注册失败：" + err.Error())
		}
	}

	if stockTools, err := inventorymcp.Tools(inventorySvc); err != nil {
		panic("库存工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(stockTools...); err != nil {
		panic("库存工具注册失败：" + err.Error())
	}

	stockWriter, stockWriteOK := inventorySvc.(inventorycontract.StockWriter)
	if !stockWriteOK {
		panic("库存模块未实现写工具所需的 ChangeStock，装配缺陷")
	}
	if stockWriteTools, err := inventorymcp.WriteTools(stockWriter, inventorySvc,
		aiservice.NewToolIdempotencyStore(aimodel.NewToolIdempotencyModel(db))); err != nil {
		panic("库存写工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(stockWriteTools...); err != nil {
		panic("库存写工具注册失败：" + err.Error())
	}

	reasonWriter, reasonWriteOK := inventorySvc.(inventorycontract.ReasonWriter)
	if !reasonWriteOK {
		panic("库存模块未实现写工具所需的变动原因能力，装配缺陷")
	}
	if reasonTools, err := inventorymcp.ReasonTools(reasonWriter); err != nil {
		panic("变动原因工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(reasonTools...); err != nil {
		panic("变动原因工具注册失败：" + err.Error())
	}
	if memberTools, err := membershipmcp.Tools(membershipSvc); err != nil {
		panic("会员工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(memberTools...); err != nil {
		panic("会员工具注册失败：" + err.Error())
	}

	memberWriter, memberWriteOK := membershipSvc.(membershipcontract.Writer)
	if !memberWriteOK {
		panic("会员模块未实现写工具所需的 Writer，装配缺陷")
	}
	if memberWriteTools, err := membershipmcp.WriteTools(memberWriter); err != nil {
		panic("会员写工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(memberWriteTools...); err != nil {
		panic("会员写工具注册失败：" + err.Error())
	}

	if mailTools, err := mailmcp.QueryTools(mailSvc); err != nil {
		panic("营销工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(mailTools...); err != nil {
		panic("营销工具注册失败：" + err.Error())
	}
	contactWriter, contactWriteOK := mailSvc.(mailcontract.ContactWriter)
	if !contactWriteOK {
		panic("营销模块未实现写工具所需的 ContactWriter，装配缺陷")
	}
	if contactWriteTools, err := mailmcp.ContactWriteTools(contactWriter); err != nil {
		panic("联系人写工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(contactWriteTools...); err != nil {
		panic("联系人写工具注册失败：" + err.Error())
	}

	if setupTools, err := mailmcp.MailSetupTools(mailSvc); err != nil {
		panic("发信配置工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(setupTools...); err != nil {
		panic("发信配置工具注册失败：" + err.Error())
	}

	campaignWriter, campaignWriteOK := mailSvc.(mailcontract.CampaignWriter)
	if !campaignWriteOK {
		panic("营销模块未实现写工具所需的 CampaignWriter，装配缺陷")
	}
	if campaignWriteTools, err := mailmcp.CampaignWriteTools(campaignWriter); err != nil {
		panic("群发活动写工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(campaignWriteTools...); err != nil {
		panic("群发活动写工具注册失败：" + err.Error())
	}

	templateWriter, templateWriteOK := mailSvc.(mailmcp.TemplateWriter)
	if !templateWriteOK {
		panic("营销模块未实现模板写工具所需的方法，装配缺陷")
	}
	if templateTools, err := mailmcp.TemplateTools(templateWriter, mailSvc); err != nil {
		panic("邮件模板工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(templateTools...); err != nil {
		panic("邮件模板工具注册失败：" + err.Error())
	}

	orderWriter, orderWriteOK := orderSvc.(ordercontract.OrderWriter)
	if !orderWriteOK {
		panic("订单模块未实现写工具所需的 CreateAdminOrder，装配缺陷")
	}
	if orderWriteTools, err := ordermcp.WriteTools(orderWriter,
		aiservice.NewToolIdempotencyStore(aimodel.NewToolIdempotencyModel(db))); err != nil {
		panic("订单写工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(orderWriteTools...); err != nil {
		panic("订单写工具注册失败：" + err.Error())
	}

	if orderStatusTools, err := ordermcp.StatusWriteTools(orderSvc); err != nil {
		panic("订单状态工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(orderStatusTools...); err != nil {
		panic("订单状态工具注册失败：" + err.Error())
	}

	orderToolStore := aiservice.NewToolIdempotencyStore(aimodel.NewToolIdempotencyModel(db))
	if couponTools, err := ordermcp.CouponTools(orderSvc, orderSvc, orderToolStore); err != nil {
		panic("优惠券工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(couponTools...); err != nil {
		panic("优惠券工具注册失败：" + err.Error())
	}
	if returnTools, err := ordermcp.ReturnTools(orderSvc, orderSvc, orderToolStore); err != nil {
		panic("退货工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(returnTools...); err != nil {
		panic("退货工具注册失败：" + err.Error())
	}
	if queryTools, err := ordermcp.QueryTools(orderSvc); err != nil {
		panic("订单查询工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(queryTools...); err != nil {
		panic("订单查询工具注册失败：" + err.Error())
	}

	if err := a.tools().RegisterAll(aimcp.UIRenderTools(func(name string) bool {
		_, ok := a.tools().Lookup(name)
		return ok
	})...); err != nil {
		panic("AI 展示工具注册失败：" + err.Error())
	}

	if err := a.tools().RegisterAll(aimcp.GuideTools()...); err != nil {
		panic("AI 手册工具注册失败：" + err.Error())
	}

	contentWriter, writerOK := a.contentSvc.(contentmcp.ContentWriter)
	if !writerOK {
		panic("内容模块未实现写工具所需的三个方法（Create / Update / Delete），装配缺陷")
	}
	contentReader, readerOK := a.contentSvc.(contentmcp.ContentReader)
	if !readerOK {
		panic("内容模块未实现 content_get 所需的 Get，装配缺陷")
	}
	if contentTools, err := contentmcp.Tools(contentWriter, contentReader,
		aiservice.NewToolIdempotencyStore(aimodel.NewToolIdempotencyModel(db))); err != nil {
		panic("内容模块工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(contentTools...); err != nil {
		panic("内容工具注册失败：" + err.Error())
	}

	productWriter, prodWriteOK := a.productSvc.(productmcp.ProductWriter)
	if !prodWriteOK {
		panic("商品模块未实现写工具所需的三个方法（Create / Update / Delete），装配缺陷")
	}
	productReader, prodReadOK := a.productSvc.(productmcp.ProductReader)
	if !prodReadOK {
		panic("商品模块未实现 product_get 所需的 Get，装配缺陷")
	}
	if productTools, err := productmcp.Tools(productReader, productWriter,
		aiservice.NewToolIdempotencyStore(aimodel.NewToolIdempotencyModel(db))); err != nil {
		panic("商品模块工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(productTools...); err != nil {
		panic("商品工具注册失败：" + err.Error())
	}

	variantWriter, variantWriteOK := a.productSvc.(productmcp.VariantWriter)
	if !variantWriteOK {
		panic("商品模块未实现变体写工具所需的三个方法，装配缺陷")
	}
	if variantTools, err := productmcp.VariantWriteTools(variantWriter); err != nil {
		panic("商品变体写工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(variantTools...); err != nil {
		panic("商品变体写工具注册失败：" + err.Error())
	}

	taxonomyWriter, taxonomyWriteOK := a.productSvc.(productmcp.TaxonomyWriter)
	if !taxonomyWriteOK {
		panic("商品模块未实现分类维度写工具所需的九个方法，装配缺陷")
	}
	taxonomyReader, taxonomyReadOK := a.productSvc.(productmcp.TaxonomyReader)
	if !taxonomyReadOK {
		panic("商品模块未实现分类维度清单工具所需的三个方法，装配缺陷")
	}
	if taxonomyTools, err := productmcp.TaxonomyTools(taxonomyReader); err != nil {
		panic("分类维度清单工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(taxonomyTools...); err != nil {
		panic("分类维度清单工具注册失败：" + err.Error())
	}
	if taxonomyWriteTools, err := productmcp.TaxonomyWriteTools(taxonomyWriter); err != nil {
		panic("分类维度写工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(taxonomyWriteTools...); err != nil {
		panic("分类维度写工具注册失败：" + err.Error())
	}

	attributeWriter, attributeWriteOK := a.productSvc.(productmcp.AttributeWriter)
	if !attributeWriteOK {
		panic("商品模块未实现属性写工具所需的四个方法，装配缺陷")
	}
	if attributeTools, err := productmcp.AttributeTools(attributeWriter); err != nil {
		panic("属性工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(attributeTools...); err != nil {
		panic("属性工具注册失败：" + err.Error())
	}

	if navigationTools, err := navigationmcp.Tools(a.navigationSvc, a.navigationSvc); err != nil {
		panic("导航工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(navigationTools...); err != nil {
		panic("导航工具注册失败：" + err.Error())
	}

	mediaWriter, mediaWriteOK := a.mediaSvc.(mediamcp.MediaWriter)
	if !mediaWriteOK {
		panic("媒体模块未实现写工具所需的两个方法（UpdateAttachment / Delete），装配缺陷")
	}
	mediaReader, mediaReadOK := a.mediaSvc.(mediamcp.MediaReader)
	if !mediaReadOK {
		panic("媒体模块未实现 media_find / media_get 所需的 List 与 Detail，装配缺陷")
	}
	if mediaTools, err := mediamcp.Tools(mediaReader, mediaWriter,
		aiservice.NewToolIdempotencyStore(aimodel.NewToolIdempotencyModel(db))); err != nil {
		panic("媒体模块工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(mediaTools...); err != nil {
		panic("媒体工具注册失败：" + err.Error())
	}
	marks.mark(portWebhookDispatcher)

	membershipReaderSetter, membershipReaderOK := orderSvc.(interface {
		SetMembershipReader(membershipcontract.Reader)
	})
	if !membershipReaderOK {
		panic("订单模块未提供会员身份注入点（SetMembershipReader），装配缺陷：会员折扣不会生效")
	}
	membershipReaderSetter.SetMembershipReader(a.membershipSvc)
	marks.mark(portOrderMembershipReader)

	purchaseSource, purchaseOK := orderSvc.(membershipcontract.PurchaseSource)
	if !purchaseOK {
		panic("订单模块未实现消费额批量只读契约（membershipcontract.PurchaseSource），装配缺陷")
	}
	a.membershipSvc.SetPurchaseSource(purchaseSource)
	marks.mark(portMembershipPurchaseSource)

	a.membershipSvc.StartRecalcScheduler()

	commentSvc := commenthttp.SetupCommentRoutes(authorizedAPI, a.adminPages, db, a.projectService, commentEntityTypes())
	a.commentSvc = commentSvc

	if commentTools, err := commentmcp.QueryTools(commentSvc); err != nil {
		panic(err)
	} else if err := a.tools().RegisterAll(commentTools...); err != nil {
		panic(err)
	}

	commentWriter, ok := commentSvc.(commentcontract.ReviewWriter)
	if !ok {
		panic("comment: 服务未实现 ReviewWriter（装配期接线错误）")
	}
	if commentWriteTools, err := commentmcp.WriteTools(commentWriter); err != nil {
		panic(err)
	} else if err := a.tools().RegisterAll(commentWriteTools...); err != nil {
		panic(err)
	}

	a.mailSvc = mailSvc
	a.userSvc = userSvc
	a.userAdminSvc = userAdminSvc
	a.orderSvc = orderSvc
}

func commentEntityTypes() []commentcontract.EntityType {
	return []commentcontract.EntityType{
		{
			Type:  contentcontract.EntityTypeArticle,
			Label: commentcontract.LabelPair{Key: contentArticleLabelKey, Fallback: "文章"},
		},
		{
			Type:  productcontract.EntityTypeProduct,
			Label: commentcontract.LabelPair{Key: productenums.ProductTranslationsEntityTypeProduct, Fallback: "商品"},
		},
	}
}

const contentArticleLabelKey = "admin.article.list.heading"

func (a *assembly) wireProductInventoryPorts() {
	marks := a.marks
	db := a.db
	productSvc := a.productSvc
	inventorySvc := a.inventorySvc

	stockPort, ok := inventorySvc.(inventorycontract.ProductStockPort)
	if !ok {
		panic("库存模块未实现商品库存端口（ProductStockPort）")
	}
	if setter, ok := productSvc.(interface {
		SetInventoryService(inventorycontract.ProductStockPort)
	}); ok {
		setter.SetInventoryService(stockPort)
	} else {
		panic("商品模块未提供库存 service 注入点（SetInventoryService）")
	}
	marks.mark(portProductInventoryService)

	stockReader := inventorymodel.NewModel(db)
	if setter, ok := productSvc.(interface {
		SetInventory(inventorycontract.ProductStockReader)
	}); ok {
		setter.SetInventory(stockReader)
	} else {
		panic("商品模块未提供库存 reader 注入点（SetInventory）")
	}
	marks.mark(portProductInventoryModel)

	variantCostPort, ok := productSvc.(productcontract.VariantCostPort)
	if !ok {
		panic("商品模块未实现成本价写回端口（VariantCostPort）")
	}
	variantCostSetter, ok := a.purchaseSvc.(interface {
		SetVariantCost(productcontract.VariantCostPort)
	})
	if !ok {
		panic("采购模块未提供成本价端口注入点（SetVariantCost）")
	}
	variantCostSetter.SetVariantCost(variantCostPort)
	marks.mark(portPurchaseVariantCost)

	// 商品的审计变更依赖已在构造时给齐（productservice.NewService 的第三参），
	// 这里不再晚接；采购仍用自己的注入点。
	purchaseChanges, ok := a.purchaseSvc.(interface {
		SetMasterDataChanges(masterdatacontract.MasterDataService)
	})
	if !ok {
		panic("采购模块未提供主数据变更记录注入点（SetMasterDataChanges）")
	}
	purchaseChanges.SetMasterDataChanges(a.masterdataSvc)
	marks.mark(portPurchaseMasterDataChanges)

	availabilityPort, ok := inventorySvc.(productcontract.VariantAvailabilityPort)
	if !ok {
		panic("库存模块未实现可用量端口（VariantAvailabilityPort）")
	}
	availabilitySetter, ok := productSvc.(interface {
		SetAvailabilityPort(productcontract.VariantAvailabilityPort)
	})
	if !ok {
		panic("商品模块未提供可用量端口注入点（SetAvailabilityPort）")
	}
	availabilitySetter.SetAvailabilityPort(availabilityPort)
	marks.mark(portProductAvailability)

	availabilityLookup, ok := productSvc.(productcontract.VariantAvailabilityLookupPort)
	if !ok {
		panic("商品模块未实现变体可用量查询端口（VariantAvailabilityLookupPort）")
	}
	a.availabilityLookup = availabilityLookup

}

func resolvePurposeSecret(configKey, purpose, sessionSecret string) string {
	configured := ""
	if v, err := config.GetViper(); err == nil && v != nil {
		configured = strings.TrimSpace(v.GetString(configKey))
	}
	if configured != "" {
		return configured
	}
	logger.Scene("init").Warn("未配置 " + configKey + "：" + purpose + "回退到会话密钥，轮换 auth.session_secret 会同时影响它；建议配置独立密钥（openssl rand -hex 32）")
	return sessionSecret
}

func (a *assembly) wireRuntimeAccessFace() {
	marks := a.marks
	db := a.db
	router := a.router
	authorizedAPI := a.authorizedAPI
	entityRegistry := a.entityRegistry
	collectionRegistry := a.collectionRegistry
	productSvc := a.productSvc
	orderSvc := a.orderSvc
	availabilityLookup := a.availabilityLookup

	sessionSecret := auth.SessionSecret()
	if strings.TrimSpace(sessionSecret) == "" {

		panic("会话密钥未初始化（auth 组件未 Init），购物车 cookie 无法签名")
	}

	cartCookieSecret := resolvePurposeSecret("cart.cookie_secret", "购物车 cookie 签名", sessionSecret)

	paymentCallbackSecret := resolvePurposeSecret("cart.payment_callback_secret", "支付回调验签", sessionSecret)
	cartSvc := cartservice.NewService(orderSvc, productSvc, availabilityLookup, mockpaypal.New(paymentCallbackSecret), cartCookieSecret)

	cartSvc.SetMembershipReader(a.membershipSvc)
	marks.mark(portCartMembershipReader)

	if policyReader, ok := a.projectService.(projectcontract.ShippingPolicyReader); ok {
		cartSvc.SetShippingPolicyReader(policyReader)
		marks.mark(portCartShippingPolicy)
	} else {
		logger.Scene("init").Warn("project 模块未提供站点运费规则读取端口（ShippingPolicyReader）：" +
			"结算运费恒为 0 —— 站点级基础运费与满额免运费都不会生效")
	}

	if checker, ok := orderSvc.(productcontract.PurchaseChecker); ok {
		if setter, sok := productSvc.(interface {
			SetPurchaseChecker(productcontract.PurchaseChecker)
		}); sok {
			setter.SetPurchaseChecker(checker)
			marks.mark(portProductPurchaseChecker)
		}
	} else {
		logger.Scene("init").Warn("order 模块未实现购买事实只读端口（productcontract.PurchaseChecker）：" +
			"「商品评论必须买过」这条规则不生效，任何登录访客都能提交")
	}
	if a.commentSvc != nil {

		if policy, ok := a.productSvc.(commentcontract.EntityPolicy); ok {

			if setter, sok := a.commentSvc.(interface {
				SetEntityPolicy(commentcontract.EntityPolicy)
			}); sok {
				setter.SetEntityPolicy(policy)
				marks.mark(portCommentEntityPolicy)
			}
		} else {
			logger.Scene("init").Warn("product 模块未实现评论差异化规则端口（commentcontract.EntityPolicy）：" +
				"商品评论不做「买过才算」这类校验，所有提交一律进审核队列")
		}
	}

	carthttp.SetupCartRoutes(router, cartSvc)
	carthttp.SetupCartPublicRoutes(router, cartSvc, userhttp.VisitorIdentityMiddleware(a.userSvc), userhttp.VisitorCSRFMiddleware())
	orderhttp.SetupOrderPublicRoutes(router, a.orderSvc, userhttp.VisitorIdentityMiddleware(a.userSvc), userhttp.VisitorCSRFMiddleware())

	analyticsSvc := analyticshttp.SetupAnalyticsRoutes(authorizedAPI, router, db, sessionSecret, a.adminPages, a.projectService)
	a.analyticsSvc = analyticsSvc

	if trafficTools, err := analyticsmcp.TrafficTools(analyticsSvc); err != nil {
		panic("访问统计工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(trafficTools...); err != nil {
		panic("访问统计工具注册失败：" + err.Error())
	}

	if err := productSvc.RegisterEntityTypes(entityRegistry); err != nil {
		panic("商品实体类型注册失败: " + err.Error())
	}

	if provider, ok := productSvc.(core.CollectionSourceProvider); !ok {
		panic("商品模块未实现集合源契约（CollectionResolver + CollectionSchemaProvider）")
	} else if err := collectionRegistry.Register(provider); err != nil {
		panic("商品集合源注册失败: " + err.Error())
	} else {
		marks.mark(portProductCollectionSource)
	}
}
