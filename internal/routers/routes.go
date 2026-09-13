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
	"strings"

	"go_wp/internal/middleware/builtin"

	"go_wp/config"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	admincontract "go_wp/internal/module/admin/contract"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	analyticshttp "go_wp/internal/module/analytics/inbound/http"
	artifacthttp "go_wp/internal/module/artifact/inbound/http"
	blockhttp "go_wp/internal/module/block/inbound/http"
	blueprinthttp "go_wp/internal/module/blueprint/inbound/http"
	carthttp "go_wp/internal/module/cart/inbound/http"
	mockpaypal "go_wp/internal/module/cart/outbound/mockpaypal"
	cartservice "go_wp/internal/module/cart/service"
	captcharouter "go_wp/internal/module/common/captcha/router"
	contentcontract "go_wp/internal/module/content/contract"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatahttp "go_wp/internal/module/masterdata/inbound/http"
	mediahttp "go_wp/internal/module/media/inbound/http"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navsource "go_wp/internal/module/navigation/outbound/source"
	orderhttp "go_wp/internal/module/order/inbound/http"
	pagehttp "go_wp/internal/module/page/inbound/http"
	pluginhttp "go_wp/internal/module/plugin/inbound/http"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationhttp "go_wp/internal/module/presentation/inbound/http"
	productcontract "go_wp/internal/module/product/contract"
	producthttp "go_wp/internal/module/product/inbound/http"
	inventoryhttp "go_wp/internal/module/product/inventory/inbound/http"
	inventoryservice "go_wp/internal/module/product/inventory/service"
	projecthttp "go_wp/internal/module/project/inbound/http"
	pubhttp "go_wp/internal/module/publication/inbound/http"
	runtimefragment "go_wp/internal/module/runtimefragment"
	usercontract "go_wp/internal/module/user/contract"
	userhttp "go_wp/internal/module/user/inbound/http"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
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
	// 开发阶段一键登录（浏览器直接访问 /admin/dev-login?to=/admin/xxx）：
	// **只在 debug 模式下注册** —— release 环境这个路由根本不存在，比运行时判断更可靠。
	// 具体安全约束（只认环回地址、只登超管、走同一套会话路径）见 admin/inbound/http/dev_login.go。
	if v, err := config.GetViper(); err == nil && strings.EqualFold(v.GetString("server.mode"), "debug") {
		router.GET("/admin/dev-login", adminhttp.DevLoginHandler(db))
	}

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
	// 商品域（issue #5）：商品与变体管理。商品是独立领域模块，不再寄居内容表。
	productSvc := producthttp.SetupProductRoutes(authorizedAPI, db, projectService)
	// 邮箱模块（issue #37）：加密密钥在 SetupMailRoutes 内从 config.yaml 的 app.secret 注入。
	mailSvc := mailhttp.SetupMailRoutes(authorizedAPI, db)
	// 用户模块（issue #36）：访客账号（注册 / 验证 / 登录 / 账号中心）。
	// 依赖 mail 只取 SendTemplate 一条能力（usercontract.MailSender），不是整个 mail 契约。
	// 路由挂在 public 面（不带 Casbin）：访客账号没有权限点，理由见 userhttp 包注释。
	// userSvc 的消费方：order（访客下单自动开号）与访问面片段端点（访客身份解析中间件）。
	userSvc := userhttp.SetupUserRoutes(router, db, mailSvc, "go_wp")
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
	_ = mailSvc
	// 订单模块（BIZ-1 销售侧）：依赖两条**收窄过**的端口 —— product 的变体快照（只读，
	// 一个方法）与 inventory 的扣减 / 归还（两个方法），不是各自模块的完整 Service。
	// 建单会读商品事实落快照、并扣减库存，两者缺失都只能在建单那一刻失败，故不设可选依赖。
	// guest 传 userSvc：订单用它为访客下单自动开号（收窄的单方法接口，见 user contract）。
	// orderSvc 的消费方有三个：cart（结算建单 + 支付落账）、dashboard（订单管理页）、
	// runtimefragment（访客订单片段）。契约里同时含访客查询与优惠码两组能力，
	// 各消费方拿到的都是同一个实现 —— 不加壳、不复制。
	orderSvc := orderhttp.SetupOrderRoutes(authorizedAPI, db, productSvc, inventorySvc, userSvc)
	// 库存 model 注入商品用例（issue #32）：商品与库存合并为同一模块后，商品查询直接读
	// 库存真源做**查询期投影**（不再有商品侧缓存列、同步台账与对账）。同模块内直调 model。
	// 商品与库存同属一个模块（issue #32）：库存用例直接交给商品用例，
	// 归属仓解析 / 库存记录生成 / 库存展示值投影都走它，不再经跨模块端口。
	invConcrete, ok := inventorySvc.(*inventoryservice.Service)
	if !ok {
		panic("库存模块装配返回的不是具体 service（无法注入商品用例）")
	}
	if setter, ok := productSvc.(interface {
		SetInventoryService(*inventoryservice.Service)
	}); ok {
		setter.SetInventoryService(invConcrete)
	} else {
		panic("商品模块未提供库存 service 注入点（SetInventoryService）")
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
	// 商品实时价格核对片段（BIZ-2）：定价工具改价只落库、不进构建管线，所以产物里的价
	// 与库里的当前价在时间窗内可能不一致；片段读**当前事实**并在不一致时给访客一句交代。
	// 端口直接复用订单域的 VariantSnapshotPort（按变体 id 读当前价 / 启用态，收窄只读），
	// 不为「读个价」再造一条几乎相同的端口。断言 + 注入，与上面同模式。
	variantSnapshots, ok := productSvc.(productcontract.VariantSnapshotPort)
	if !ok {
		panic("商品模块未实现变体快照端口（VariantSnapshotPort）")
	}
	runtimefragment.SetVariantSnapshotProvider(variantSnapshots)

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
	presentationSvc := presentationhttp.SetupPresentationRoutes(authorizedAPI, db, contentTemplateSvc, entityRegistry, projectService, blockSvc, collectionRegistry, publicationSvc)

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
	// 商品构建期数据源（issue #35）：组件直连受限接口，不再只靠按名路由。
	// ProductService 嵌入了 ProductDataSource，装配处拿到的契约天然能传。
	runtimefragment.SetProductDataSource(productSvc)
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
	productSearch, ok := productSvc.(productcontract.SearchPort)
	if !ok {
		panic("商品模块未实现检索端口（SearchPort）")
	}
	runtimefragment.SetProductSearchProvider(productSearch)
	publishedLocator, ok := presentationSvc.(presentationcontract.PublishedEntityLocator)
	if !ok {
		panic("自动发布模块未实现已上线路径解析端口（PublishedEntityLocator）")
	}
	runtimefragment.SetPublishedEntityLocator(publishedLocator)
	// navigationSvc 注入 page 装配：core.nav 绑定菜单位置时构建期解析菜单项。
	pageService := pagehttp.SetupPageRoutes(authorizedAPI, db, artifactSvc, publicationSvc, projectService, blockSvc, pluginSvc, collectionResolver, navigationSvc, mediaSvc)
	// 系统页面槽位解析器接给片段层（BIZ-1）：购物车片段的「去结算」、结算结果的
	// 「查看订单」都要按槽位取路径。传的是 pageService —— 它嵌入了只读的
	// SitePageResolver，发布 / 删除 / 改 URL 那部分能力传不进片段层。
	runtimefragment.SetSitePageResolver(pageService)
	// 访客订单片段（BIZ-1）：片段端点按访客会话取自己的订单。传的是 orderSvc ——
	// 它嵌入了只读的 VisitorOrderReader，写路径（建单 / 状态流转 / 优惠码管理）
	// 那部分能力传不进片段层。归属校验在 order 模块的 SQL 条件里，不在这层。
	runtimefragment.SetVisitorOrderReader(orderSvc)
	// 访客身份解析中间件：片段端点需要知道「这个请求是谁」。
	// 它与后台的 SessionAuthMiddleware 是两套身份（不同 cookie、不同存储），
	// 挂在片段组上只做「尽力解析」，未登录不阻断 —— 必须登录的能力自己渲染引导文案。
	runtimefragment.SetVisitorIdentityMiddleware(userhttp.VisitorIdentityMiddleware(userSvc))
	// 账号中心片段（资料 / 偏好 / 改密码 / 登录设备）：只注入**收窄后的**只读端口 ——
	// 片段层拿不到注册、改密码、踢出设备这些写能力，越权防护靠接口形状。
	runtimefragment.SetVisitorAccountPort(userSvc)
	// 页面 / 自动发布两条构建路径同样接上（issue #35）：装配处拿到的 ProductService
	// 嵌入了 ProductDataSource，直接传即可（受限接口，写方法传不出去）。
	if setter, ok := pageService.(interface {
		SetProductDataSource(productcontract.ProductDataSource)
	}); ok {
		setter.SetProductDataSource(productSvc)
	} else {
		panic("页面模块未提供商品数据源注入点（SetProductDataSource）")
	}
	if setter, ok := presentationSvc.(interface {
		SetProductDataSource(productcontract.ProductDataSource)
	}); ok {
		setter.SetProductDataSource(productSvc)
	} else {
		panic("发布实例模块未提供商品数据源注入点（SetProductDataSource）")
	}

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
		adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminAuthzSvc, navigationSvc, productSvc, presentationSvc, contentTemplateSvc,
		// 文章管理页（INF-1）：content 契约在注入片段端口时已拿到，这里复用同一个实例。
		contentSvc,
		inventorySvc, masterdataSvc, mailSvc,
		// 订单管理页（BIZ-1）：orderSvc 是在前面装配订单模块时拿到的契约
		//（它同时提供访客查询与优惠码能力，后台页只用查询与状态流转那几条）。
		orderSvc,
		// 访问统计页（BIZ-8）：只读聚合（按天 / 按路径 + 时间范围筛选 + 分页）。
		analyticsSvc,
		// 客户管理页：用户模块的后台面（收窄到四条方法，见 usercontract.CustomerAdminPort）。
		// 它同时也是「访客面 /user/*」那套 service 的同一个实例 —— 两个面共用实现，
		// 但页面拿到的接口里只有「读客户 + 停用启用 + 解除锁定」。
		userAdminSvc)

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
	// SiteRedirectMiddleware 在前：改 URL 后的旧路径是「指向 redirect.json 的激活链接」，
	// http.FileServer 只读文件、不认识它 —— 少了这一层，勾了「保留旧链接」的旧路径
	// 表现是 404（承诺未兑现）。重定向判定不查库，访问面零查库不变量不变。
	router.Group("/site", builtin.SiteRedirectMiddleware(), builtin.StaticGzipMiddleware()).
		StaticFS("/", gin.Dir(pipeline.ActiveRoot(), false))
}
