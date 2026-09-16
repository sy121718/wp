package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_order.go - 订单与退货页路由（含状态流转、取消、退款、备注与退货入库）。

func setupOrderRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	// 订单管理页（BIZ-1）：列表 + 状态计数 + 详情（同一页面靠 orderId 展开）+ 流转 / 取消 / 退款。
	// 页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；写动作复用订单 API 权限点做 Casbin 鉴权。
	// 状态合法性不在这里判断：服务端状态机拒绝哪条边，页面就把哪条边藏起来 ——
	// 前端最多只能少给一个按钮，给多了也只是被服务端拒掉并原样回显原因。
	orderPages := NewOrderPageHandle(d.orders, d.projects)
	adminPages.GET("/orders", orderPages.OrdersPage)
	adminPages.POST("/orders/status", builtin.CasbinMiddlewareForPath("/api/order/status"), orderPages.OrderStatusChange)
	adminPages.POST("/orders/cancel", builtin.CasbinMiddlewareForPath("/api/order/cancel"), orderPages.OrderCancel)
	adminPages.POST("/orders/refund", builtin.CasbinMiddlewareForPath("/api/order/refund"), orderPages.OrderRefund)
	// 后台备注（迁移 146 的 order:note）：只改 admin_note 一列，不写状态流转。
	adminPages.POST("/orders/note", builtin.CasbinMiddlewareForPath("/api/order/note"), orderPages.OrderNoteSave)

	// 退货入库（BIZ-1）：客户申请 → 审核 → **先入库、后退款**。
	// 页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；写动作复用退货 API 权限点（迁移 145）。
	returnPages := NewReturnPageHandle(d.orders, d.projects, d.inventories)
	adminPages.GET("/returns", returnPages.ReturnsPage)
	adminPages.POST("/returns/approve", builtin.CasbinMiddlewareForPath("/api/order/return/approve"), returnPages.ReturnApprove)
	adminPages.POST("/returns/reject", builtin.CasbinMiddlewareForPath("/api/order/return/reject"), returnPages.ReturnReject)
	adminPages.POST("/returns/receive", builtin.CasbinMiddlewareForPath("/api/order/return/receive"), returnPages.ReturnReceive)
}
