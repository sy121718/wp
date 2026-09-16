package routers

// assembly.go — 路由装配的承载结构（审计 CQ-008）。
//
// SetupRoutes 曾经是一个约 700 行的巨型函数，混合了组件装配、依赖注入、端口注入、
// 路由分组与中间件挂载。拆分的**唯一原则是保持执行顺序逐行不变**：装配天然是线性的
// （后一个模块常依赖前一个模块的契约，端口注入必须发生在两侧都构造完成之后），
// 重排就是引入顺序漂移。因此这里只把同一段线性代码按主题切成函数，
// 用 assembly 承载跨段共享的服务，不做任何「先全构造、再全挂载」的重排。
//
// 原有注释逐字保留 —— 它们是这个文件最有价值的部分，记录了大量装配陷阱的成因。
//
// 重构的安全网是路由清单快照：routes_snapshot_test.go 在重构前后 dump 同一份
// 路由表，要求逐字节一致（504 条）。装配自检另见 wiring.go 的 mustAllPortsWired。

import (
	"context"
	"net/http"
	"strings"

	"go_wp/config"

	"go_wp/internal/builder/core"
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticshttp "go_wp/internal/module/analytics/inbound/http"
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
	captcharouter "go_wp/internal/module/common/captcha/router"
	contentcontract "go_wp/internal/module/content/contract"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	mailcontract "go_wp/internal/module/mail/contract"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatahttp "go_wp/internal/module/masterdata/inbound/http"
	mediacontract "go_wp/internal/module/media/contract"
	mediahttp "go_wp/internal/module/media/inbound/http"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	ordercontract "go_wp/internal/module/order/contract"
	orderhttp "go_wp/internal/module/order/inbound/http"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	producthttp "go_wp/internal/module/product/inbound/http"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventoryhttp "go_wp/internal/module/product/inventory/inbound/http"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	orderstock "go_wp/internal/module/product/inventory/outbound/orderstock"
	inventoryservice "go_wp/internal/module/product/inventory/service"
	projectcontract "go_wp/internal/module/project/contract"
	projecthttp "go_wp/internal/module/project/inbound/http"
	pubcontract "go_wp/internal/module/publication/contract"
	pubhttp "go_wp/internal/module/publication/inbound/http"
	runtimefragment "go_wp/internal/module/runtimefragment"
	usercontract "go_wp/internal/module/user/contract"
	userhttp "go_wp/internal/module/user/inbound/http"
	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookhttp "go_wp/internal/module/webhook/inbound/http"
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/public/migrations"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// syncDeclaredPermissions 把装配期声明的权限点幂等同步进库并重载 Casbin 策略（审计 SEC-011）。
//
// 为什么必须在**路由装配之后**调用：声明表是注册动作的产物（RouteGroup.GET/POST 采集），
// 早于路由注册执行就等于没有声明可同步。
//
// 失败处理与迁移 seed 同口径（记 Error 日志、不阻断启动）：启动失败会让整站不可用，
// 而这里失败的表现是「新接口缺权限点 → 403」，故障面小、且日志里写明了后果与排查方向。
func syncDeclaredPermissions(db *gorm.DB) {
	if db == nil {
		return
	}
	res, err := permission.SyncToDB(context.Background(), db)
	if err != nil {
		logger.Scene("init").Error(err,
			"权限点声明同步失败：新接口可能仍因库中缺权限点（含超管策略）而 403，请检查数据库可写与 sys_permission 表结构")
		return
	}
	// 策略在启动时载入内存，同步完必须重载；少了这一步会表现为「策略已写库、接口仍 403」。
	if err := casbin.ReloadPolicy(); err != nil {
		logger.Scene("init").With("err", err).Warn("权限点声明同步后策略重载失败（Casbin 未初始化时忽略）")
	}
	logger.Scene("init").
		With("declared", res.Declared).With("inserted", res.Inserted).
		With("pathFixed", res.PathFixed).With("superPolicies", res.SuperPolicies).
		Info("权限点声明同步完成：" + res.String())
	// 显式豁免必须可见：豁免是「挂在 Casbin 组下但不要权限点」的自觉选择，
	// 一旦有人把本该要权限的路由写成豁免，启动日志是唯一的痕迹。
	// 双轨期的漂移可见性：库里有、代码没声明的权限点摆出来（不清理，只报告）。
	if len(res.Unmanaged) > 0 {
		logger.Scene("init").With("count", len(res.Unmanaged)).
			Info("库中存在代码未声明的权限点（历史 seed 或页面路由入口，保留不动）：" + strings.Join(res.Unmanaged, "; "))
	}
	if ex := permission.Exempts(); len(ex) > 0 {
		logger.Scene("init").With("count", len(ex)).
			Info("以下路由显式豁免权限点（check-permission-gaps.sh 的 EXEMPT 名单应对应）：" + strings.Join(ex, "; "))
	}
}

// assembly 承载一次装配过程中构造出的全部服务与共享状态。
//
// 存在的理由是**约束拆分后的参数爆炸**：这些服务在线性装配里互相引用
// （订单要商品与库存、发布实例要内容模板与全局块、后台页面几乎要全部契约），
// 若按值传递，每个切段的函数签名都会膨胀到十几个参数。
//
// 字段**只增不减**：切分只搬运代码，不改变谁依赖谁。
type assembly struct {
	router *gin.Engine
	marks  wiringMarks
	// db 在 buildFoundation 里取得；取不到时装配整体跳过（原行为：log + return）。
	db *gorm.DB

	api *gin.RouterGroup
	// authorizedAPI 是**声明式权限路由组**（审计 SEC-011）：挂在它下面的每条路由
	// 注册时必须给出权限点（permission.Perm），路径由注册动作自身算出，
	// 装配末尾统一幂等 upsert 进 sys_permission 与超管策略。
	authorizedAPI *permission.RouteGroup

	adminAuthzSvc admincontract.AuthzContextService

	mediaSvc       mediacontract.MediaService
	projectService projectcontract.ProjectService
	blockSvc       blockcontract.BlockService
	artifactSvc    artifactcontract.ArtifactService
	publicationSvc pubcontract.PublicationService
	buildSvc       buildcontract.BuildService
	blueprintSvc   blueprintcontract.BlueprintService
	navigationSvc  navigationcontract.NavigationService

	collectionRegistry core.CollectionRegistry
	contentSvc         contentcontract.ContentService
	entityRegistry     core.EntitySourceRegistry
	contentTemplateSvc contenttemplatecontract.ContentTemplateService
	masterdataSvc      masterdatacontract.MasterDataService
	inventorySvc       inventorycontract.InventoryService
	// inventoryConcrete 是库存模块的具体 service（不是契约）：商品用例经
	// SetInventoryService 直接持有它，属同模块内直调而非跨模块端口。
	inventoryConcrete *inventoryservice.Service
	productSvc        productcontract.ProductService

	mailSvc      mailcontract.MailService
	userSvc      usercontract.UserService
	userAdminSvc usercontract.CustomerAdminPort

	orderSvc ordercontract.OrderService

	analyticsSvc analyticscontract.AnalyticsService

	presentationSvc    presentationcontract.PresentationService
	pluginSvc          plugincontract.PluginService
	collectionResolver core.CollectionResolver
	pageService        pagecontract.PageService

	// availabilityLookup 在商品端口注入段取到，购物车建单时要用同一份实现。
	availabilityLookup productcontract.VariantAvailabilityLookupPort
}

// buildFoundation 组件级与进程级基础设施：模板渲染器、静态资源、媒体与站点静态面、
// 健康检查，以及业务装配所需的通用依赖（db）与权限 seed。
//
// 返回后 a.db 为 nil 表示数据库未就绪，调用方必须停止后续装配（原 return 语义）。
func (a *assembly) buildFoundation(ready func() error) {
	router := a.router

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
	// 静态资源来源按模式分流（审计 OSS-018）：开发模式读磁盘（改 CSS/JS 立即生效），
	// 生产模式走 embed —— 二进制自带静态资产，不再要求部署时附带源码树。
	staticFS := gin.Dir("internal/templates/static", false)
	if gin.Mode() == gin.ReleaseMode {
		if embedded, err := templates.EmbeddedStaticFS(); err != nil {
			logger.Scene("init").Error(err, "静态资源 embed 不可用，回退磁盘目录")
		} else {
			staticFS = embedded
		}
	}
	router.Group("/static", builtin.StaticGzipMiddleware(), builtin.StaticCacheMiddleware()).StaticFS("/", staticFS)

	// 媒体上传存储（pkg/upload local provider 默认 public/storage）。
	// 同样禁目录列表（审计 Low：/storage 目录列表开启）。
	// SEC-014：用户上传文件同域直出，加 nosniff 降低 MIME 嗅探执行风险。
	storage := router.Group("/storage", func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	})
	storage.StaticFS("/", gin.Dir("public/storage", false))

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
	a.db = db

	// 业务权限 seed：权限点（sys_permission）、菜单（sys_menus）与默认超管策略（sys_casbin_rule）。
	// 表结构迁移由装配链上的 migrations 组件负责；此处幂等执行 seed（ConditionSQL 已存在则跳过），
	// seed 直写 sys_casbin_rule 后重载 Casbin 内存策略与 urlCodeMap，保证启动时策略即生效。
	if err := migrations.RunSeeds(db); err != nil {
		logger.Scene("init").Error(err, "业务权限 seed 失败")
	} else if err := casbin.ReloadPolicy(); err != nil {
		logger.Scene("init").With("err", err).Warn("业务权限策略重载失败（Casbin 未初始化时忽略）")
	}
}

