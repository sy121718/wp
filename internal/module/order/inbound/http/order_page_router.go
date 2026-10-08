package orderhttp

// order_page_router.go — 订单域后台页面（/admin/orders*、/returns、/coupons）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 order_list_page.go / return_page.go / coupon_page.go / order_create_page.go / sales_page.go。
// 路由注册只出现在 *_router.go，门禁 scripts/check-route-registration-placement.sh 守这条。
//
// 页面写动作复用订单 API 的权限点，由 builtin.CasbinMiddlewareForPath 按**实际 API 路径**鉴权
// —— 权限点路径一个字符都不能改。页面 GET 的 Casbin 待补（见 docs/02-Z §4.3）。
// 页面 handler 与 API handler 同用一个 svc 实例（同一个契约，不另建一套）。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	ordercontract "go_wp/internal/module/order/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"
)

// ——— 后台页面（/admin/*，BIZ-1 销售侧）———
//
// 页面 GET 走 /admin 组认证（Session + CSRF，无 Casbin）；写动作复用订单 API 的权限点，
// 由 builtin.CasbinMiddlewareForPath 按**实际 API 路径**鉴权 —— 权限点路径一个字符都不能改。
// 页面 handler 与 API handler 同用这一个 svc 实例（同一个契约，不另建一套）。

