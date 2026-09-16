package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// router_site.go - 站点内容与配置页路由：页面/翻译、导航、块、媒体、主题、站点设置、插件。

func setupSiteRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	adminPages.GET("", handle.Dashboard)
	// 语言切换（多语言 P1）：校验 lang → 写 Cookie → 302 回跳。
	// 挂 admin 页面组（Session + CSRF，不走 Casbin）：GET 属安全方法，CSRF 直接放行；
	// 无 Handle 依赖，故为包级 handler。
	adminPages.GET("/lang", LangSwitch)
	// 页面管理列表：列出/新建站点工程与页面。
	// 页面写操作复用对应 API 权限点做 Casbin 鉴权（页面路径与权限点路径不一致，
	// 直接以页面路径 enforce 会因权限点表无此路径而拒绝所有用户）。
	adminPages.GET("/pages", handle.PagesList)
	adminPages.POST("/pages/create", builtin.CasbinMiddlewareForPath("/api/page/create"), handle.CreatePage)
	// 翻译工作台（多语言 P5c，docs/06-D §7.8）：入口在页面列表行内「多语言」按钮，不做独立菜单。
	// 保存写 sys_translation（engine=manual）并触发全站标记待重建，鉴权复用「保存草稿」权限点。
	adminPages.GET("/page/translations", handle.PageTranslations)
	adminPages.POST("/page/translations/save", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), handle.SavePageTranslations)
	adminPages.POST("/projects/create", builtin.CasbinMiddlewareForPath("/api/project/create"), handle.CreateProject)
	// 全局块管理：页眉/页脚/区块（编辑进工作台；stale 传播在本模块编排）。
	// 前台导航菜单（公开站点导航，与后台权限菜单严格隔离）：结构树 + 排序 + 打开方式。
	adminPages.GET("/navigations", handle.NavigationsPage)
	adminPages.POST("/navigations/create", builtin.CasbinMiddlewareForPath("/api/navigation/create"), handle.NavigationCreate)
	adminPages.POST("/navigations/add-source", builtin.CasbinMiddlewareForPath("/api/navigation/create"), handle.NavigationAddSource)
	adminPages.POST("/navigations/update", builtin.CasbinMiddlewareForPath("/api/navigation/update"), handle.NavigationUpdate)
	adminPages.POST("/navigations/delete", builtin.CasbinMiddlewareForPath("/api/navigation/delete"), handle.NavigationDelete)
	// 导航译文工作台（审计 I18N-007）：菜单标签不在页面文档里，页面翻译工作台看不到它，
	// 构建期靠 navigation.label 语境回填 —— 本页是那个语境的唯一维护入口。
	// 保存复用「修改导航」权限点：译文是导航项内容的一部分。
	navigationTranslations := NewNavigationTranslationHandle(d.navigations, d.projects)
	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		navigationTranslations.SetContentWriter(writer)
	}
	if marker, ok := d.pages.(navTranslationPageMarker); ok {
		navigationTranslations.SetPageMarker(marker)
	}
	adminPages.GET("/navigations/translations", navigationTranslations.NavigationTranslations)
	adminPages.POST("/navigations/translations/save", builtin.CasbinMiddlewareForPath("/api/navigation/update"), navigationTranslations.SaveNavigationTranslations)
	adminPages.POST("/navigations/move", builtin.CasbinMiddlewareForPath("/api/navigation/update"), handle.NavigationMove)
	adminPages.GET("/blocks", handle.BlocksList)
	adminPages.POST("/blocks/create", builtin.CasbinMiddlewareForPath("/api/block/create"), handle.CreateBlock)
	adminPages.POST("/blocks/delete", builtin.CasbinMiddlewareForPath("/api/block/delete"), handle.DeleteBlock)
	// 工作台保存块内容（保存后编排 stale 传播）。
	adminPages.POST("/blocks/save-content", builtin.CasbinMiddlewareForPath("/api/block/update"), handle.SaveBlockContent)
	// 媒体库（左树右库：分类树筛选 + WP 式网格/列表 + 详情编辑）。
	adminPages.GET("/media", handle.MediaPage)
	// 主题管理（多主题：列表/新建/激活/删除 + 单主题设置）。
	adminPages.GET("/themes", handle.ThemeManage)
	adminPages.POST("/themes/create", builtin.CasbinMiddlewareForPath("/api/theme/create"), handle.CreateTheme)
	adminPages.POST("/themes/activate", builtin.CasbinMiddlewareForPath("/api/theme/activate"), handle.ActivateTheme)
	adminPages.POST("/themes/delete", builtin.CasbinMiddlewareForPath("/api/theme/delete"), handle.DeleteTheme)
	adminPages.GET("/themes/settings", handle.ThemeSettings)
	adminPages.POST("/themes/settings/save", builtin.CasbinMiddlewareForPath("/api/theme/update"), handle.SaveThemeSettings)
	// 旧单主题设置入口 → 新主题管理页。
	adminPages.GET("/theme", handle.ThemeRedirect)
	// 站点设置（基础站点信息：站点名/简介/联系邮箱，走 project SiteSettings）。
	adminPages.GET("/settings", handle.SiteSettings)
	adminPages.POST("/settings/save", builtin.CasbinMiddlewareForPath("/api/project/update"), handle.SaveSiteSettings)
	// 站点语言清单（多语言 P3）：行片段走 HTMX 服务端渲染，保存复用 project 更新权限点。
	adminPages.POST("/settings/locales/rows", builtin.CasbinMiddlewareForPath("/api/project/update"), handle.LocaleRowsFragment)
	adminPages.POST("/settings/locales/save", builtin.CasbinMiddlewareForPath("/api/project/update"), handle.SaveSiteLocales)
	// 插件管理（列表/上传安装/启停/卸载，docs/06-plugin-system.md）。
	adminPages.GET("/plugins", handle.PluginsPage)
	adminPages.POST("/plugins/install", builtin.CasbinMiddlewareForPath("/api/plugin/install"), handle.PluginsInstall)
	adminPages.POST("/plugins/toggle", builtin.CasbinMiddlewareForPath("/api/plugin/toggle"), handle.PluginsToggle)
	adminPages.POST("/plugins/uninstall", builtin.CasbinMiddlewareForPath("/api/plugin/uninstall"), handle.PluginsUninstall)
}
