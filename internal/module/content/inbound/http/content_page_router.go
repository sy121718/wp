package contenthttp

// content_page_router.go — 文章后台页面（/admin/articles*）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 article_page*.go / article_handle.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面 GET 的 Casbin 待补（见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3）；
// 写动作已按既有 content:* / page:create / presentation:* 权限点 enforce（迁移 033）。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/content/contract"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/presentation/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
)

// SetupContentPages 注册文章后台页面（/admin 组，中间件链由装配层统一挂好）。
//
// 为什么不像其它模块那样在 SetupContentRoutes 里顺带注册：文章页依赖
// contenttemplate / presentation / page 三个**晚于 content 装配**的契约 ——
// content 必须先装配（实体类型注册表 entityRegistry 与集合源注册表都由它产出，
// 而 presentation 又依赖 entityRegistry），所以 content 的 Setup 阶段根本拿不到
// 这三个契约。装配层在全部契约就绪后调用本函数，装配点与 dashboard 页面并排。
//
// pages 为 nil 时整体跳过（与既有 rg == nil 的早退同构）。
func SetupContentPages(pages *gin.RouterGroup, contents contentcontract.ContentService,
	projects projectcontract.ProjectService, templates contenttemplatecontract.ContentTemplateService,
	pageSvc pagecontract.PageService, instances presentationcontract.PresentationService) {
	if pages == nil {
		return
	}

	// 文章管理页（INF-1）：CMS 内容实体（contents，迁移 080 起只保留 article）的后台入口，
	// 以及 SEO-10 要求的编辑期评测侧栏（密度 / 长度 / 可读性 / 内链）。
	articlePages := NewArticlePageHandle(contents, projects, templates, instances, pageSvc)
	// 内链建议端口（SEO-015）：从同一个 presentation 服务上断言出收窄只读接口。
	// 断言失败（实现方变更）不阻塞启动 —— 建议端点降级为空列表，评分侧栏照常。
	if loc, ok := any(instances).(presentationcontract.PublishedEntityLocator); ok {
		articlePages.SetArticleLinkLocator(loc)
	}
	pages.GET("/articles", shell.PageAuthz("/api/content/list"), articlePages.ArticlesPage)
	// 待重建影响面清单（只读抽屉片段）：与页头徽章同一个查询，徽章给数、抽屉给清单。
	// 鉴权复用列表页的读权限点 —— 片段里就是全站待重建页面清单，不挂鉴权等于把它
	// 露给任何登录账号（菜单隐藏不是访问控制）。GET 与 /api/content/list 的策略动词一致。
	pages.GET("/articles/stale/drawer", builtin.CasbinMiddlewareForPath("/api/content/list"), articlePages.ArticleStaleDrawer)
	// 文章新建整页（弃抽屉，对齐商品的 /admin/products/new）：左正文右实时预览。
	// 页面 GET 本走 /admin 组（Session+CSRF，无 Casbin），但打开它就等于拿到建文章的
	// 表单 —— 与 POST /articles/create 同挂 content:create 权限点，能建才能进。
	//
	// 必须用 CasbinMiddlewareForPathAs 而不是 CasbinMiddlewareForPath：后者取真实请求
	// 方法（这里是 GET）去 enforce，而 `/api/content/create` 的策略声明是 **POST**，
	// 两者不匹配 → 含超管在内全员 403（实测缺陷，页面完全不可达）。这里显式声明
	// 「本页面入口按 POST 语义鉴权」，让动词不匹配这件事在代码里可见。
	pages.GET("/articles/new", builtin.CasbinMiddlewareForPathAs("/api/content/create", http.MethodPost), articlePages.ArticleNewPage)
	pages.GET("/articles/edit", shell.PageAuthz("/api/content/list"), articlePages.ArticleEditPage)
	pages.POST("/articles/create", builtin.CasbinMiddlewareForPath("/api/content/create"), articlePages.ArticleCreate)
	pages.POST("/articles/update", builtin.CasbinMiddlewareForPath("/api/content/update"), articlePages.ArticleUpdate)
	pages.POST("/articles/delete", builtin.CasbinMiddlewareForPath("/api/content/delete"), articlePages.ArticleDelete)
	// 批量删除复用单条删除的权限点（不新增权限点、不写迁移）：能删一篇的人就能删一批。
	pages.POST("/articles/bulk-delete", builtin.CasbinMiddlewareForPath("/api/content/delete"), articlePages.ArticlesBulkDelete)

	// 文章翻译工作台（审计 I18N-006）：对称商品的 /admin/products/translations。
	// 保存复用「保存内容」权限点（与文章编辑同源）—— 译文是文章内容的一部分，
	// 另立权限点只会让「能改文章但不能改它的译文」这种半吊子配置出现。
	articleTranslations := NewArticleTranslationHandle(contents)
	// 写入端口用默认构造（从全局库句柄取）：本函数的参数里没有 db，
	// 而工作台的写入通道与用户的读取通道必须指向同一个库 —— 显式传参会引出一个
	// 「传错库也能编译通过」的口子，默认构造反而更稳。构造失败时不注入，
	// 工作台仍可看原文（只是保存会提示存储不可用）。
	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		articleTranslations.SetContentWriter(writer)
	}
	pages.GET("/articles/translations", shell.PageAuthz("/api/content/list"), articleTranslations.ArticleTranslations)
	pages.POST("/articles/translations/save", builtin.CasbinMiddlewareForPath("/api/content/update"), articleTranslations.SaveArticleTranslations)
	// 评分是纯计算（不写库、不写产物），只走组级 Session+CSRF，不再叠权限点：
	// 能打开编辑页的人就能算分，分数本身不构成新的信息公开面。
	pages.POST("/articles/score", articlePages.ArticleScorePanel)
	// SEO 评测抽屉片段（只读观测 + 一个 htmx 动作）：与编辑页同一权限点（content:list 的
	// 写侧是 content:update，评测本身不改数据，按读侧鉴权即可）。GET 与策略动词一致。
	pages.GET("/articles/seo/drawer", builtin.CasbinMiddlewareForPath("/api/content/list"), articlePages.ArticleSeoDrawer)
	// 详情页真实预览帧（iframe 直接 src）：页面组鉴权（Session），**不挂 Casbin** ——
	// 与评分抽屉同一取舍：它只读渲染、不落库，而 iframe 无法携带 CSRF 头。
	pages.GET("/articles/preview-frame", articlePages.ArticlePreviewFrame)
	// 内链建议（SEO-015）：同为只读计算（候选来自已上线路径解析端口），
	// 权限策略与评分侧栏一致 —— Session + CSRF，不叠权限点。
	pages.POST("/articles/link-suggestions", articlePages.ArticleLinkSuggestions)
	// 文章 → 画布（06-B 决策 5 的第一个真实用途）：预览是纯计算不叠权限点，
	// 创建页面复用页面创建权限点（写的是 Page 草稿，与 /admin/pages 新建同一件事）。
	pages.POST("/articles/import-preview", articlePages.ArticleImportPreview)
	pages.POST("/articles/import-page", builtin.CasbinMiddlewareForPath("/api/page/create"), articlePages.ArticleImportCreate)
	pages.POST("/articles/publish", builtin.CasbinMiddlewareForPath("/api/presentation/create"), articlePages.ArticlePublish)
	pages.POST("/articles/rebuild", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), articlePages.ArticleRebuild)
	pages.POST("/articles/url", builtin.CasbinMiddlewareForPath("/api/presentation/update-url"), articlePages.ArticleUpdateURL)
}