// SetupOrderPages 注册订单域后台页面；pages 为 nil 时整体跳过。
func SetupOrderPages(pages *gin.RouterGroup, svc ordercontract.OrderService,
	projects projectcontract.ProjectService, product productcontract.ProductService,
	warehouses ordercontract.ReturnWarehouseSource, dict sysconfigcontract.DictReader) {
	if pages == nil {
		return
	}
	// 订单管理页：列表 + 状态计数 + 详情（同一页面靠 orderId 展开）+ 流转 / 取消 / 退款 / 备注。
	// 状态合法性不在这里判断：服务端状态机拒绝哪条边，页面就把哪条边藏起来 ——
	// 前端最多只能少给一个按钮，给多了也只是被服务端拒掉并原样回显原因。
	orderPages := NewOrderPageHandle(svc, projects, dict)
	pages.GET("/orders", orderPages.OrdersPage)
	// 销售概览页（GET /admin/orders/overview）：整页只读报表，**没有任何写动作**，
	// 所以不需要 builtin.CasbinMiddlewareForPath（那几个是给页面上的写按钮用的），
	// 与 /orders 同样的只读页待遇 —— 靠 /admin 组的 Session + CSRF 与侧栏入口控制。
	salesPages := NewOrderSalesPageHandle(svc, projects)
	pages.GET("/orders/overview", salesPages.SalesOverviewPage)
	// 但这一页**要有一个权限点**：声明它不为拦请求（页面组不过 Casbin），而是为了
	// 让它成为可授权的对象 —— 侧栏菜单绑 order:overview，角色分权经 menu_ids 收集到它，
	// 而 permission.RoutesOf 也才有非空结果（空集会被工具侧按 fail closed 一律 forbidden）。
	// 声明放在这里（与路由同一处），启动期 SyncToDB 幂等 upsert 进 sys_permission，
	// **不写权限点 seed 迁移**（AGENTS.md §数据库：新增权限点加常量 + 在路由注册处声明）。
	permission.Declare(http.MethodGet, "/admin/orders/overview", permission.OrderOverview)
	// 后台代客建单页（docs/02-W-admin-order-create.md）：独立整页，页头与空态两个入口
	// 都指向它（同一个 URL）。写动作复用 order:create —— 与上面几条同手法，
	// **不新增权限点、不写 seed 迁移**；权限点路径一个字符都不能改。
	//
	// 这一页是整页表单（字段 20+、明细可多行），失败一律**就地重渲 200 + 回填**，
	// 不走 303 + ?err= 回跳：303 之后是 GET，没有 PostForm，几十个字段必然全丢。
	orderCreate := NewOrderCreatePageHandle(svc, projects, product, dict)
	pages.GET("/orders/new", orderCreate.OrderCreatePage)
	pages.POST("/orders/create", builtin.CasbinMiddlewareForPath("/api/order/create"), orderCreate.OrderCreateSubmit)
	pages.POST("/orders/status", builtin.CasbinMiddlewareForPath("/api/order/status"), orderPages.OrderStatusChange)
	pages.POST("/orders/cancel", builtin.CasbinMiddlewareForPath("/api/order/cancel"), orderPages.OrderCancel)
	pages.POST("/orders/refund", builtin.CasbinMiddlewareForPath("/api/order/refund"), orderPages.OrderRefund)
	// 后台备注（迁移 146 的 order:note）：只改 admin_note 一列，不写状态流转。
	pages.POST("/orders/note", builtin.CasbinMiddlewareForPath("/api/order/note"), orderPages.OrderNoteSave)
	// 列表级批量动作：逐条走上面那两条单条路径的 service 用例，权限点也逐字复用它们，
	// 不新增权限点、不写迁移（新增权限点就要同批 seed，否则含超管在内全员 403）。
	pages.POST("/orders/bulk-status", builtin.CasbinMiddlewareForPath("/api/order/status"), orderPages.OrderBulkStatus)
	pages.POST("/orders/bulk-cancel", builtin.CasbinMiddlewareForPath("/api/order/cancel"), orderPages.OrderBulkCancel)

	// 退货入库（RMA）：客户在访问面提交申请，后台在这里审核与收货。
	// **先入库、后退款**的强顺序由 service 的 ReceiveReturn 保证。
	returnPages := NewReturnPageHandle(svc, projects, warehouses, dict)
	pages.GET("/returns", returnPages.ReturnsPage)
	pages.POST("/returns/approve", builtin.CasbinMiddlewareForPath("/api/order/return/approve"), returnPages.ReturnApprove)
	pages.POST("/returns/reject", builtin.CasbinMiddlewareForPath("/api/order/return/reject"), returnPages.ReturnReject)
	pages.POST("/returns/receive", builtin.CasbinMiddlewareForPath("/api/order/return/receive"), returnPages.ReturnReceive)
	// 批量审核：同意与拒绝各复用单条动作的权限点。
	pages.POST("/returns/bulk-approve", builtin.CasbinMiddlewareForPath("/api/order/return/approve"), returnPages.ReturnBulkApprove)
	pages.POST("/returns/bulk-reject", builtin.CasbinMiddlewareForPath("/api/order/return/reject"), returnPages.ReturnBulkReject)

	// 优惠码管理页：列表 + 新建 + 修改（含停用 / 启用）+ 删除 + 核销记录。
	// 核销**没有手工入口** —— 它发生在建单事务内，页面只展示结果（核销明细是真源）。
	couponPages := NewCouponPageHandle(svc, projects)
	pages.GET("/coupons", couponPages.CouponsPage)
	pages.GET("/coupons/edit-form", builtin.CasbinMiddlewareForPathAs("/api/order/coupon/update", http.MethodPost), couponPages.CouponEditForm)
	pages.POST("/coupons/create", builtin.CasbinMiddlewareForPath("/api/order/coupon/create"), couponPages.CouponCreate)
	pages.POST("/coupons/update", builtin.CasbinMiddlewareForPath("/api/order/coupon/update"), couponPages.CouponUpdate)
	pages.POST("/coupons/delete", builtin.CasbinMiddlewareForPath("/api/order/coupon/delete"), couponPages.CouponDelete)
	// 批量删除 / 启停：启停走 /api/order/coupon/update（单条启停也走它）。
	pages.POST("/coupons/bulk-delete", builtin.CasbinMiddlewareForPath("/api/order/coupon/delete"), couponPages.CouponBulkDelete)
	pages.POST("/coupons/bulk-toggle", builtin.CasbinMiddlewareForPath("/api/order/coupon/update"), couponPages.CouponBulkToggle)
}
