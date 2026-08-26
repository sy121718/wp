// Package dashboardhttp 注册 dashboard 模块的页面路由。
package dashboardhttp

import (
	"github.com/gin-gonic/gin"
)

// SetupDashboardRoutes 注册后台页面路由（挂载到引擎根路径）。
func SetupDashboardRoutes(router *gin.Engine) {
	if router == nil {
		return
	}

	handle := NewHandle()

	// 页面路由
	router.GET("/", handle.Dashboard)
	router.GET("/admin", handle.Dashboard)
	router.GET("/builder", handle.Builder)
}