// buildAPIAndCoreCRUD 建立 /api 根组与三层鉴权链，并装配依赖顺序在前的模块
// （media → project → block → artifact → publication → build → blueprint → navigation →
// content → contenttemplate → masterdata → inventory → product）。
//
// 这一段是装配顺序的**主干**：后面所有端口注入都建立在这里拿到的契约之上。
func (a *assembly) buildAPIAndCoreCRUD() {
	router := a.router
	marks := a.marks
	db := a.db

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
	// admin 模块自建六领域的中间件链（含各自的分组级 CasbinMiddleware），因此这里传的是
	// 带声明能力的包装组而非 authorizedAPI：路径前缀仍是 /api，权限点声明照常生效。
	adminAuthzSvc := adminhttp.SetupAdminRoutes(permission.NewRouteGroup(api), db)
	a.adminAuthzSvc = adminAuthzSvc
	// 开发阶段一键登录（浏览器直接访问 /admin/dev-login?to=/admin/xxx）：
	// **只在 debug 模式下注册** —— release 环境这个路由根本不存在，比运行时判断更可靠。
	// 具体安全约束（只认环回地址、只登超管、走同一套会话路径）见 admin/inbound/http/dev_login.go。
	if v, err := config.GetViper(); err == nil && strings.EqualFold(v.GetString("server.mode"), "debug") {
		router.GET("/admin/dev-login", adminhttp.DevLoginHandler(db))
	}

	// 三层链（SessionAuth + CSRF + Casbin）外面再包一层声明式路由组：注册即声明权限点。
	authorizedAPI := permission.NewRouteGroup(
		api.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), builtin.CasbinMiddleware()))
	a.api = api
	a.authorizedAPI = authorizedAPI

	mediaSvc := mediahttp.SetupMediaRoutes(authorizedAPI, db)
	projectService := projecthttp.SetupProjectRoutes(authorizedAPI, db)
	blockSvc := blockhttp.SetupBlockRoutes(authorizedAPI, db, projectService)
	artifactSvc := artifacthttp.SetupArtifactRoutes(authorizedAPI, db)
	publicationSvc := pubhttp.SetupPublicationRoutes(authorizedAPI, db)
	// 构建任务队列（审计 DB-007）：自装配只注册路由与队列能力，worker 在 page 装配后启动 ——
	// 那时才有执行器，早启动会让这中间进来的任务被判成「没有执行器」而失败。
	buildSvc := buildhttp.SetupBuildRoutes(authorizedAPI, db)
	// Page 初始化工具 Blueprint（0-B，InitPageDocument 未来接 page CreatePage）。
	blueprintSvc := blueprinthttp.SetupBlueprintRoutes(authorizedAPI, db)
	// 公开站点导航（0-C，与后台 menu 严格隔离）。
	navigationSvc := navigationhttp.SetupNavigationRoutes(authorizedAPI, db)
	// 蓝图契约在下面接线给 page（SetBlueprints）：新建页面可从蓝图初始化文档。
	// 集合源注册表（装配期注册，构建期只读，issue #9）：各领域模块注册自己的集合源
	// （内容集合 / 商品集合），集合类组件与集合源元数据接口只认注册表 —— 构建层
	// 不认识具体领域模块，新增领域（库存/分类…）只需在装配期多注册一次。
	collectionRegistry := core.NewCollectionRegistry()
	// CMS 内容（0-A2）。集合源元数据接口经注册表返回全量集合源（含商品等其它领域）。
	contentSvc := contenthttp.SetupContentRoutes(authorizedAPI, db, collectionRegistry)
	// 内容译文存储由 SetupContentRoutes 内部用同一个 db 注入（可选端口：未接入即回退原文）。
	marks.mark(portContentContentStore)
	if provider, ok := contentSvc.(core.CollectionSourceProvider); !ok {
		panic("内容模块未实现集合源契约（CollectionResolver + CollectionSchemaProvider）")
	} else if err := collectionRegistry.Register(provider); err != nil {
		panic("内容集合源注册失败: " + err.Error())
	} else {
		marks.mark(portContentCollectionSource)
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
	// 商品域（issue #5）：商品与变体管理。商品是独立领域模块，不再寄居内容表。
	productSvc := producthttp.SetupProductRoutes(authorizedAPI, db, projectService)
	// 商品译文存储由 SetupProductRoutes 内部用同一个 db 注入（可选端口：未接入即回退原文）。
	marks.mark(portProductContentStore)

	a.mediaSvc = mediaSvc
	a.projectService = projectService
	a.blockSvc = blockSvc
	a.artifactSvc = artifactSvc
	a.publicationSvc = publicationSvc
	a.buildSvc = buildSvc
	a.blueprintSvc = blueprintSvc
	a.navigationSvc = navigationSvc
	a.collectionRegistry = collectionRegistry
	a.contentSvc = contentSvc
	a.entityRegistry = entityRegistry
	a.contentTemplateSvc = contentTemplateSvc
	a.masterdataSvc = masterdataSvc
	a.inventorySvc = inventorySvc
	a.productSvc = productSvc
}

