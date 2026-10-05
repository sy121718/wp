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
	"go_wp/internal/mcp"
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminhttp "go_wp/internal/module/admin/inbound/http"
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
	commentservice "go_wp/internal/module/comment/service"
	captcharouter "go_wp/internal/module/common/captcha/router"
	contentcontract "go_wp/internal/module/content/contract"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contentmcp "go_wp/internal/module/content/inbound/mcp"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	inventorymodel "go_wp/internal/module/inventory/model"
	orderstock "go_wp/internal/module/inventory/outbound/orderstock"
	inventoryservice "go_wp/internal/module/inventory/service"
	mailcontract "go_wp/internal/module/mail/contract"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatahttp "go_wp/internal/module/masterdata/inbound/http"
	mediacontract "go_wp/internal/module/media/contract"
	mediahttp "go_wp/internal/module/media/inbound/http"
	mediamcp "go_wp/internal/module/media/inbound/mcp"
	membershipcontract "go_wp/internal/module/membership/contract"
	membershiphttp "go_wp/internal/module/membership/inbound/http"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	ordercontract "go_wp/internal/module/order/contract"
	orderhttp "go_wp/internal/module/order/inbound/http"
	ordermcp "go_wp/internal/module/order/inbound/mcp"
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
	runtimefragment "go_wp/internal/module/runtimefragment"
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
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"
	"go_wp/pkg/auth"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
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
	// pendingReceipts 收敛积压的只读观测源（page / presentation 各自一条）：发布模块
	// 装配完成后注册，/readyz 据此输出 pendingReceipts 字段（形状见 readyz_receipts.go）。
	// **只是可观测性**：无论积压多少都不参与就绪判定。
	pendingReceipts pendingReceiptRegistry

	api *gin.RouterGroup
	// authorizedAPI 是**声明式权限路由组**（审计 SEC-011）：挂在它下面的每条路由
	// 注册时必须给出权限点（permission.Perm），路径由注册动作自身算出，
	// 装配末尾统一幂等 upsert 进 sys_permission 与超管策略。
	authorizedAPI *permission.RouteGroup
	// sysConfigSvc 系统配置契约（buildFoundation 里创建，系统设置页复用同一实例）。
	sysConfigSvc sysconfigcontract.Service
	// sysConfigDict 同一实例的**字典只读口**（sys_config 的宽接口不含它，见 contract 的
	// DictReader）：站点设置页的语言码 datalist 与 i18n 页的语言下拉要用它。
	sysConfigDict sysconfigcontract.DictReader

	// adminPages 是后台页面路由组（Session + CSRF + 权限上下文），在 admin 模块装配后
	// 立即创建 —— admin 是全部模块里最早装配的，所以页面组能在任何模块注册后台页面
	// 之前就绪。各模块在自己的 Setup 里拿它注册页面，与注册 API 完全同构。
	adminPages *gin.RouterGroup
	// workbenchPages 是编辑器路由组（**根级前缀**：/workbench* 与仪表盘首页 "/"）。
	// 中间件与 adminPages 相同（Session + CSRF + 权限上下文），前缀不同而已。
	workbenchPages *gin.RouterGroup

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
	// toolRegistry 模型可调用工具的**跨模块注册表**：各领域模块在装配期把只读工具注册进去，
	// AI 会话层在运行期读它。用 tools() 惰性取用，不要直接读这个字段（见该方法的注释）。
	toolRegistry       *mcp.Registry
	contentTemplateSvc contenttemplatecontract.ContentTemplateService
	masterdataSvc      masterdatacontract.MasterDataService
	inventorySvc       inventorycontract.InventoryService
	productSvc         productcontract.ProductService

	mailSvc      mailcontract.MailService
	userSvc      usercontract.UserService
	userAdminSvc usercontract.CustomerAdminPort

	orderSvc ordercontract.OrderService

	// membershipSvc 会员等级与权益（BIZ-3）。装配在 userSvc 之后、orderSvc 之前：
	// 它依赖工程契约，而订单侧（折扣）是它的消费方。
	membershipSvc membershipcontract.MembershipService
	// membershipFacing 会员模块的文案出口（FacingTexter，装配期从 membershipSvc 断言取）。
	//
	// 单独存一份是因为它**不在 MembershipService 接口里**：那个接口是模块能力清单，
	// 而文案出口只服务消费方（片段层 / 客户页拿不到 membership 的 enums 白名单）。
	membershipFacing membershipcontract.FacingTexter

	analyticsSvc analyticscontract.AnalyticsService

	presentationSvc    presentationcontract.PresentationService
	pluginSvc          plugincontract.PluginService
	collectionResolver core.CollectionResolver
	pageService        pagecontract.PageService

	// availabilityLookup 在商品端口注入段取到，购物车建单时要用同一份实现。
	availabilityLookup productcontract.VariantAvailabilityLookupPort

	// commentSvc 评论模块契约（BIZ-5）。装配在订单之后：它只依赖工程契约与 adminPages，
	// 但排在交易域之后便于阅读（评论是内容侧的横切能力，独立模块）。
	// 片段层用的是它的**收窄接口**（commentcontract.FragmentPort），见 wireRuntimeAccessFace。
	commentSvc commentcontract.CommentService

	// fragDeps 片段层依赖快照（internal/module/runtimefragment）。
	//
	// 为什么要先攒着、最后一次性提交（见 runSelfCheck）：片段层的依赖散落在多个装配段
	// （商品端口 / 访问面 / 发布端口），它们的提供方要到各自段落才构造完成；攒进一个
	// 结构体后一次提交，既保住「依赖一次到齐」的语义，又不必为凑一次调用而重排装配顺序
	// （装配顺序本身承载依赖关系，重排的风险远大于这里多攒几行）。
	fragDeps runtimefragment.Deps
}

