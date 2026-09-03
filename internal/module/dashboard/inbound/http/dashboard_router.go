// Package dashboardhttp 注册 dashboard 模块的页面路由。
package dashboardhttp

import (
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

	// 页面路由
	router.GET("/", handle.Dashboard)
	router.GET("/admin", handle.Dashboard)
	// 可视化编辑器外壳、已保存草稿预览与临时 AST 预览（均不影响线上产物）。
	router.GET("/workbench", handle.Workbench)
	router.GET("/workbench/preview", handle.Preview)
	router.POST("/workbench/preview", handle.PreviewDraft)
	// 全局块画布预览（工作台块编辑模式 iframe 内嵌）。
	router.GET("/workbench/block/preview", handle.BlockPreview)
	// 页面管理列表：列出/新建站点工程与页面。
	router.GET("/admin/pages", handle.PagesList)
	router.POST("/admin/pages/create", handle.CreatePage)
	router.POST("/admin/projects/create", handle.CreateProject)
	// 全局块管理：页眉/页脚/区块（编辑进工作台；stale 传播在本模块编排）。
	router.GET("/admin/blocks", handle.BlocksList)
	router.POST("/admin/blocks/create", handle.CreateBlock)
	router.POST("/admin/blocks/delete", handle.DeleteBlock)
	// 工作台保存块内容（保存后编排 stale 传播）。
	router.POST("/admin/blocks/save-content", handle.SaveBlockContent)
	// 媒体库（左树右库：分类树筛选 + WP 式网格/列表 + 详情编辑）。
	router.GET("/admin/media", handle.MediaPage)
	// 主题管理（多主题：列表/新建/激活/删除 + 单主题设置）。
	router.GET("/admin/themes", handle.ThemeManage)
	router.POST("/admin/themes/create", handle.CreateTheme)
	router.POST("/admin/themes/activate", handle.ActivateTheme)
	router.POST("/admin/themes/delete", handle.DeleteTheme)
	router.GET("/admin/themes/settings", handle.ThemeSettings)
	router.POST("/admin/themes/settings/save", handle.SaveThemeSettings)
	// 旧单主题设置入口 → 新主题管理页。
	router.GET("/admin/theme", handle.ThemeRedirect)
}