// buildIdentityAndCommerce 装配身份侧与交易侧模块：mail → user → webhook → order。
//
// 三处匿名接口断言（TransactionalSender / CustomerAdminPort / Dispatcher）都是
// **刻意收窄**：消费方只该拿到它需要的那几条能力，装配缺陷要在启动时炸掉，
// 而不是等运行时才发现能力拿不到。
func (a *assembly) buildIdentityAndCommerce() {
	marks := a.marks
	db := a.db
	authorizedAPI := a.authorizedAPI
	router := a.router
	productSvc := a.productSvc
	inventorySvc := a.inventorySvc

	// 邮箱模块（issue #37）：加密密钥在 SetupMailRoutes 内从 config.yaml 的 app.secret 注入。
	mailSvc := mailhttp.SetupMailRoutes(authorizedAPI, db)
	// 敏感配置加密密钥由 SetupMailRoutes 内部从 config 读取后注入。
	marks.mark(portMailCipherSecret)
	// 用户模块（issue #36）：访客账号（注册 / 验证 / 登录 / 账号中心）。
	// 依赖 mail 只取 SendTransactional 一条能力（usercontract.MailSender），不是整个 mail 契约；
	// 这里断言取那份**收窄**端口，与下面 CustomerAdminPort 同一手法 ——
	// 装配缺陷（mail 侧改了事务发送形状）要在启动时炸掉，而不是等到有人注册时才发现发不出信。
	mailSender, mailSenderOK := mailSvc.(mailcontract.TransactionalSender)
	if !mailSenderOK {
		panic("邮箱模块未实现 TransactionalSender（事务发送契约），装配缺陷")
	}
	// 路由挂在 public 面（不带 Casbin）：访客账号没有权限点，理由见 userhttp 包注释。
	// userSvc 的消费方：order（访客下单自动开号）与访问面片段端点（访客身份解析中间件）。
	userSvc := userhttp.SetupUserRoutes(router, db, mailSender, "go_wp")
	// 后台客户管理（/api/customer/*，权限点见迁移 152）：同一个 service 的**管理面**。
	// 与访客面共用一份实现，但刻意是两条契约 —— 拿得到 CustomerAdminPort 的地方
	// 才能列出全部客户、停用别人的账号，而片段层拿到的那份接口里没有这些能力。
	// 这里断言而不是裸类型转换：装配缺陷要在启动时炸掉，而不是等运营点开客户页。
	userAdminSvc, userAdminOK := userSvc.(usercontract.CustomerAdminPort)
	if !userAdminOK {
		panic("用户模块未实现 CustomerAdminPort（后台客户管理契约），装配缺陷")
	}
	userhttp.SetupCustomerAdminRoutes(authorizedAPI, userAdminSvc)
	// 营销追踪端点（#38 P1）：公开路由（访问面），无鉴权 —— 能力由 TrackingService 收窄。
	mailhttp.SetupTrackingRoutes(router, mailSvc)
	// webhook 外部集成通道（OSS-006 端点白名单 + SEC-015 SSRF 防护）：
	// 表与 service 在 199 就建好了，却一直没有 contract / inbound / 调用方 ——
	// 与 CQ-019（商品侧注入点「有 setter、无调用方」）同一类的死代码。
	// 本轮补三段接线：后台配置面（这里）、事件派发口（下面注入订单）、
	// worker 注册（SetupWebhookRoutes 内部调 RegisterWebhookTaskHandler）。
	// 密钥在 SetupWebhookRoutes 内从 config.yaml 的 app.secret 注入（与 mail 同一手法）。
	webhookSvc := webhookhttp.SetupWebhookRoutes(authorizedAPI, db)
	marks.mark(portWebhookCipherSecret)
	// 派发口只取 DispatchEvent 一条能力（webhookcontract.Dispatcher），不是端点管理契约 ——
	// 订单不需要也不该有「替管理员改端点配置、看别人投递日志」的能力。
	// 这里断言取那份**收窄**端口：装配缺陷（webhook 侧改了派发形状）要在启动时炸掉，
	// 而不是等订单支付成功后发现通知发不出去。
	webhookDispatcher, webhookOK := webhookSvc.(webhookcontract.Dispatcher)
	if !webhookOK {
		panic("webhook 模块未实现 Dispatcher（事件派发契约），装配缺陷")
	}

	// 订单模块（BIZ-1 销售侧）：依赖两条**收窄过**的端口 —— product 的变体快照（只读，
	// 一个方法）与 inventory 的扣减 / 归还（两个方法），不是各自模块的完整 Service。
	// 建单会读商品事实落快照、并扣减库存，两者缺失都只能在建单那一刻失败，故不设可选依赖。
	// guest 传 userSvc：订单用它为访客下单自动开号（收窄的单方法接口，见 user contract）。
	// orderSvc 的消费方有三个：cart（结算建单 + 支付落账）、dashboard（订单管理页）、
	// runtimefragment（访客订单片段）。契约里同时含访客查询与优惠码两组能力，
	// 各消费方拿到的都是同一个实现 —— 不加壳、不复制。
	//
	// 库存能力**经适配器**注入（orderstock）：订单契约的入参是订单自己的语义类型
	//（出哪几个 SKU、各多少件、什么原因），库存用例吃的是它自己的 dto。
	// 适配层放在库存侧，依赖方向是「实现方依赖调用方契约」，订单模块不认识库存任何包（审计 CQ-004）。
	invConcrete, ok := inventorySvc.(*inventoryservice.Service)
	if !ok {
		panic("库存模块装配返回的不是具体 service（无法注入商品用例）")
	}
	a.inventoryConcrete = invConcrete
	orderSvc := orderhttp.SetupOrderRoutes(authorizedAPI, db, productSvc, orderstock.New(invConcrete), userSvc, webhookDispatcher)
	marks.mark(portWebhookDispatcher)

	a.mailSvc = mailSvc
	a.userSvc = userSvc
	a.userAdminSvc = userAdminSvc
	a.orderSvc = orderSvc
}

