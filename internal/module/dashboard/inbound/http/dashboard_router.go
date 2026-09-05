// Package dashboardhttp 注册 dashboard 模块的页面路由。
package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"

	"github.com/gin-gonic/gin"
)

// SetupDashboardRoutes 注册后台页面路由（挂载到引擎根路径）。
func SetupDashboardRoutes(router *gin.Engine,
	pages pagecontract.PageService,
	projects projectcontract.ProjectService,
	blocks blockcontract.BlockService) {
	if router == nil {
		return
	}

	handle := NewHandle(pages, projects, blocks)

	// 登录页：不挂认证（未登录请求被中间件 302 到此，独立布局渲染登录表单）。
	router.GET("/admin/login", handle.LoginPage)

	// 页面路由（全部挂 Session 认证：未登录的页面请求由中间件 302 到 /admin/login）。
	authPages := router.Group("", builtin.SessionAuthMiddleware())
	authPages.GET("/", handle.Dashboard)
	authPages.GET("/workbench", handle.Workbench)
	authPages.GET("/workbench/preview", handle.Preview)
	authPages.POST("/workbench/preview", handle.PreviewDraft)
	// 全局块画布预览（工作台块编辑模式 iframe 内嵌）。
	authPages.GET("/workbench/block/preview", handle.BlockPreview)

	// /admin/* 后台页面统一挂 Session 认证。
	adminPages := router.Group("/admin", builtin.SessionAuthMiddleware())
	adminPages.GET("", handle.Dashboard)
	// 页面管理列表：列出/新建站点工程与页面。
	adminPages.GET("/pages", handle.PagesList)
	adminPages.POST("/pages/create", handle.CreatePage)
	adminPages.POST("/projects/create", handle.CreateProject)
	// 全局块管理：页眉/页脚/区块（编辑进工作台；stale 传播在本模块编排）。
	adminPages.GET("/blocks", handle.BlocksList)
	adminPages.POST("/blocks/create", handle.CreateBlock)
	adminPages.POST("/blocks/delete", handle.DeleteBlock)
	// 工作台保存块内容（保存后编排 stale 传播）。
	adminPages.POST("/blocks/save-content", handle.SaveBlockContent)
	// 媒体库（左树右库：分类树筛选 + WP 式网格/列表 + 详情编辑）。
	adminPages.GET("/media", handle.MediaPage)
	// 主题管理（多主题：列表/新建/激活/删除 + 单主题设置）。
	adminPages.GET("/themes", handle.ThemeManage)
	adminPages.POST("/themes/create", handle.CreateTheme)
	adminPages.POST("/themes/activate", handle.ActivateTheme)
	adminPages.POST("/themes/delete", handle.DeleteTheme)
	adminPages.GET("/themes/settings", handle.ThemeSettings)
	adminPages.POST("/themes/settings/save", handle.SaveThemeSettings)
	// 旧单主题设置入口 → 新主题管理页。
	adminPages.GET("/theme", handle.ThemeRedirect)
}
