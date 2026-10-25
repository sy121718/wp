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

	"go_wp/internal/adminui"
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

// 发货三屏的模板名（迁移 624 / 625，S3）：与路由同处 —— 失败与成功渲染的必须是同一张模板。
const (
	PendingShipPageTemplate   = "admin/order/order_pending_ship.html"
	ShipFormPageTemplate      = "admin/order/order_ship_form.html"
	OrderCarriersPageTemplate = "admin/order/order_carriers.html"
)

// SetupOrderPages 注册订单域后台页面；pages 为 nil 时整体跳过。
// OrderNewPageTemplate 订单新建页的模板名。
const OrderNewPageTemplate = "admin/order/order_new.html"

// ReturnListPageTemplate 退货列表页的模板名。
const ReturnListPageTemplate = "admin/order/returns.html"

// CouponListPageTemplate 优惠码列表页的模板名。
const CouponListPageTemplate = "admin/order/coupons.html"

// CouponEditFormTemplate 优惠码编辑抽屉片段（片段出口，不带页壳）。
const CouponEditFormTemplate = "admin/order/coupon_edit_form.html"

// OrderListPageTemplate 订单列表页的模板名（页面出口由页面槽带进上下文）。
const OrderListPageTemplate = "admin/order/orders.html"

// OrderSalesPageTemplate 销售概览页的模板名（页面出口由页面槽带进上下文）。
const OrderSalesPageTemplate = "admin/order/sales_overview.html"

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
	pages.GET("/orders",
		adminui.PageAuthz("/api/order/list"),
		adminui.PageSlot(OrderListPageTemplate),
		orderPages.OrdersPage)
	// 销售概览页（GET /admin/orders/overview）：整页只读报表 —— 只读不等于不鉴权，
	// 菜单隐藏不是访问控制（docs/02-Z §4.3）。obj 取该页菜单绑的码 order:overview 声明的
	// api_path，而它恰好就是这条页面路径本身（**存量特例**：绝大多数页面的 obj 是 `/api/*`
	// 形态，见 internal/middleware/builtin/page_authz.go 的说明），归一与 02-Z 第二刀同批。
	salesPages := NewOrderSalesPageHandle(svc, projects)
	pages.GET("/orders/overview",
		adminui.PageAuthz("/admin/orders/overview"),
		adminui.PageSlot(OrderSalesPageTemplate),
		salesPages.SalesOverviewPage)
	// 这个权限点同时仍是可授权对象：声明它不只是为了拦请求 —— 侧栏菜单绑 order:overview，
	// 角色分权经 menu_ids 收集到它，
	// 而 permission.RoutesOf 也才有非空结果（空集会被工具侧按 fail closed 一律 forbidden）。
	// 声明放在这里（与路由同一处），启动期 SyncToDB 幂等 upsert 进 sys_permission，
	// **不写权限点 seed 迁移**（AGENTS.md §数据库：新增权限点加常量 + 在路由注册处声明）。
	permission.Declare(http.MethodGet, "/admin/orders/overview", permission.OrderOverview)
	// 后台代客建单页（docs/02-W-admin-order-create.md）：独立整页，页头与空态两个入口
	// 都指向它（同一个 URL）。写动作复用 order:create —— 与上面几条同手法，
	// **不新增权限点、不写 seed 迁移**；权限点路径一个字符都不能改。
	//
	// 这一页是整页表单（字段 20+、明细可多行）：**成功**走 303 PRG 回列表（Location 带
	// orderId，那一单的详情是展开的），**失败**一律就地重渲 200 + 字段标红 + 回填 ——
	// 失败不能走 303：303 之后是 GET，没有 PostForm，几十个字段必然全丢。
	orderCreate := NewOrderCreatePageHandle(svc, projects, product, dict)
	pages.GET("/orders/new",
		adminui.PageAuthz("/api/order/list"),
		adminui.PageSlot(OrderNewPageTemplate),
		orderCreate.OrderCreatePage)
	// POST 也挂 PageSlot：失败时回的是**同一张建单页**（200 + 回填），
	// 成功时导航由前端 data-write-nav 决定（见 docs/02-W §6）。
	pages.POST("/orders/create",
		builtin.CasbinMiddlewareForPath("/api/order/create"),
		adminui.PageSlot(OrderNewPageTemplate),
		orderCreate.OrderCreateSubmit)
	// 列表页的写动作（流转 / 取消 / 退款 / 备注 与两个批量动作）已整批迁到前端直调 JSON：
	// 单条走 order_router.go 里的 /api/order/{status,cancel,refund,note}，批量走
	// /api/order/{status,cancel} 的**批档形状**（体里有 ids 即批档，判别键是形状不是 action）
	// —— 页面不再有自己的写路径，于是也**不再需要**「页面路径借 API 权限点」那层
	// （那些 CasbinMiddlewareForPath("/api/...") 只是把 API path 字符串拿来当 Casbin obj 用）。
	//
	// 不另开 /api/order/bulk-* 的理由：sys_permission 每个权限点只有一列 api_path/api_method，
	// 再开路径就必须新增权限点，而「新增权限点 = 已授权角色静默缩权」是本仓有案可查的事故
	// （见 order_bulk_write.go 头注释）。

	// 退货入库（RMA）：客户在访问面提交申请，后台在这里审核与收货。
	// **先入库、后退款**的强顺序由 service 的 ReceiveReturn 保证。
	// 审核 / 拒绝 / 收货与两个批量动作已迁到前端直调 JSON（task-98 S3）：端点就是
	// /api/order/return/{approve,reject,receive}，批量靠请求体形状分流（见 return_bulk_write.go），
	// 页面不再有自己的写路径 —— 于是也**不再需要**「页面路径借 API 权限点」那层
	// （那些 CasbinMiddlewareForPath 只是把 API path 字符串拿来当 Casbin obj 用）。
	returnPages := NewReturnPageHandle(svc, projects, warehouses, dict)
	pages.GET("/returns",
		adminui.PageAuthz("/api/order/return/list"),
		adminui.PageSlot(ReturnListPageTemplate),
		returnPages.ReturnsPage)

	// 优惠码管理页：只剩两个 GET —— 列表与编辑抽屉片段。核销**没有手工入口** —— 它发生在
	// 建单事务内，页面只展示结果（核销明细是真源）。
	//
	// 写动作（新建 / 修改含停用启用 / 删除 / 批量删除 / 批量启停）已迁到前端直调 JSON：
	// 端点就是 order_router.go 里的 /api/order/coupon/{create,update,delete}，页面不再有
	// 自己的写路径 —— 于是也**不再需要**「页面路径借 API 权限点」那层（那些
	// CasbinMiddlewareForPath("/api/...") 只是把 API path 字符串拿来当 Casbin obj 用）。
	//
	// 编辑抽屉仍是 GET：片段里的表单自己声明 data-write-api 端点（前端发 JSON），
	// 权限照旧按「能改券」判 —— 看不到 update 权限的人不该拿到这张表单。
	// 发货三屏（迁移 624 / 625，S3）：
	//   · 待发货页 —— 只读列表（「发货」按钮打开下面的抽屉片段）；
	//   · 发货表单 —— 抽屉片段（GET），表单提交走 JSON /api/order/shipment/create；
	//   · 承运商页 —— 只读列表 + 新建/编辑抽屉，写动作走 /api/order/carrier/*。
	//
	// 三个页面的写路径都不在 /admin 组上：JSON 端点本来就是给前端调的，页面再挂一遍
	// 就等于把同一份授权写第二处（sys_permission 每个权限点只有一列 api_path）。
	shipPages := NewOrderShipmentPageHandle(svc, projects)
	pages.GET("/orders/pending-ship", adminui.PageAuthz("/api/order/shipment/pending"),
		adminui.PageSlot(PendingShipPageTemplate), shipPages.PendingShipPage)
	// 发货抽屉片段：权限点取「能发货」那条（看不到它的人不该拿到这张表单）。
	pages.GET("/orders/ship-form", builtin.CasbinMiddlewareForPathAs("/api/order/shipment/create", http.MethodPost),
		adminui.BareSlot(ShipFormPageTemplate), shipPages.ShipFormFragment)
	pages.GET("/order-carriers", adminui.PageAuthz("/api/order/carrier/list"),
		adminui.PageSlot(OrderCarriersPageTemplate), shipPages.OrderCarriersPage)
	// 模板名与路由同处（失败与成功渲染的必须是同一张模板）。
	permission.Declare(http.MethodGet, "/admin/orders/pending-ship", permission.OrderShipmentList)
	permission.Declare(http.MethodGet, "/admin/order-carriers", permission.OrderCarrierManage)

	couponPages := NewCouponPageHandle(svc, projects)
	pages.GET("/coupons",
		adminui.PageAuthz("/api/order/coupon/list"),
		adminui.PageSlot(CouponListPageTemplate),
		couponPages.CouponsPage)
	pages.GET("/coupons/edit-form",
		builtin.CasbinMiddlewareForPathAs("/api/order/coupon/update", http.MethodPost),
		adminui.BareSlot(CouponEditFormTemplate),
		couponPages.CouponEditForm)
}
