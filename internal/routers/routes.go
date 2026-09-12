// routes.go — 主路由聚合（页面路由 + 各模块自装配路由）。
//
// 权限策略的两个坑（排查 403 时先看这里）：
//  1. Casbin 策略在启动时从 sys_casbin_rule 载入内存，**改库后不会自动重载** ——
//     新接口的 seed 迁移必须配合进程重启才生效，否则表现为「策略已写、接口仍 403」；
//  2. 策略按 v0 = user_id 授权（超管是 user_id=1），**is_admin=1 不自动放行** ——
//     新建的管理员账号需要在 seed/后台里单独授权，否则登录后各接口一律 403。
package routers

import (
	"context"
	"net/http"

	"go_wp/internal/middleware/builtin"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	admincontract "go_wp/internal/module/admin/contract"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	artifacthttp "go_wp/internal/module/artifact/inbound/http"
	blockhttp "go_wp/internal/module/block/inbound/http"
	blueprinthttp "go_wp/internal/module/blueprint/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatahttp "go_wp/internal/module/masterdata/inbound/http"
	mediahttp "go_wp/internal/module/media/inbound/http"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navsource "go_wp/internal/module/navigation/outbound/source"
	pagehttp "go_wp/internal/module/page/inbound/http"
	pluginhttp "go_wp/internal/module/plugin/inbound/http"
	presentationhttp "go_wp/internal/module/presentation/inbound/http"
	productcontract "go_wp/internal/module/product/contract"
	producthttp "go_wp/internal/module/product/inbound/http"
	inventoryhttp "go_wp/internal/module/product/inventory/inbound/http"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	projecthttp "go_wp/internal/module/project/inbound/http"
	pubhttp "go_wp/internal/module/publication/inbound/http"
	runtimefragment "go_wp/internal/module/runtimefragment"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/public/migrations"

	"github.com/gin-gonic/gin"
)

