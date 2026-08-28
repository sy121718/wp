// Package dashboardhttp 注册 dashboard 模块的页面路由。
package dashboardhttp

import (
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"

	"github.com/gin-gonic/gin"
)

// SetupDashboardRoutes 注册后台页面路由（挂载到引擎根路径）。
func SetupDashboardRoutes(router *gin.Engine,
	pages pagecontract.PageService,
	projects projectcontract.ProjectService) {
	if router == nil {
		return
	}

	handle := NewHandle(pages, projects)

	// 页面路由
	router.GET("/", handle.Dashboard)
	router.GET("/admin", handle.Dashboard)
	// 可视化编辑器外壳、已保存草稿预览与临时 AST 预览（均不影响线上产物）。
	router.GET("/workbench", handle.Workbench)
	router.GET("/workbench/preview", handle.Preview)
	router.POST("/workbench/preview", handle.PreviewDraft)
	// 页面管理列表：列出/新建站点工程与页面。
	router.GET("/admin/pages", handle.PagesList)
	router.POST("/admin/pages/create", handle.CreatePage)
	router.POST("/admin/projects/create", handle.CreateProject)
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
