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
	mediahttp "go_wp/internal/module/media/inbound/http"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navsource "go_wp/internal/module/navigation/outbound/source"
	pagehttp "go_wp/internal/module/page/inbound/http"
	pluginhttp "go_wp/internal/module/plugin/inbound/http"
	presentationhttp "go_wp/internal/module/presentation/inbound/http"
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
	// CMS 内容（0-A2，contenttemplate/presentation 依赖其字段白名单契约）。
	contentSvc := contenthttp.SetupContentRoutes(authorizedAPI, db)
	// 内容结构模板（presentation 依赖 ResolveTemplate；模板行需 project_id 外键）。
	contentTemplateSvc := contenttemplatehttp.SetupContentTemplateRoutes(authorizedAPI, db, projectService)
	// 自动发布实例（内容实体驱动，复用编译/存储/激活管线；实例行需 project_id 外键）。
	presentationSvc := presentationhttp.SetupPresentationRoutes(authorizedAPI, db, contentTemplateSvc, contentSvc, projectService)

	// 插件模块（page 构建路径依赖其装配素材，须先于 page 装配）。
	// plugin 是外部插件宿主：注入 admin 权限上下文契约，供插件运行时读取当前用户权限。
	pluginSvc := pluginhttp.SetupPluginRoutes(authorizedAPI, db, adminAuthzSvc)
	// content service 同时实现 core.CollectionResolver（插件集合绑定渲染）。
	collectionResolver, _ := contentSvc.(core.CollectionResolver)
	// navigationSvc 注入 page 装配：core.nav 绑定菜单位置时构建期解析菜单项。
	pageService := pagehttp.SetupPageRoutes(authorizedAPI, db, artifactSvc, publicationSvc, projectService, blockSvc, pluginSvc, collectionResolver, navigationSvc, mediaSvc)

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
		adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminAuthzSvc, navigationSvc)

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
