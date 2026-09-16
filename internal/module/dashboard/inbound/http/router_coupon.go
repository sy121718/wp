package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_coupon.go - 优惠码页与商品域译文工作台路由。

func setupCouponRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	// 优惠码管理页（BIZ-1）：列表 + 新建 + 修改（含停用/启用）+ 删除 + 核销记录。
	// 核销**没有手工入口** —— 它发生在建单事务内，页面只展示结果。
	// 写动作复用优惠码 API 权限点（迁移 142）。
	couponPages := NewCouponPageHandle(d.orders, d.projects)
	adminPages.GET("/coupons", couponPages.CouponsPage)
	adminPages.POST("/coupons/create", builtin.CasbinMiddlewareForPath("/api/order/coupon/create"), couponPages.CouponCreate)
	adminPages.POST("/coupons/update", builtin.CasbinMiddlewareForPath("/api/order/coupon/update"), couponPages.CouponUpdate)
	adminPages.POST("/coupons/delete", builtin.CasbinMiddlewareForPath("/api/order/coupon/delete"), couponPages.CouponDelete)
}

func setupProductTranslationWorkbench(adminPages *gin.RouterGroup, d *routeDeps) {
	// 商品域翻译工作台（issue #12）：入口在商品列表行内「多语言」按钮（与页面翻译工作台同构）。
	// 保存写 sys_translation（engine=manual）并标记待重建，鉴权复用商品更新权限点（同一改动面）。
	SetupProductTranslationRoutes(adminPages,
		builtin.CasbinMiddlewareForPath("/api/product/update"), d.products, d.projects, d.pages, d.presentations)
}
