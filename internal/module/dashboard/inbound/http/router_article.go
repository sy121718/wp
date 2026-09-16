package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	presentationcontract "go_wp/internal/module/presentation/contract"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// router_article.go - 文章与内容模板页路由（含译文工作台、评分、内链与发布动作）。

func setupArticleRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	contentTemplatePages := newContentTemplatePageHandle(d.templates, d.projects, d.products, d.contents)

	// 文章管理页（INF-1）：CMS 内容实体（contents，迁移 080 起只保留 article）的后台入口，
	// 以及 SEO-10 要求的编辑期评测侧栏（密度 / 长度 / 可读性 / 内链）。
	// 页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；写动作复用既有 content:* 权限点（迁移 033），
	// 本次不新增权限点；发布复用商品详情模板页那两条 presentation 权限点（同一行为，只是实体类型不同）。
	// 侧栏入口见 nav_menu.go 的 content 组。
	articlePages := NewArticlePageHandle(d.contents, d.projects, d.templates, d.presentations, d.pages)
	// 内链建议端口（SEO-015）：从同一个 presentation 服务上断言出收窄只读接口。
	// 断言失败（实现方变更）不阻塞启动 —— 建议端点降级为空列表，评分侧栏照常。
	if loc, ok := any(d.presentations).(presentationcontract.PublishedEntityLocator); ok {
		articlePages.SetArticleLinkLocator(loc)
	}
	adminPages.GET("/articles", articlePages.ArticlesPage)
	adminPages.GET("/articles/edit", articlePages.ArticleEditPage)
	// 内容模板（EDT-001）：列表 + 编辑入口（302 到工作台）。
	adminPages.GET("/content-templates", contentTemplatePages.ContentTemplatesPage)
	adminPages.GET("/content-templates/edit", contentTemplatePages.ContentTemplateEditPage)
	adminPages.POST("/articles/create", builtin.CasbinMiddlewareForPath("/api/content/create"), articlePages.ArticleCreate)
	adminPages.POST("/articles/update", builtin.CasbinMiddlewareForPath("/api/content/update"), articlePages.ArticleUpdate)
	adminPages.POST("/articles/delete", builtin.CasbinMiddlewareForPath("/api/content/delete"), articlePages.ArticleDelete)

	// 文章翻译工作台（审计 I18N-006）：对称商品的 /admin/products/translations。
	// 保存复用「保存内容」权限点（与文章编辑同源）—— 译文是文章内容的一部分，
	// 另立权限点只会让「能改文章但不能改它的译文」这种半吊子配置出现。
	articleTranslations := NewArticleTranslationHandle(d.contents)
	// 写入端口用默认构造（从全局库句柄取）：本函数的参数里没有 db，
	// 而工作台的写入通道与用户的读取通道必须指向同一个库 —— 显式传参会引出一个
	// 「传错库也能编译通过」的口子，默认构造反而更稳。构造失败时不注入，
	// 工作台仍可看原文（只是保存会提示存储不可用）。
	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		articleTranslations.SetContentWriter(writer)
	}
	adminPages.GET("/articles/translations", articleTranslations.ArticleTranslations)
	adminPages.POST("/articles/translations/save", builtin.CasbinMiddlewareForPath("/api/content/update"), articleTranslations.SaveArticleTranslations)
	// 评分是纯计算（不写库、不写产物），只走组级 Session+CSRF，不再叠权限点：
	// 能打开编辑页的人就能算分，分数本身不构成新的信息公开面。
	adminPages.POST("/articles/score", articlePages.ArticleScorePanel)
	// 内链建议（SEO-015）：同为只读计算（候选来自已上线路径解析端口），
	// 权限策略与评分侧栏一致 —— Session + CSRF，不叠权限点。
	adminPages.POST("/articles/link-suggestions", articlePages.ArticleLinkSuggestions)
	// 文章 → 画布（06-B 决策 5 的第一个真实用途）：预览是纯计算不叠权限点，
	// 创建页面复用页面创建权限点（写的是 Page 草稿，与 /admin/pages 新建同一件事）。
	adminPages.POST("/articles/import-preview", articlePages.ArticleImportPreview)
	adminPages.POST("/articles/import-page", builtin.CasbinMiddlewareForPath("/api/page/create"), articlePages.ArticleImportCreate)
	adminPages.POST("/articles/publish", builtin.CasbinMiddlewareForPath("/api/presentation/create"), articlePages.ArticlePublish)
	adminPages.POST("/articles/rebuild", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), articlePages.ArticleRebuild)
	adminPages.POST("/articles/url", builtin.CasbinMiddlewareForPath("/api/presentation/update-url"), articlePages.ArticleUpdateURL)
}