// wireProductInventoryPorts 商品 ↔ 库存之间的端口注入（同一模块内直调 + 跨契约端口）。
//
// 本段的每一次注入都是 fail-fast：漏接的后果全是**静默**的（投影恒为 0、
// 守卫恒被跳过、可用量拿不到会把套餐卖爆），只有断言能让它在启动时暴露。
func (a *assembly) wireProductInventoryPorts() {
	marks := a.marks
	db := a.db
	productSvc := a.productSvc
	inventorySvc := a.inventorySvc
	invConcrete := a.inventoryConcrete

	// 库存 model 注入商品用例（issue #32）：商品与库存合并为同一模块后，商品查询直接读
	// 库存真源做**查询期投影**（不再有商品侧缓存列、同步台账与对账）。同模块内直调 model。
	// 商品与库存同属一个模块（issue #32）：库存用例直接交给商品用例，
	// 归属仓解析 / 库存记录生成 / 库存展示值投影都走它，不再经跨模块端口。
	if setter, ok := productSvc.(interface {
		SetInventoryService(*inventoryservice.Service)
	}); ok {
		setter.SetInventoryService(invConcrete)
	} else {
		panic("商品模块未提供库存 service 注入点（SetInventoryService）")
	}
	marks.mark(portProductInventoryService)
	// 库存 model 注入（issue #32，审计 CQ-019）：商品侧的**库存投影**（后台库存列）
	// 与「变体仍有非零库存则拒绝删除」守卫都读它。此前只有 setter、没有任何调用方 ——
	// 投影恒为 0、守卫恒被跳过，两者都不报错。这里显式接线：与库存 service 同一个 db。
	if setter, ok := productSvc.(interface {
		SetInventory(*inventorymodel.Model)
	}); ok {
		setter.SetInventory(inventorymodel.NewModel(db))
	} else {
		panic("商品模块未提供库存 model 注入点（SetInventory）")
	}
	marks.mark(portProductInventoryModel)
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
	marks.mark(portInventoryVariantCost)
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
		setter.SetMasterDataChanges(a.masterdataSvc)
	}
	marks.mark(portProductMasterDataChanges)
	marks.mark(portInventoryMasterDataChanges)
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
	marks.mark(portProductAvailability)
	// 捆绑配置器片段（issue #20）：前台配置器走访问面的 /_fragments 端点，
	// 经商品模块的窄契约（BundleConfiguratorPort）读配置与整单校验 ——
	// 访问面不经过后台鉴权链，也不认识商品表。装配期注入，未注入即 fail-closed。
	bundlePort, ok := productSvc.(productcontract.BundleConfiguratorPort)
	if !ok {
		panic("商品模块未实现捆绑配置器端口（BundleConfiguratorPort）")
	}
	runtimefragment.SetBundleProvider(bundlePort)
	marks.mark(portRuntimeFragBundle)
	// 商品变体可用量片段（issue #24）：商品详情规格选择器旁的「实时库存」走访问面片段端点。
	// 同一份注入模式：product 模块实现 VariantAvailabilityLookupPort（内部再调 inventory 的
	// VariantAvailabilityPort 读真源），片段层只管渲染结论。与库存端口一样 fail-fast ——
	// 漏接的表现是「页面上永远显示以结算时库存为准」，比启动时报错隐蔽得多。
	availabilityLookup, ok := productSvc.(productcontract.VariantAvailabilityLookupPort)
	if !ok {
		panic("商品模块未实现变体可用量查询端口（VariantAvailabilityLookupPort）")
	}
	runtimefragment.SetVariantAvailabilityProvider(availabilityLookup)
	a.availabilityLookup = availabilityLookup
	marks.mark(portRuntimeFragVariantAvailability)
	// 商品实时价格核对片段（BIZ-2）：定价工具改价只落库、不进构建管线，所以产物里的价
	// 与库里的当前价在时间窗内可能不一致；片段读**当前事实**并在不一致时给访客一句交代。
	// 端口直接复用订单域的 VariantSnapshotPort（按变体 id 读当前价 / 启用态，收窄只读），
	// 不为「读个价」再造一条几乎相同的端口。断言 + 注入，与上面同模式。
	variantSnapshots, ok := productSvc.(productcontract.VariantSnapshotPort)
	if !ok {
		panic("商品模块未实现变体快照端口（VariantSnapshotPort）")
	}
	runtimefragment.SetVariantSnapshotProvider(variantSnapshots)
	marks.mark(portRuntimeFragVariantSnapshot)
}

