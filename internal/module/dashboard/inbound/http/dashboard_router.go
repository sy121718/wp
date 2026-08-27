// Package dashboardhttp 注册 dashboard 模块的页面路由。
package dashboardhttp

import (
	"github.com/gin-gonic/gin"
)

// SetupDashboardRoutes 注册后台页面路由（挂载到引擎根路径）。
func SetupDashboardRoutes(router *gin.Engine, pages PageReader) {
	if router == nil {
		return
	}

	handle := NewHandle(pages)

	// 页面路由
	router.GET("/", handle.Dashboard)
	router.GET("/admin", handle.Dashboard)
	// 可视化编辑器外壳、已保存草稿预览与临时 AST 预览（均不影响线上产物）。
	router.GET("/workbench", handle.Workbench)
	router.GET("/workbench/preview", handle.Preview)
	router.POST("/workbench/preview", handle.PreviewDraft)
}
