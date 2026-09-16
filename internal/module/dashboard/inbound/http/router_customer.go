package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_customer.go - 客户管理页路由（列表/详情/停用/解锁）。

func setupCustomerRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	// 客户管理页：后台此前没有任何地方读 users 表 —— 管理员看不到客户列表、不能按客户看订单、
	// 不能停用或解锁账号。页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；
	// 写动作走新权限点 user:customer_status / user:customer_unlock（迁移 152），
	// 与 /api/customer/* 那组接口是同一个权限点（页面的动作不另立一套授权）。
	customerPages := NewCustomerPageHandle(d.customerAdmin, d.orders, d.projects)
	adminPages.GET("/customers", customerPages.CustomersPage)
	adminPages.GET("/customers/detail", customerPages.CustomerDetailPage)
	adminPages.POST("/customers/status",
		builtin.CasbinMiddlewareForPath("/api/customer/status"), customerPages.CustomerStatusSave)
	adminPages.POST("/customers/unlock",
		builtin.CasbinMiddlewareForPath("/api/customer/unlock"), customerPages.CustomerUnlock)
}
