package contenthttp

// article_router.go — 文章后台页面路由（INF-1 / SEO-10 / SEO-015 / I18N-006）。
//
// 与原 dashboard 版 router_article.go 的差异只有两处：
//   - 内容模板页（/admin/content-templates*）归 contenttemplate 模块，不在这里注册；
//   - 页面注册从「模块 Setup 内部」提到独立导出函数 —— 原因见 SetupContentPages 的注释。
//
// 权限点一个字符都没动：页面 GET 走 /admin 组（Session + CSRF，无 Casbin），
// 写动作复用既有 content:* / page:create / presentation:* 权限点（迁移 033）。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	projectcontract "go_wp/internal/module/project/contract"
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
	pages.GET("/articles", articlePages.ArticlesPage)
	pages.GET("/articles/edit", articlePages.ArticleEditPage)
	pages.POST("/articles/create", builtin.CasbinMiddlewareForPath("/api/content/create"), articlePages.ArticleCreate)
	pages.POST("/articles/update", builtin.CasbinMiddlewareForPath("/api/content/update"), articlePages.ArticleUpdate)
	pages.POST("/articles/delete", builtin.CasbinMiddlewareForPath("/api/content/delete"), articlePages.ArticleDelete)

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
	pages.GET("/articles/translations", articleTranslations.ArticleTranslations)
	pages.POST("/articles/translations/save", builtin.CasbinMiddlewareForPath("/api/content/update"), articleTranslations.SaveArticleTranslations)
	// 评分是纯计算（不写库、不写产物），只走组级 Session+CSRF，不再叠权限点：
	// 能打开编辑页的人就能算分，分数本身不构成新的信息公开面。
	pages.POST("/articles/score", articlePages.ArticleScorePanel)
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