// SetupRoutes 注册全部路由。
//
// 装配说明：管理面六领域（管理员/角色/权限/菜单/部门/数据权限）合并为 admin 大模块，
// 模块内部同包直调、自包含装配；media、dashboard、captcha 为独立模块。
func SetupRoutes(router *gin.Engine, ready func() error) {
	if router == nil {
		return
	}

	// Jet 模板渲染器（根目录 internal/templates）。
	// 开发模式由 Gin 运行模式驱动：release 关缓存（AGENTS.md 约定「生产模式必须关闭」），
	// debug/test 禁用缓存即时生效。硬编码 true 会让模板解析错误潜伏到运行时才暴露。
	router.HTMLRender = templates.NewJetHTMLRender("internal/templates", gin.Mode() != gin.ReleaseMode)

	// 静态文件服务（admin CSS + builder JS/CSS 统一在此）。
	// gin.Dir(listDirectory=false) 禁目录列表：无 index 文件时返回空列表而非
	// 泄漏目录清单（审计 Low：/static 目录列表开启）。
	// StaticGzipMiddleware：文本类资源（js/css/svg）gzip 传输压缩。
	// StaticCacheMiddleware：静态资源统一协商缓存（no-cache + Last-Modified），
	// 避免 ES modules 子模块因启发式缓存执行旧代码（docs/09 §3 拆分后修复）。
	router.Group("/static", builtin.StaticGzipMiddleware(), builtin.StaticCacheMiddleware()).StaticFS("/", gin.Dir("internal/templates/static", false))

	// 媒体上传存储（pkg/upload local provider 默认 public/storage）。
	// 同样禁目录列表（审计 Low：/storage 目录列表开启）。
	router.StaticFS("/storage", gin.Dir("public/storage", false))

	// 静态访问面：已发布站点直出激活产物（只读文件系统，零查库零模板）。
	// ActiveRoot 位于产物根下两级（{root}/public/active），符号链接目标相对可达。
	setupStaticFace(router)

	// 健康检查
	router.GET("/livez", func(c *gin.Context) {
		c.JSON(http.StatusOK, response.Response{
			Code:    http.StatusOK,
			Message: "ok",
			Data:    gin.H{"status": "alive"},
		})
	})

	router.GET("/readyz", func(c *gin.Context) {
		if ready != nil {
			if err := ready(); err != nil {
				c.JSON(http.StatusServiceUnavailable, response.Response{
					Code:    http.StatusServiceUnavailable,
					Message: err.Error(),
					Data:    gin.H{"status": "not_ready"},
				})
				return
			}
		}
		c.JSON(http.StatusOK, response.Response{
			Code:    http.StatusOK,
			Message: "ok",
			Data:    gin.H{"status": "ready"},
		})
	})

	// 通用依赖
	db, err := database.GetDB()
	if err != nil {
		logger.Scene("init").Error(err, "数据库未就绪，业务路由未装配")
		return
	}

	// 业务权限 seed：权限点（sys_permission）、菜单（sys_menus）与默认超管策略（sys_casbin_rule）。
	// 表结构迁移由 cmd/main.go runMigrations 负责；此处幂等执行 seed（ConditionSQL 已存在则跳过），
	// seed 直写 sys_casbin_rule 后重载 Casbin 内存策略与 urlCodeMap，保证启动时策略即生效。
	if err := migrations.RunSeeds(db); err != nil {
		logger.Scene("init").Error(err, "业务权限 seed 失败")
	} else if err := casbin.ReloadPolicy(); err != nil {
		logger.Scene("init").With("err", err).Warn("业务权限策略重载失败（Casbin 未初始化时忽略）")
	}

	// 业务 API 路由（依赖顺序：media → project → block → artifact → publication → page）
	//
	// 认证 + 鉴权装配（SessionAuthMiddleware + CSRFMiddleware + CasbinMiddleware 统一收口）：
	//   - 豁免：GET /api/captcha（登录前置依赖）与 admin 模块路由
	//     （admin 内部已对六领域分组挂中间件，POST /api/admin/login 保持匿名可达）
	//   - 其余业务 API 统一挂 builtin.SessionAuthMiddleware() + builtin.CSRFMiddleware()
	//     + builtin.CasbinMiddleware()
	//   - CSRF 校验：POST/PUT/PATCH/DELETE 必须携带 X-CSRF-Token 头或 csrf_token 表单字段，
	//     token 在登录成功时生成并随响应下发（登录页写入 sessionStorage），GET 等安全方法直接放行
	//   - Casbin 鉴权：权限点定义与默认超管策略见 public/migrations/030/031 业务权限 seed；
	//     超管（is_admin=1）由 seed 全量授权，非超管需经角色/用户授权接口分配
	api := router.Group("/api")
	captcharouter.SetupCaptchaRoutes(api)
	// admin 对外权限上下文查询契约（供外部模块/插件消费，见 AuthzContextService）。
	adminAuthzSvc := adminhttp.SetupAdminRoutes(api, db)

	authorizedAPI := api.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), builtin.CasbinMiddleware())
	mediaSvc := mediahttp.SetupMediaRoutes(authorizedAPI, db)
	projectService := projecthttp.SetupProjectRoutes(authorizedAPI, db)
	blockSvc := blockhttp.SetupBlockRoutes(authorizedAPI, db, projectService)
	artifactSvc := artifacthttp.SetupArtifactRoutes(authorizedAPI, db)
	publicationSvc := pubhttp.SetupPublicationRoutes(authorizedAPI, db)
	// Page 初始化工具 Blueprint（0-B，InitPageDocument 未来接 page CreatePage）。
	blueprintSvc := blueprinthttp.SetupBlueprintRoutes(authorizedAPI, db)
	// 公开站点导航（0-C，与后台 menu 严格隔离）。
	navigationSvc := navigationhttp.SetupNavigationRoutes(authorizedAPI, db)
	_ = blueprintSvc // 未来 page CreatePage 消费 InitPageDocument
	// 集合源注册表（装配期注册，构建期只读，issue #9）：各领域模块注册自己的集合源
	// （内容集合 / 商品集合），集合类组件与集合源元数据接口只认注册表 —— 构建层
	// 不认识具体领域模块，新增领域（库存/分类…）只需在装配期多注册一次。
	collectionRegistry := core.NewCollectionRegistry()
	// CMS 内容（0-A2）。集合源元数据接口经注册表返回全量集合源（含商品等其它领域）。
	contentSvc := contenthttp.SetupContentRoutes(authorizedAPI, db, collectionRegistry)
	if provider, ok := contentSvc.(core.CollectionSourceProvider); !ok {
		panic("内容模块未实现集合源契约（CollectionResolver + CollectionSchemaProvider）")
	} else if err := collectionRegistry.Register(provider); err != nil {
		panic("内容集合源注册失败: " + err.Error())
	}
	// 实体类型注册表：各领域模块在装配期注册自己的实体类型；
	// 内容模板 / 发布实例据此校验类型与取字段解析器，不再直接依赖内容模块。
	// 注册失败即装配缺陷（fail-fast，与本仓组件注册同口径）。
	entityRegistry := core.NewEntitySourceRegistry()
	if err := contentSvc.RegisterEntityTypes(entityRegistry); err != nil {
		panic("实体类型注册失败: " + err.Error())
	}
	// 内容结构模板（presentation 依赖 ResolveTemplate；模板行需 project_id 外键）。
	contentTemplateSvc := contenttemplatehttp.SetupContentTemplateRoutes(authorizedAPI, db, projectService, entityRegistry)
	// 主数据变更记录（issue #19）：append-only 的字段级审计（商品 / 变体 / 货源）。
	// 必须早于商品与库存两个模块装配：它们在写关键主数据时经本模块契约留痕
	//（依赖方向 product / inventory → masterdata），装配期把契约注入它们的可变端口。
	// 本模块不认识任何业务表：调用方把「改前 / 改后」字段快照递进来，它只做 diff 与落库。
	masterdataSvc := masterdatahttp.SetupMasterDataRoutes(authorizedAPI, db, projectService)
	// 仓库与库存记录（issue #15）：库存真源（SKU × 仓库）+ 仓库实体（短码 / 名称 / 默认仓）。
	// 必须早于商品模块装配：商品模块的变体库存端口由本模块实现（依赖方向 inventory → product），
	// 装配期把实现当作端口传进去 —— 建变体时解析归属仓（不选则默认仓）、并在归属仓
	// 生成一条初始 0 的库存记录。
	inventorySvc := inventoryhttp.SetupInventoryRoutes(authorizedAPI, db, projectService)
	// 端口断言：本模块契约与 product 契约定义的端口是两套接口，同一实现同时满足两者
	//（与 contentSvc → core.CollectionSourceProvider 同一手法）。装配缺陷即 fail-fast。
	variantStockPort, ok := inventorySvc.(productcontract.VariantStockPort)
	if !ok {
		panic("库存模块未实现变体库存端口（VariantStockPort）")
	}
	// 商品域（issue #5）：商品与变体管理。商品是独立领域模块，不再寄居内容表。
	productSvc := producthttp.SetupProductRoutes(authorizedAPI, db, projectService, variantStockPort)
	// 库存 model 注入商品用例（issue #32）：商品与库存合并为同一模块后，商品查询直接读
	// 库存真源做**查询期投影**（不再有商品侧缓存列、同步台账与对账）。同模块内直调 model。
	if setter, ok := productSvc.(interface{ SetInventory(*inventorymodel.Model) }); ok {
		setter.SetInventory(inventorymodel.NewModel(db))
	} else {
		panic("商品模块未提供库存 model 注入点（SetInventory）")
	}
	// 商品侧库存缓存端口（issue #16）已删除（issue #32）：商品与库存合并为同一模块后，
	// 商品查询直接读库存真源做查询期投影，不再需要缓存副本、同步台账与对账。
	// 成本价写回端口（issue #18）：与 VariantStockCachePort 同向（product 实现、inventory 调用）——
	// 采购收货 / 生产入库登记后把单价写进 product_variants.cost_price。同一手法：断言 + 注入，
	// 任一未实现即 fail-fast（装配缺陷不该拖到运行时才暴露）。
	variantCostPort, ok := productSvc.(productcontract.VariantCostPort)
	if !ok {
		panic("商品模块未实现成本价写回端口（VariantCostPort）")
	}
	variantCostSetter, ok := inventorySvc.(interface {
		SetVariantCost(productcontract.VariantCostPort)
	})
	if !ok {
		panic("库存模块未提供成本价端口注入点（SetVariantCost）")
	}
	variantCostSetter.SetVariantCost(variantCostPort)
	// 主数据变更记录注入（issue #19）：两个模块的端口是同一套方法（SetMasterDataChanges），
	// 同一手法断言 + 注入；任一未实现即 fail-fast（装配缺陷不该拖到运行时才暴露 ——
	// 变更记录漏接的表现是「审计静默缺失」，比报错隐蔽得多）。
	masterDataSetters := []struct {
		name   string
		target any
	}{
		{"商品模块", productSvc}, {"库存模块", inventorySvc},
	}
	for _, item := range masterDataSetters {
		setter, sok := item.target.(interface {
			SetMasterDataChanges(masterdatacontract.MasterDataService)
		})
		if !sok {
			panic(item.name + "未提供主数据变更记录注入点（SetMasterDataChanges）")
		}
		setter.SetMasterDataChanges(masterdataSvc)
	}
	// 库存真源可用量端口（issue #20）：捆绑品的单项上限与整单下限都受可用量约束，
	// 且只读 inventory_stocks 真源 —— 读 product_variants.stock_total 缓存会直接变成超卖。
	// 方向与 VariantStockPort 相同（inventory 实现、product 调用）：断言 + 注入，
	// 任一未实现即 fail-fast（漏接的表现是「校验拿不到可用量」，会把套餐卖爆）。
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
	// 捆绑配置器片段（issue #20）：前台配置器走访问面的 /_fragments 端点，
	// 经商品模块的窄契约（BundleConfiguratorPort）读配置与整单校验 ——
	// 访问面不经过后台鉴权链，也不认识商品表。装配期注入，未注入即 fail-closed。
	bundlePort, ok := productSvc.(productcontract.BundleConfiguratorPort)
	if !ok {
		panic("商品模块未实现捆绑配置器端口（BundleConfiguratorPort）")
	}
	runtimefragment.SetBundleProvider(bundlePort)
	// 商品变体可用量片段（issue #24）：商品详情规格选择器旁的「实时库存」走访问面片段端点。
	// 同一份注入模式：product 模块实现 VariantAvailabilityLookupPort（内部再调 inventory 的
	// VariantAvailabilityPort 读真源），片段层只管渲染结论。与库存端口一样 fail-fast ——
	// 漏接的表现是「页面上永远显示以结算时库存为准」，比启动时报错隐蔽得多。
	availabilityLookup, ok := productSvc.(productcontract.VariantAvailabilityLookupPort)
	if !ok {
		panic("商品模块未实现变体可用量查询端口（VariantAvailabilityLookupPort）")
	}
	runtimefragment.SetVariantAvailabilityProvider(availabilityLookup)
	// 商品实体类型注册（issue #6）：注册后商品可作为内容模板的数据源
	// （类型合法性 + 字段白名单由注册表判定），构建期经注册表取商品字段解析器。
	// 与内容模块同样 fail-fast：注册失败即装配缺陷。
	if err := productSvc.RegisterEntityTypes(entityRegistry); err != nil {
		panic("商品实体类型注册失败: " + err.Error())
	}
	// 商品集合源注册（issue #9）：注册后 "content:product" 出现在集合源元数据里，
	// 现有集合类组件绑定商品字段即可在构建期解析出集合项（字段白名单由商品
	// contract 单一来源给出，白名单外字段在构建期被拒绝）。
	if provider, ok := productSvc.(core.CollectionSourceProvider); !ok {
		panic("商品模块未实现集合源契约（CollectionResolver + CollectionSchemaProvider）")
	} else if err := collectionRegistry.Register(provider); err != nil {
		panic("商品集合源注册失败: " + err.Error())
	}
	// 自动发布实例（内容实体驱动，复用编译/存储/激活管线；实例行需 project_id 外键）。
	// blockSvc 注入用于内容模板内部的全局块引用展开（页眉/页脚等，构建期内联）。
	presentationSvc := presentationhttp.SetupPresentationRoutes(authorizedAPI, db, contentTemplateSvc, entityRegistry, projectService, blockSvc, collectionRegistry)

	// 插件模块（page 构建路径依赖其装配素材，须先于 page 装配）。
	// plugin 是外部插件宿主：注入 admin 权限上下文契约，供插件运行时读取当前用户权限。
	pluginSvc := pluginhttp.SetupPluginRoutes(authorizedAPI, db, adminAuthzSvc)
	// 集合解析注入：注册表即 core.CollectionResolver（按源分发到内容 / 商品解析器，
	// 同时实现 CollectionSchemaProvider 供组件按白名单严格校验）。
	collectionResolver := core.CollectionResolver(collectionRegistry)
	// 商品列表片段（issue #27）：访问面的筛选/排序/分页局部刷新出口需要同一份集合解析器 ——
	// 片段渲染调 builder.RenderNodeHTML 复用构建期组件，取数自然也要走同一个注册表，
	// 否则「点筛选得到的」与「静态产物里的」会是两批数据。
	runtimefragment.SetCollectionResolver(collectionResolver)
	// navigationSvc 注入 page 装配：core.nav 绑定菜单位置时构建期解析菜单项。
	pageService := pagehttp.SetupPageRoutes(authorizedAPI, db, artifactSvc, publicationSvc, projectService, blockSvc, pluginSvc, collectionResolver, navigationSvc, mediaSvc)

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
	fanout.SetRebuilder(pipeline.SourceTypePage, pageService)
	contentSvc.SetDependencyInvalidator(fanout)

	// 导航来源实体解析（page/article/product/category/block → 标题 + URL）：
	// 依赖 page/content/presentation/block 契约，故在它们全部装配完成后注入。
	navigationSvc.SetSourceResolver(navsource.New(pageService, contentSvc, presentationSvc, blockSvc))

	// 页面路由（编辑器外壳依赖 page/block/plugin 契约，置于 API 装配之后）。
	// admin 六领域 CRUD 契约：SetAdminRoutes 返回的 AuthzContextService 动态类型即合并后的
	// *Service（同实现全部六接口），此处匿名接口断言获得管理面 CRUD 能力注入 dashboard，
	// 供 /admin 六领域管理页（管理员/角色/菜单/权限/部门/数据权限）消费；不使用 GET/POST 之外的动词。
	adminCRUD := adminAuthzSvc.(interface {
		admincontract.AdminService
		admincontract.RoleService
		admincontract.PermService
		admincontract.MenuService
		admincontract.DeptService
		admincontract.RuleService
	})
	dashboardhttp.SetupDashboardRoutes(router, pageService, projectService, blockSvc, pluginSvc, collectionResolver,
		adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminAuthzSvc, navigationSvc, productSvc, presentationSvc, contentTemplateSvc, inventorySvc, masterdataSvc)

	// 运行时片段端点（0-D，公开路由：capability 白名单 + 认证策略在 handler 内）。
	runtimefragment.SetupFragmentRoutes(router)

	// 未匹配路由返回 404
	router.NoRoute(func(c *gin.Context) {
		response.NotFound(c, "请求的资源不存在")
	})
}

// setupStaticFace 挂载静态访问面（docs/03-pipeline.md §5）。
//
// ActiveRoot 位于产物根下两级（{root}/public/active，pipeline.ActiveRoot() 单源），
// 符号链接目标相对可达。因此访问面根必须是 active 目录本身；文件由 StaticFS
// 只读直出，/ 落到 index 入口。
// 注意必须无条件挂载：首次发布发生在启动之后，启动时目录必然不存在，
// 若按目录存在与否跳过挂载，静态访问面将永远无法生效（每次请求动态读盘，
// 目录与产物在首次发布后即时生效）。
//
// gin.Dir(listDirectory=false) 底层仍是 http.Dir（符号链接跟随行为不变，
// 不限制 activeRoot 的 symlink 访问面），仅禁用 Readdir 以阻止目录列表
// （审计 Low：/site 目录列表开启）。
// 访问面文本产物（HTML/CSS/JS）经 StaticGzipMiddleware 传输压缩提速。
func setupStaticFace(router *gin.Engine) {
	router.Group("/site", builtin.StaticGzipMiddleware()).StaticFS("/", gin.Dir(pipeline.ActiveRoot(), false))
}