// dataRuleSnapshotPort 装配期消费的数据权限快照端口（admin 实现，只取装配需要的四条）。
//
// 用窄接口而不是 admincontract.AuthzContextService 的扩展：快照的加载与解析是**装配期**
// 与**中间件**的关切，不该进对外权限契约；admin 只要实现了这四条就能被装配，改动被限制在
// 「谁提供快照」这一层。
type dataRuleSnapshotPort interface {
	// LoadDataRuleSnapshot 重建快照（admin 的写路径同步重载与懒加载共用同一个入口；
	// 装配期**不**调用它做预加载，见 buildAPIAndCoreCRUD 里的注释）。
	LoadDataRuleSnapshot(ctx context.Context) error
	// StartDataRuleSnapshotAutoRefresh 启动定时兜底刷新（测试进程不启动）。
	StartDataRuleSnapshotAutoRefresh(ctx context.Context)
	// DeptSubtreeIDsFromSnapshot 部门子树 id 列表（含自身与全部子孙），供中间件填充
	// UserContext 并让引擎把 dept.scope:SELF_AND_CHILDREN 展开成 IN (...)（向下方向）。
	DeptSubtreeIDsFromSnapshot(deptID uint64) []uint64
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
	// builtin.NoDirListFS 禁目录列表：无 index.html 的目录当不存在（404），
	// 不泄漏目录清单（审计 Low：/static 目录列表开启）。它与 gin.Dir(path,false)
	// 行为等价，但对普通文件仍以 *os.File 透出，静态大文件才能走内核零拷贝。
	// StaticGzipMiddleware：文本类资源（js/css/svg）gzip 传输压缩。
	// StaticCacheMiddleware：静态资源统一协商缓存（no-cache + Last-Modified），
	// 避免 ES modules 子模块因启发式缓存执行旧代码（docs/09 §3 拆分后修复）。
	// 静态资源来源按模式分流（审计 OSS-018）：开发模式读磁盘（改 CSS/JS 立即生效），
	// 生产模式走 embed —— 二进制自带静态资产，不再要求部署时附带源码树。
	staticFS := builtin.NoDirListFS("internal/templates/static")
	if gin.Mode() == gin.ReleaseMode {
		if embedded, err := templates.EmbeddedStaticFS(); err != nil {
			logger.Scene("init").Error(err, "静态资源 embed 不可用，回退磁盘目录")
		} else {
			// embed 的字节在内存里，本来就没有 WriteTo —— 零拷贝不适用，无需包装。
			staticFS = embedded
		}
	}
	router.Group("/static", builtin.StaticGzipMiddleware(), builtin.StaticCacheMiddleware()).StaticFS("/", staticFS)

	// 媒体上传存储（pkg/upload local provider 默认 public/storage）。
	// 同样禁目录列表（审计 Low：/storage 目录列表开启）。
	// SEC-014：用户上传文件同域直出，加 nosniff 降低 MIME 嗅探执行风险。
	// StorageCacheMiddleware：按 URL 是否带内容指纹二分缓存语义 —— 带指纹的变体
	// 可 immutable 长缓存，原图 URL 换图后不变只能协商缓存（判据见该中间件注释）。
	storage := router.Group("/storage",
		func(c *gin.Context) {
			c.Header("X-Content-Type-Options", "nosniff")
			c.Next()
		},
		builtin.StorageCacheMiddleware(),
	)
	storage.StaticFS("/", builtin.NoDirListFS("public/storage"))

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

	// /readyz：组件级就绪 + 只读观测字段。
	//
	// pendingReceipts 是本轮接入的观测字段（待收敛回执：条数 / 最老一条年龄 / 最近一次收敛），
	// 形状与注册见 readyz_receipts.go。**硬要求：pending > 0 绝不影响就绪判定** ——
	// 它是「线上与库可能暂时不一致」的信号，收敛例程会自己收掉；拿它当门会让一次发布失败
	// 把整个实例摘出负载均衡。就绪与否只看 ready()。
	router.GET("/readyz", func(c *gin.Context) {
		data := gin.H{
			"status":          "ready",
			"pendingReceipts": a.pendingReceipts.snapshot(c.Request.Context()),
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

	// 通用依赖
	db, err := database.GetDB()
	if err != nil {
		logger.Scene("init").Error(err, "数据库未就绪，业务路由未装配")
		return
	}
	a.db = db

	// 系统配置（sys_config）与 i18n 全局默认值的接线（阶段 3）。
	//
	// 全局默认语言 / 站点语言 URL 方案 / 语言码覆盖的**唯一来源**是 sys_config 的 i18n 组
	// （原先在 config.yaml：改一次要重启，且散落多处）。pkg/i18n 只声明 ValueLoader 形状、
	// 不认识业务包，这里把 sysconfig 的**只读窄口**适配后注入 —— 依赖方向 internal → pkg。
	//
	// 时机：紧跟 db 就绪之后、任何消费它的模块之前 —— 默认语言进产物字节（<html lang>、
	// hreflang 的 x-default、default_plain 下哪条链接不带前缀），值被读进产物就是既有事实。
	// 本步与「是否跳过 seed」无关（表由迁移链建，读不到组时 loader 返回空值 → pkg/i18n
	// 退回代码内常量并记日志，不阻断启动）。
	//
	// onChanged 注入 Invalidate：配置保存成功后立刻重读，否则运维保存完看到的行为仍是旧的
	// （那正是「写进库了但不生效」的老毛病）。
	// onChanged：进程内默认值刷新 + 站点级 stale 标记。
	//
	// 为什么保存配置也要标 stale：i18n 组的三个键**都进产物字节** ——
	// 默认语言进 <html lang> 与 hreflang 的 x-default；站点语言 URL 方案决定
	// default_plain 下哪条链接不带前缀；语言短码覆盖改变站内链接里的短码形态。
	// 只刷新进程内值而不标 stale 的表现是「后台改了默认语言，线上仍是旧的 URL 形态与切换器」
	// —— 而且没有任何报错（与 FIX-21 的站点设置是同一个失效模式）。
	//
	// 走既有的站点级 stale 网（MarkStaleForI18n）：影响面本来就是全站（每一页都带
	// <html lang> 与语言链接），不是「图省事退化成全站标记」。
	// pageService 在本步（buildFoundation）尚未装配，故延迟到回调触发时读取 ——
	// 回调只在后台保存配置时发生，那时装配早已完成。
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
	// 同一个实例挂到装配对象上：系统设置页（在后面的 core CRUD 段落装配）要复用它，
	// 保存后的主动刷新走的正是上面注入的 onChanged。
	a.sysConfigSvc = sysConfigSvc
	a.sysConfigDict = sysConfigSvc

	// 业务权限 seed：权限点（sys_permission）、菜单（sys_menus）与默认超管策略（sys_casbin_rule）。
	// 表结构迁移由装配链上的 migrations 组件负责；此处幂等执行 seed（ConditionSQL 已存在则跳过），
	// seed 直写 sys_casbin_rule 后重载 Casbin 内存策略与 urlCodeMap，保证启动时策略即生效。
	//
	// database.run_migrations=false 时跳过 seed（切到非超级业务角色后，seed 里的写入会被
	// 策略 / 权限挡住，且本来就应该由管理连接的 -migrate-only 完成，见 docs/rls-role-cutover.md）。
	// **注意范围**：这里跳过的只有 seed。装配末尾的 permission.SyncToDB 是权限点幂等 upsert，
	// 不属于 seed，每次启动照跑。
	if !config.RunMigrationsEnabled() {
		// seed 已由管理连接完成，但策略仍要从库里进内存：进程重启后 Casbin 是空的。
		if err := casbin.ReloadPolicy(); err != nil {
			logger.Scene("init").With("err", err).Warn("业务权限策略重载失败（Casbin 未初始化时忽略）")
		}
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		logger.Scene("init").Error(err, "业务权限 seed 失败")
		// seed 失败时不重载：库里可能是半截台账，按已有策略继续跑更可预期（与改造前一致）。
		return
	}
	if err := casbin.ReloadPolicy(); err != nil {
		logger.Scene("init").With("err", err).Warn("业务权限策略重载失败（Casbin 未初始化时忽略）")
	}
}

// buildAPIAndCoreCRUD 建立 /api 根组与三层鉴权链，并装配依赖顺序在前的模块
// （media → project → block → artifact → publication → build → blueprint → navigation →
// content → contenttemplate → masterdata → inventory → product）。
//
// 这一段是装配顺序的**主干**：后面所有端口注入都建立在这里拿到的契约之上。
// tools 工具注册表的惰性取用。
//
// 惰性（而不是在某个 build 阶段显式赋值）是为了**摆脱装配顺序**：AI 模块与领域模块
// 分属两个 build 函数，谁先跑都是实现细节。注册表只在运行期被读（第一次读一定晚于全部装配），
// 所以「第一次取用即建」就足以让两边拿到同一个实例。
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

	// 业务 API 路由（依赖顺序：media → project → block → artifact → publication → page）
	//
	// 认证 + 鉴权装配（SessionAuthMiddleware + CSRFMiddleware + CasbinMiddleware 统一收口）：
	//   - 豁免：GET /api/captcha（登录前置依赖）与 admin 模块路由
	//     （admin 内部已对六领域分组挂中间件，POST /api/admin/login 保持匿名可达）
	//   - 其余业务 API 统一挂 builtin.SessionAuthMiddleware() + builtin.CSRFMiddleware()
	//     + builtin.CasbinMiddleware()
	//   - CSRF 校验：POST/PUT/PATCH/DELETE 必须携带 X-CSRF-Token 头或 csrf_token 表单字段，
	//     token 在登录成功时生成并随响应下发（登录页写入 sessionStorage），GET 等安全方法直接放行
	//   - Casbin 鉴权：权限点由「codes.go 常量 + 路由注册处声明」定义，装配末尾经
	//     permission.SyncToDB 幂等 upsert 进 sys_permission 与超管（is_admin=1）策略；
	//     030/031 是存量权限点台账，非超管需经角色/用户授权接口分配
	api := router.Group("/api")
	captcharouter.SetupCaptchaRoutes(api)
	// admin 对外权限上下文查询契约（供外部模块/插件消费，见 AuthzContextService）。
	// admin 模块自建六领域的中间件链（含各自的分组级 CasbinMiddleware），因此这里传的是
	// 带声明能力的包装组而非 authorizedAPI：路径前缀仍是 /api，权限点声明照常生效。
	adminAuthzSvc := adminhttp.SetupAdminRoutes(permission.NewRouteGroup(api), db)
	a.adminAuthzSvc = adminAuthzSvc

	// 数据权限快照（性能整改）：快照本身是**懒加载**的（第一次命中查询时由 admin 侧加载），
	// 装配期**不做预加载、不 fail-fast** —— 数据库抖动不该变成启动失败，代价只是首次命中
	// 请求多付一次 3 条小查询的加载。这里只接两件事：
	//
	//   1. 定时兜底刷新的启动（快照从未加载时它会自己跳过，理由见 admin 侧注释）；
	//   2. 把**同一份**部门快照的**子树**（向下：本部门及全部子孙）注入 datarule 中间件，
	//      让中间件不必再查库解析 —— 这是引擎侧 dept.scope:SELF_AND_CHILDREN 走 IN (...)
	//      而不是子查询的前提。快照还没加载时解析器返回 nil，引擎自动回退既有子查询，
	//      行为与改造前一致。
	snapPort, ok := adminAuthzSvc.(dataRuleSnapshotPort)
	if !ok {
		panic("admin 模块未实现数据权限快照端口（装配缺陷：部门快照无法注入中间件）")
	}
	snapPort.StartDataRuleSnapshotAutoRefresh(context.Background())
	builtin.SetDataRuleDeptResolver(snapPort.DeptSubtreeIDsFromSnapshot)
	marks.mark(portDataRuleDeptResolver)

	// 后台页面组在这里就绪：各模块在自己的 Setup 里既注册 /api/* 也注册 /admin/*，
	// 中间件链（Session + CSRF + 权限上下文 / 侧栏菜单树）只由这一处定义 ——
	// 模块不必各写一遍，也就不会漏挂某一环。
	a.adminPages = router.Group("/admin",
		builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
		shell.PermContextMiddleware(adminAuthzSvc))
	// 编辑器组：根级前缀（/workbench*、仪表盘首页），中间件与后台页面组一致。
	a.workbenchPages = router.Group("",
		builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
		shell.PermContextMiddleware(adminAuthzSvc))
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
	// 系统设置页（/admin/system）：读写 sys_config 的 i18n / trade 两组全局默认值。
	// 复用上面那个已是 ValueLoader 的实例 —— 保存后的主动刷新（i18n.Invalidate）走的就是它。
	sysconfighttp.SetupSysConfigRoutes(authorizedAPI, a.adminPages, a.sysConfigSvc)

	// AI 模块（配置面 /api/ai/* + 后台页 /admin/ai/providers，会话面 /api/ai/session/* + /admin/ai/sessions）：
	// 装配期把 app.secret 交给服务层当密文密钥（api_key_cipher 只存密文，接口只回「是否已配置」）；
	// 权限点 ai:provider_* / ai:session_* 由装配末尾的 permission.SyncToDB 幂等落库，无需在本迁移里 seed。
	//
	// 工具注册表在这里**先建、后填**：AI 会话层拿到的是同一个指针，各领域模块的工具在它们
	// 自己装配时注册（见下面订单模块那段）—— 顺序无关，是因为注册表只在**运行期**被读
	//（第一次读一定晚于全部装配），而不是因为「恰好 AI 排在最后」。
	// 第五个参数是根路由：外部接入点挂在 /mcp（不是 /api/mcp），自带 PAT 鉴权。
	aihttp.SetupAIRoutes(authorizedAPI, a.adminPages, db, a.tools(), router, a.sysConfigSvc)

	mediaSvc := mediahttp.SetupMediaRoutes(authorizedAPI, db)
	projectService := projecthttp.SetupProjectRoutes(authorizedAPI, db, a.sysConfigDict)
	blockSvc := blockhttp.SetupBlockRoutes(authorizedAPI, db, projectService)
	artifactSvc := artifacthttp.SetupArtifactRoutes(authorizedAPI, db)
	publicationSvc := pubhttp.SetupPublicationRoutes(authorizedAPI, db)
	// 构建任务队列（审计 DB-007）：自装配只注册路由与队列能力，worker 在 page 装配后启动 ——
	// 那时才有执行器，早启动会让这中间进来的任务被判成「没有执行器」而失败。
	buildSvc := buildhttp.SetupBuildRoutes(authorizedAPI, db)
	// Page 初始化工具 Blueprint（0-B，InitPageDocument 未来接 page CreatePage）。
	blueprintSvc := blueprinthttp.SetupBlueprintRoutes(authorizedAPI, db)
	// 公开站点导航（0-C，与后台 menu 严格隔离）。工程契约用于只带 id 的入口逐工程定位
	// 工程归属（DB-009：navigations 带 FORCE 策略，作用域只能落到具体工程）。
	navigationSvc := navigationhttp.SetupNavigationRoutes(authorizedAPI, db, projectService)
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
	masterdataSvc := masterdatahttp.SetupMasterDataRoutes(authorizedAPI, a.adminPages, db, projectService)
	// 仓库与库存记录（issue #15）：库存真源（SKU × 仓库）+ 仓库实体（短码 / 名称 / 默认仓）。
	// 必须早于商品模块装配：商品模块的变体库存端口由本模块实现（依赖方向 inventory → product），
	// 装配期把实现当作端口传进去 —— 建变体时解析归属仓（不选则默认仓）、并在归属仓
	// 生成一条初始 0 的库存记录。
	inventorySvc := inventoryhttp.SetupInventoryRoutes(authorizedAPI, a.adminPages, db, projectService)
	// 商品域（issue #5）：商品与变体管理。商品是独立领域模块，不再寄居内容表。
	productSvc := producthttp.SetupProductRoutes(authorizedAPI, db, projectService)
	// product ↔ inventory 是循环依赖：product 的 Setup 要 inventory 契约，而库存页面
	// 要 product 契约做「商品 → 变体」下拉，任何固定顺序都装配不出来。
	// 所以 products 不进参数表，改为后置注入（装配期写一次、之后只读；
	// 与 runtimefragment.Deps.BundleProvider 同一手法），handler 侧对 nil 降级为下拉为空。
	inventoryhttp.SetProductCatalog(productSvc)
	marks.mark(portInventoryProductCatalog)
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
	mailSvc := mailhttp.SetupMailRoutes(authorizedAPI, db, a.adminPages)
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

	// 会员等级与权益（BIZ-3）：等级按工程定义、归属按 (project, user) 唯一。
	// 装配位置在 userSvc 之后（两者都围绕访客账号，但会员是**独立领域** ——
	// AGENTS.md 的命名约束把 admin / user 定义为两个独立领域，会员等级要被
	// order / cart / runtimefragment 消费，塞进 user 会让片段层拿到完整 UserService）、
	// orderSvc 之前（订单侧是消费方：折扣要读会员身份）。
	//
	// 消费额批量只读端口（membershipcontract.PurchaseSource）由**订单侧实现**，
	// 而订单装配在本段之后 —— 所以这里只能先建服务，端口在订单段就绪后回填
	//（见下面 order 段末尾的 SetPurchaseSource；不改成构造参数依赖：那会把
	// 「order 依赖 membership 的折扣」与「membership 依赖 order 的消费额」变成构造环）。
	membershipSvc := membershiphttp.SetupMembershipRoutes(authorizedAPI, a.adminPages, db, a.projectService)
	// 文案出口不在 MembershipService 接口里（它只服务消费方，不是模块能力的一部分），
	// 装配层断言一次：客户页与片段层都要用它 —— 缺了就只能一律通用提示，
	// 把「这个工程还没配默认等级」这类可行动差异吞掉。
	membershipFacing, membershipFacingOK := membershipSvc.(membershipcontract.FacingTexter)
	if !membershipFacingOK {
		panic("会员模块未实现 FacingTexter（消费方文案出口契约），装配缺陷")
	}
	a.membershipSvc = membershipSvc
	a.membershipFacing = membershipFacing
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
	// orderSvc 的消费方有三个：cart（结算建单 + 支付落账）、order 模块后台页（订单管理）、
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
	// 末位参数 a.sysConfigDict（系统字典只读口）：订单 / 退货详情的地址要把快照里的
	// 国家代码显示成当前语言的名字。传的是同一实例（它在 sysconfig 内部按语言缓存
	// 「码 → 名」），不是另建一个 —— 每个消费方各建一层缓存等于同一份字典查 N 遍。
	orderSvc := orderhttp.SetupOrderRoutes(authorizedAPI, db, productSvc, orderstock.New(invConcrete), userSvc, webhookDispatcher, a.projectService, a.adminPages, orderstock.NewWarehouseSource(a.inventorySvc), a.sysConfigDict)
	// 订单模块的模型可调用工具（只读聚合）注册进上面那个表。
	// 装配顺序在这里不构成约束：注册表是同一个指针，AI 侧只在**运行期**读它。
	// 注册失败即 panic（权限点重复 / 依赖缺失这类装配缺陷，必须在启动时炸掉，
	// 而不是让 AI 静默地少一个工具 —— 那种缺陷的表现是「它就是不查订单」，无从排查）。
	if orderTools, err := ordermcp.Tools(orderSvc); err != nil {
		panic("订单模块工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(orderTools...); err != nil {
		panic("订单工具注册失败：" + err.Error())
	}
	// 概览聚合工具（趋势 / 热销榜 / 状态计数）与上面的区间摘要分开装配：
	// 两者的依赖接口不同，函数分开之后每类工具的依赖都能收窄到自己需要的那几条只读方法。
	if overviewTools, err := ordermcp.OverviewTools(orderSvc); err != nil {
		panic("订单概览工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(overviewTools...); err != nil {
		panic("订单概览工具注册失败：" + err.Error())
	}
	// 站点工程清单（site_projects）：其它模块的工具都要求 projectId，而用户在
	// 概览页这类跨工程的页面上看不到也说不清有哪些工程 —— 实测模型会停在这里
	//（「这两个工具都必须带 projectId，我拿不到当前是哪个工程，不能代填」）。
	// 它是「按工程查数」这条链的入口，所以与上面的工具一起注册。
	if projectTools, err := projectmcp.Tools(a.projectService); err != nil {
		panic("站点工程工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(projectTools...); err != nil {
		panic("站点工程工具注册失败：" + err.Error())
	}
	// 「按模糊线索查客户」的两个只读工具（customer_find / customer_get）。
	//
	// 用户的提问是模糊的（「张三是不是注册过」「谁这周注册的」「有多少人邮箱没验证」），
	// 所以工具收的是线索而不是 id：customer_find 的 keyword 同时匹配邮箱 / 用户名 /
	// 昵称 / 展示名。依赖收窄到 CustomerQueryReader —— 手里没有 SetCustomerStatus，
	// AI 停用不了任何账号。
	if userAdminOK {
		if customerTools, err := usermcp.QueryTools(userAdminSvc); err != nil {
			panic("客户工具装配失败：" + err.Error())
		} else if err := a.tools().RegisterAll(customerTools...); err != nil {
			panic("客户工具注册失败：" + err.Error())
		}
	}
	// 「按线索查订单」的两个只读工具（order_find / order_get）。
	// 与上面两批分开装配的理由同样成立：它服务的是「一个线索指向一单」这类问题
	//（订单 20261005001 到哪了），与聚合（一共多少 / 每天多少 / 谁最好）是两种形状。
	if queryTools, err := ordermcp.QueryTools(orderSvc); err != nil {
		panic("订单查询工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(queryTools...); err != nil {
		panic("订单查询工具注册失败：" + err.Error())
	}
	// AI 模块自己的工具（展示指令）：它不拥有业务表，只把模型的展示意图收敛成受校验的 spec；
	// 真正的取数发生在会话层（那里才有调用者身份，权限逐块判）。
	// 传的是**运行时查询**：ui_render 的积木靠 source 指名数据源，而这个名字必须在
	// 真正调用时才能判断存不存在（装配顺序上其它模块的工具未必已经注册完）。
	if err := a.tools().RegisterAll(aimcp.UIRenderTools(func(name string) bool {
		_, ok := a.tools().Lookup(name)
		return ok
	})...); err != nil {
		panic("AI 展示工具注册失败：" + err.Error())
	}
	// 领域手册（口径与注意事项）。它同样零依赖 —— 读的是编译进二进制的文本，
	// 不注入任何模块端口，所以不会因为某个模块没装配而消失。
	if err := a.tools().RegisterAll(aimcp.GuideTools()...); err != nil {
		panic("AI 手册工具注册失败：" + err.Error())
	}
	// 内容模块的工具：一个读 + 三个写。**同批上**（见 contentmcp.Tools 的注释）——
	// content_update 是整份替换，没有 content_get 的写入口等于让模型凭记忆拼字段集。
	//
	// 写工具的幂等台账落在 ai 模块的表上（迁移 569），但**接口在 mcp 层**
	// （mcp.IdempotencyStore）：工具层不该认识 ai 模块，adapter 在 service 里做转换。
	// 断言成收窄的读写端口而不是直接传整个 ContentService：工具层握着 Publish /
	// RegisterEntityTypes 时，「AI 顺手发一版」会从「显式加一个工具」退化成「随手就能做」。
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
	// 商品模块的工具（商品主体：一个读 + 三个写）。
	//
	// 只覆盖 products 一行 —— 属性 / 品牌 / 分类 / 捆绑配置各自是独立的数据形态，
	// 混进同一批会让「商品」在工具列表里指五样东西（模型分不清时会挑一个最像的调下去）。
	// 断言成收窄的读写端口：ProductService 同时握着 SetBundleConfig / UpdateVariantCost /
	// CreateAttribute，工具层握着它们时「顺手调个价」会从「显式加一个工具」退化成「随手就能做」。
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
	// 媒体模块的工具（搜 + 读 + 改信息 + 删）。
	//
	// 没有「上传」与「换图」：那两条收的是 multipart 文件，模型给不出。
	// 这一点写进了 media_update 的说明 —— 免得模型对着「换张图」的请求硬凑一个调用。
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

	// —— 会员 ↔ 订单的端口对接（BIZ-3 消费侧）——
	//
	// 两条方向相反的接线，都用 setter 回填（order 在 membership 之后装配，
	// 改成构造参数就会形成「order 要 membership 的折扣、membership 要 order 的消费额」的环）：
	//
	//	① order ← membership：折扣要读会员身份（Reader，收窄到一条只读方法）。
	//	   未注入 = 会员折扣功能未开启（金额与接入前逐字一致），故是可选降级；
	//	   这里仍然断言注入 —— 本进程内 membership 恒定可得，缺了就是装配缺陷。
	//	② membership ← order：消费额批量只读端口（PurchaseSource）。
	//	   membership 读不到 users 表、也不该读 orders 表，「谁该升级」只有订单侧能回答。
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
	// 日结重算在端口就绪之后启动（幂等）：放在这里而不是会员装配段，
	// 因为端口未注入时 StartRecalcScheduler 只会留一条 Warn 就返回 ——
	// 那会让「日结从来没跑过」看起来像「订单侧没接线」，而实际只是启动顺序错了。
	a.membershipSvc.StartRecalcScheduler()

	// —— 评论（BIZ-5）：独立模块，多态挂载 ——
	//
	// 为什么是独立模块而不是每个实体模块自带一张评论表：评论的横切关注点
	// （审核状态机 / 限流 / 防刷 / 审核后台 / i18n / 分页）与实体无关，
	// 每模块自带等于把这一整套写 N 遍、运营还要跑 N 个后台页。
	//
	// 实体类型的白名单**由拥有该实体的模块声明**（见 commentEntityTypes），
	// comment 模块不认识 article / product 的任何细节：装配层把两个常量搬过去而已。
	// 新增一种可评论实体 = 在这里加一行 + 拥有者模块的常量已存在，不改 comment 模块。
	commentSvc := commenthttp.SetupCommentRoutes(authorizedAPI, a.adminPages, db, a.projectService, commentEntityTypes())
	a.commentSvc = commentSvc

	a.mailSvc = mailSvc
	a.userSvc = userSvc
	a.userAdminSvc = userAdminSvc
	a.orderSvc = orderSvc
}

// commentEntityTypes 评论可挂载的实体类型（**取值由拥有该实体的模块声明**）。
//
// 这是「白名单由拥有者声明」这条原则在装配层的落地形态（同 pkg/datarule 的域声明）：
// comment 模块只认 contract.EntityType 这个结构，取值一律从 content / product 的
// contract 常量搬过来 —— 本模块与装配层都**不抄一份取值表**。
//
// label 的 i18n key 也来自拥有者：
//   - product 侧有现成常量（productcontract.EntityTypeLabel 用的就是它）；
//   - content 侧没有把「文章」收成常量（该词条在模板里以字面量使用），这里引用
//     内容模块自己的词条 key（admin.article.list.heading），而不是新造一个 ——
//     新造会让同一个概念在两处有两种说法（一处改了另一处不知道）。
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

// contentArticleLabelKey 内容模块「文章」的展示名词条（内容模块自己的词条表）。
//
// 写成常量而不是散在调用点：它必须与内容模块词条表里的 key 逐字一致，
// 且 i18n 是**字符串协议**（写错不会编译失败，只会静默回落中文兜底）。
const contentArticleLabelKey = "admin.article.list.heading"

// wireProductInventoryPorts 商品 ↔ 库存之间的端口注入（同一模块内直调 + 跨契约端口）。
//
// 本段的每一次注入都是 fail-fast：漏接的后果全是**静默**的（投影恒为 0、
// 守卫恒被跳过、可用量拿不到会把套餐卖爆），只有断言能让它在启动时暴露。
func (a *assembly) wireProductInventoryPorts() {
	marks := a.marks
	db := a.db
	productSvc := a.productSvc
	inventorySvc := a.inventorySvc
	// 商品只接库存的受限端口；具体 service 留给订单适配器使用。
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
	// Reader 只提供变体投影与删除守卫，使用与库存 service 相同的数据库连接。
	stockReader := inventorymodel.NewModel(db)
	if setter, ok := productSvc.(interface {
		SetInventory(inventorycontract.ProductStockReader)
	}); ok {
		setter.SetInventory(stockReader)
	} else {
		panic("商品模块未提供库存 reader 注入点（SetInventory）")
	}
	marks.mark(portProductInventoryModel)
	// 商品侧库存缓存端口（issue #16）已删除（issue #32）：商品与库存合并为同一模块后，
	// 商品查询直接读库存真源做查询期投影，不再需要缓存副本、同步台账与对账。
	// 成本价写回端口（issue #18）：与 VariantStockCachePort 同向（product 实现、inventory 调用）——
	// 采购收货 / 生产入库登记后把单价写进 product_variants.cost_price。同一手法：断言 + 注入，
	// 任一未实现即 fail-fast（装配缺陷不该拖到运行时才暴露）。
	//
	// 关于下列「运行时类型断言 + fail-fast」为什么保留而不是删掉：
	// 端口形状已由**实现侧的编译期断言**钉住（product/service/product_service.go、
	// product_bundle_validate.go、variant_availability.go、product_variant_snapshot.go 与
	// inventory/service/inventory_service.go 各自的 `var _ xcontract.Xxx = (*Service)(nil)`），
	// 签名漂移现在编译期就报错。但 productSvc / inventorySvc 的静态类型是**接口**
	//（product/inbound/http 与 inventory/inbound/http 的 Setup 返回接口），装配层拿不到具体类型 ——
	// 换实现、注入测试替身都会绕过服务包内的断言。所以这一段保留为装配层的兜底，
	// 而不是删成裸类型转换：裸转换失败只会得到一个无上下文的运行时 panic。
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
	// 运行时兜底，理由见上面「为什么保留」；编译期断言在 inventory/service/inventory_service.go。
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
	// 运行时兜底，理由见上面「为什么保留」；编译期断言在 product/service/product_bundle_validate.go。
	bundlePort, ok := productSvc.(productcontract.BundleConfiguratorPort)
	if !ok {
		panic("商品模块未实现捆绑配置器端口（BundleConfiguratorPort）")
	}
	a.fragDeps.BundleProvider = bundlePort
	marks.mark(portRuntimeFragBundle)
	// 商品变体可用量片段（issue #24）：商品详情规格选择器旁的「实时库存」走访问面片段端点。
	// 同一份注入模式：product 模块实现 VariantAvailabilityLookupPort（内部再调 inventory 的
	// VariantAvailabilityPort 读真源），片段层只管渲染结论。与库存端口一样 fail-fast ——
	// 漏接的表现是「页面上永远显示以结算时库存为准」，比启动时报错隐蔽得多。
	// 运行时兜底，理由见上面「为什么保留」；编译期断言在 product/service/variant_availability.go。
	availabilityLookup, ok := productSvc.(productcontract.VariantAvailabilityLookupPort)
	if !ok {
		panic("商品模块未实现变体可用量查询端口（VariantAvailabilityLookupPort）")
	}
	a.fragDeps.VariantAvailabilityProvider = availabilityLookup
	a.availabilityLookup = availabilityLookup
	marks.mark(portRuntimeFragVariantAvailability)
	// 商品实时价格核对片段（BIZ-2）：定价工具改价只落库、不进构建管线，所以产物里的价
	// 与库里的当前价在时间窗内可能不一致；片段读**当前事实**并在不一致时给访客一句交代。
	// 端口直接复用订单域的 VariantSnapshotPort（按变体 id 读当前价 / 启用态，收窄只读），
	// 不为「读个价」再造一条几乎相同的端口。断言 + 注入，与上面同模式。
	// 运行时兜底，理由见上面「为什么保留」；编译期断言在 product/service/product_variant_snapshot.go。
	variantSnapshots, ok := productSvc.(productcontract.VariantSnapshotPort)
	if !ok {
		panic("商品模块未实现变体快照端口（VariantSnapshotPort）")
	}
	a.fragDeps.VariantSnapshotProvider = variantSnapshots
	marks.mark(portRuntimeFragVariantSnapshot)
}

// wireRuntimeAccessFace 访问面运行时能力：购物车与结算、支付回调、访问统计打点，
// 以及商品实体类型与集合源注册。
// resolvePurposeSecret 读取某个用途的独立签名密钥；未配置时回退到会话密钥并告警。
//
// 为什么是「独立配置」而不是「从会话密钥 HKDF 派生」：单体内派生出的子密钥与主密钥
// 存在于同一个进程内存里 —— 任何能读到子密钥的攻击者同样读得到主密钥，所以派生
// 不提供**隔离**，只避免「同一个字节串被多处直接复用」。真正的收益（轮换某个域时
// 不连带影响别的域）只能来自各自独立、互相不可推导的配置密钥。
//
// 回退而不是 fail-fast：不配也要能跑起来（行为与分离前一致），但降级必须在装配期
// 留下痕迹 —— 运行起来之后「为什么轮换密钥把购物车清空了」不会自己暴露成错误。
// 与 resolveAnonSalt（analytics.pepper，SEC-013）同一套路。
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

	// 购物车与访客结算（BIZ-1 访问面）：
	//   · 购物车状态在**客户端签名 cookie** 里（访客未登录也要能加购），服务端不持久化；
	//   · 结算走订单域建单（落快照 + 扣库存 + 幂等）→ 支付通道扣款 → 订单落账；
	//   · 支付通道现在是**模拟 PayPal**（orders.payment_method = paypal，
	//     流水号由订单号派生，因此天然幂等）。接真通道时只换这一行的实现，
	//     购物车、订单与片段层的代码都不动 —— 通道的接口定义在 cart 模块的契约里。
	sessionSecret := auth.SessionSecret()
	if strings.TrimSpace(sessionSecret) == "" {
		// 没有签名密钥的购物车 cookie 等于没有签名：任何人都能伪造一辆车。
		// 这是装配缺陷（auth 组件必须在本函数之前 Init），fail-fast 而不是降级。
		panic("会话密钥未初始化（auth 组件未 Init），购物车 cookie 无法签名")
	}
	// 密钥按用途分离（P1）。此前这三处共用同一个 auth.SessionSecret()，
	// 代价全在**轮换的爆炸半径**上：轮换会话密钥不只让所有人重新登录，
	// 还会清空所有访客的购物车，并让在途的支付回调验签失败 ——
	// 后者可能丢支付结果，比丢购物车严重得多。
	cartCookieSecret := resolvePurposeSecret("cart.cookie_secret", "购物车 cookie 签名", sessionSecret)
	// 支付通道用回调密钥做验签的共享密钥：模拟通道的签名是
	// HMAC-SHA256(secret, 原始报文)，换成真通道时只改这一行。
	paymentCallbackSecret := resolvePurposeSecret("cart.payment_callback_secret", "支付回调验签", sessionSecret)
	cartSvc := cartservice.NewService(orderSvc, productSvc, availabilityLookup, mockpaypal.New(paymentCallbackSecret), cartCookieSecret)
	// 购物车 ↔ 会员（BIZ-3 免运费）：结算时按等级决定运费。经 setter 注入而不是加进
	// NewService 的签名 —— 那个构造函数在测试里有 10+ 处直调。
	// 未注入 = 免运费未开启（运费与接入前逐字一致），故是可选降级。
	cartSvc.SetMembershipReader(a.membershipSvc)
	marks.mark(portCartMembershipReader)
	// 购物车 ↔ 站点运费规则：结算以站点设置的基础运费（shippingBaseFee）为起点，
	// 满额免运费门槛（shippingFreeThreshold）与会员权益依次作用在同一笔上。
	// 经 setter 注入而不是加进 NewService 的签名 —— 那个构造函数在测试里有 10+ 处直调。
	//
	// 取端口用类型断言而不是把它加进 ProjectService 接口：那是 project 模块的**大接口**，
	// 每加一条方法都会波及全部消费者与测试替身；这里只要「读这个工程的运费规则」一条
	//（见 internal/module/project/contract/shipping_policy.go）。
	// 未接入 = 站点不收运费（结算运费恒 0，与接入前逐字一致），故是可选降级。
	if policyReader, ok := a.projectService.(projectcontract.ShippingPolicyReader); ok {
		cartSvc.SetShippingPolicyReader(policyReader)
		marks.mark(portCartShippingPolicy)
	} else {
		logger.Scene("init").Warn("project 模块未提供站点运费规则读取端口（ShippingPolicyReader）：" +
			"结算运费恒为 0 —— 站点级基础运费与满额免运费都不会生效")
	}
	a.fragDeps.CartProvider = cartSvc
	marks.mark(portRuntimeFragCart)
	// 片段层的会员身份（BIZ-3）：两个新能力（membershipBadge / membershipPanel）的读取端口
	// 与文案出口。可选降级 —— 未注入时片段渲染「会员信息暂时不可用」这句**可见文案**，
	// 而不是 500（片段端点把 error 变成 500，htmx 不 swap，用户什么都看不到）。
	a.fragDeps.MembershipReader = a.membershipSvc
	a.fragDeps.MembershipFacingTexter = a.membershipFacing
	marks.mark(portRuntimeFragMembershipTexter)
	marks.mark(portRuntimeFragMembership)
	// 商品评论差异化规则的**输入**（order → product）：把「某访客买过某商品吗」交给
	// 商品模块，由它实现 commentcontract.EntityPolicy（紧接着的下一段把它注入 comment）。
	//
	// 两段是同一条链路的两个环节，放在一起读：order（事实）→ product（规则）→
	// comment（存储与审核）。分开写会让后来的人以为 purchases 是别的用途。
	//
	// **可选降级**：未注入 = 「买过才能评」未启用（放行），product 侧记 Warn。
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
	// 评论片段（BIZ-5）：两个能力（commentList / commentSubmit）的读写端口、文案出口与
	// 来源 IP 哈希的盐。**可选降级** —— 未注入时片段渲染「评论功能暂时不可用」这句
	// 可见文案，而不是 500（片段端点把 error 变成 500，htmx 不 swap，用户什么都看不到）。
	if a.commentSvc != nil {
		a.fragDeps.CommentPort = a.commentSvc
		a.fragDeps.CommentFacingTexter = a.commentSvc
		marks.mark(portRuntimeFragCommentTexter)
		// 哈希口径留在评论模块（本包 import 它的 service 会被架构门禁拦下），
		// 装配层只把「盐从哪来」这件事接上：按用途分离密钥（同 cart cookie / 支付回调的
		// 既有手法），未配置时回退会话密钥并告警（resolvePurposeSecret 内部记 Warn）。
		commentSalt := resolvePurposeSecret("comment.ip_pepper", "评论来源 IP 哈希", sessionSecret)
		a.fragDeps.CommentSourceHasher = func(ip string) string {
			return commentservice.HashSourceIP(commentSalt, ip)
		}
		marks.mark(portRuntimeFragCommentHasher)
		marks.mark(portRuntimeFragComment)
		// 差异化规则的提供方（「商品评论必须买过」这类）：由**拥有该实体的模块**实现。
		//
		// 本批 product 尚未实现该端口 → 不注入 = **放行**（判断与理由见
		// commentcontract.EntityPolicy 的注释：它是产品策略而不是安全边界，
		// 且未注入即拒绝会把「装配漏了一行」表现成「整站评论功能废掉」）。
		// 留一条 Warn 让「规则没生效」可见 —— 否则它会表现成「评论随便发」而无从解释。
		if policy, ok := a.productSvc.(commentcontract.EntityPolicy); ok {
			// 端口注入点不在 CommentService 契约里（那是模块对外能力清单，
			// 差异化规则只服务消费侧），所以用类型断言取注入点 ——
			// 与 SetMasterDataChanges / SetAvailabilityPort 的既有手法一致。
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
	analyticsSvc := analyticshttp.SetupAnalyticsRoutes(authorizedAPI, router, db, sessionSecret, a.adminPages, a.projectService)
	a.analyticsSvc = analyticsSvc
	// 访问统计工具（traffic_summary）：用户说的「页面浏览 / 运营数据」就是它。
	//
	// 注册点在这里而不是与其它工具并列：它依赖刚刚建出来的 analyticsSvc，
	// 而那个变量到这一行才存在（工具装配的其余部分在主函数前段）。
	// 与订单那批并列的理由仍是成立的 —— 两边的「天」都是 UTC 日界，
	// 模型可以把「这周几单、多少人看」并排放在一起答，而它们来自两次独立取数、
	// 互不污染口径。依赖收窄到 TrafficReader —— 手里没有 Collect。
	if trafficTools, err := analyticsmcp.TrafficTools(analyticsSvc); err != nil {
		panic("访问统计工具装配失败：" + err.Error())
	} else if err := a.tools().RegisterAll(trafficTools...); err != nil {
		panic("访问统计工具注册失败：" + err.Error())
	}
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