// wireRuntimeAccessFace 访问面运行时能力：购物车与结算、支付回调、访问统计打点，
// 以及商品实体类型与集合源注册。
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

	// 购物车与访客结算（BIZ-1 访问面）：
	//   · 购物车状态在**客户端签名 cookie** 里（访客未登录也要能加购），服务端不持久化；
	//   · 结算走订单域建单（落快照 + 扣库存 + 幂等）→ 支付通道扣款 → 订单落账；
	//   · 支付通道现在是**模拟 PayPal**（orders.payment_method = paypal，
	//     流水号由订单号派生，因此天然幂等）。接真通道时只换这一行的实现，
	//     购物车、订单与片段层的代码都不动 —— 通道的接口定义在 cart 模块的契约里。
	secret := auth.SessionSecret()
	if strings.TrimSpace(secret) == "" {
		// 没有签名密钥的购物车 cookie 等于没有签名：任何人都能伪造一辆车。
		// 这是装配缺陷（auth 组件必须在本函数之前 Init），fail-fast 而不是降级。
		panic("会话密钥未初始化（auth 组件未 Init），购物车 cookie 无法签名")
	}
	// 支付通道用会话密钥做回调验签的共享密钥：模拟通道的签名是
	// HMAC-SHA256(secret, 原始报文)，换成真通道时只改这一行。
	cartSvc := cartservice.NewService(orderSvc, productSvc, availabilityLookup, mockpaypal.New(secret), secret)
	runtimefragment.SetCartProvider(cartSvc)
	marks.mark(portRuntimeFragCart)
	// 支付回调（BIZ-1）：公开路由，靠签名验签 —— 通道不可能持有后台会话与 CSRF token，
	// 所以它不进 /api 的三层链，也不走片段端点（片段有参数与上下文两条协议约束，
	// 而回调带的是原始报文）。
	carthttp.SetupCartRoutes(router, cartSvc)
	// 访问统计（BIZ-8）：打点端点挂**公开路由** —— 访客浏览器直连，没有后台会话也没有
	// CSRF token（与 mail 追踪、支付回调同一位置与同一理由）。越权防护靠接口形状：
	// 公开路由拿到的那份契约只有「写一条浏览记录」，查询与删除能力传不出去。
	// 后台只读聚合（/api/analytics/summary）走 authorizedAPI 三层链，权限点 analytics:view。
	// pepper 用会话密钥：IP 与访客标识只以带盐哈希落库 —— 裸哈希在 IPv4 空间（2^32）里
	// 等于把明文换个写法存下来。
	analyticsSvc := analyticshttp.SetupAnalyticsRoutes(authorizedAPI, router, db, secret)
	a.analyticsSvc = analyticsSvc
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
	} else {
		marks.mark(portProductCollectionSource)
	}
}
