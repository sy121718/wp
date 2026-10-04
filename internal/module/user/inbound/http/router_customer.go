package userhttp

// router_customer.go — 后台客户管理页路由（/admin/customers*）。
//
// 为什么页面不在 SetupUserRoutes 里注册（是装配顺序，不是分层洁癖）：
// 客户详情页要读订单摘要（ordercontract.CustomerOrderSummaryReader），而订单模块装配在
// user **之后** —— 订单依赖 user 的访客开号端口。所以页面注册只能发生在订单契约就绪之后，
// 由装配层在订单模块装配完成后调用一次（此时 adminPages / userAdminSvc / orderSvc /
// projectService 都已就绪）。
//
// 页面挂在装配层传入的 /admin 组上：该组已有 Session + CSRF + 权限上下文中间件
// （见 internal/routers/assembly.go 的 adminPages）。写动作额外按**对应 API 的路径**
// 走 Casbin 权限点，与 /api/customer/* 完全同源，一个字符都不改。
// pages 为 nil 时跳过注册 —— 与 rg == nil 早退同构，装配不因缺少页面组而失败。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	membershipcontract "go_wp/internal/module/membership/contract"
	ordercontract "go_wp/internal/module/order/contract"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
)

// SetupCustomerPages 注册后台客户管理页（pages = /admin 后台页面组）。
//
// users 是 user 模块自己的后台面（装配层用 userSvc 断言 CustomerAdminPort 得到）；
// orders / projects 允许为 nil：页面据此降级渲染（详情页显式说明订单摘要不可用），
// 与页面里既有的 h.orders == nil / h.projects == nil 分支同口径，不 panic。
//
// membership / membershipFacing 是 BIZ-3 的会员展示端口（装配层从 membership 契约断言取），
// 同样允许为 nil —— 详情页的「会员等级」块渲染一句「会员模块尚未接入」而不是空白块。
// 二者成对传入并在同一个 setter 里落地：只接一半的中间态（读得到等级、错误却直出原文）
// 没有部署理由。
func SetupCustomerPages(pages *gin.RouterGroup,
	users usercontract.CustomerAdminPort,
	orders ordercontract.CustomerOrderSummaryReader,
	projects projectcontract.ProjectService,
	membership membershipcontract.Reader,
	membershipFacing membershipcontract.FacingTexter,
	growth ordercontract.CustomerGrowthReader,
	segments ordercontract.CustomerSegmentReader,
	rfm ordercontract.CustomerRfmReader,
	cohort ordercontract.CustomerCohortReader) {
	if pages == nil {
		return
	}
	// 客户管理页：后台此前没有任何地方读 users 表 —— 管理员看不到客户列表、不能按客户看订单、
	// 不能停用或解锁账号。页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；
	// 写动作走新权限点 user:customer_status / user:customer_unlock（迁移 152），
	// 与 /api/customer/* 那组接口是同一个权限点（页面的动作不另立一套授权）。
	h := NewCustomerPageHandle(users, orders, projects)
	// 会员展示端口（BIZ-3）：经 setter 注入而不是加进 NewCustomerPageHandle 的签名 ——
	// 那个构造函数有 10+ 处直调（含渲染测试），为一块展示改签名会把它们全卷进来。
	h.SetMembershipDisplay(membership, membershipFacing)
	h.SetCustomerGrowth(growth)
	h.SetCustomerSegments(segments)
	h.SetCustomerRfm(rfm)
	h.SetCustomerCohort(cohort)
	pages.GET("/customers", h.CustomersPage)
	// 客户概览：目录（客户）下的第一项。只读，走 /admin 组认证即可 ——
	// 与列表页同源（同一张 users 表的两种看法），不另立权限点。
	pages.GET("/customers/overview", h.CustomerOverviewPage)
	// RFM 分析：客户目录下的第三项。只读，与列表页同源（同一张表的另一种看法），
	// 不另立权限点 —— 与客户概览同一条口径。
	pages.GET("/customers/rfm", h.CustomerRfmPage)
	// 群组留存：客户目录下的第四项。同样只读、同样复用列表权限点 ——
	// 它是同一批客户按时间的另一种看法，不是另一份数据。
	pages.GET("/customers/cohort", h.CustomerCohortPage)
	pages.GET("/customers/detail", h.CustomerDetailPage)
	pages.POST("/customers/status",
		builtin.CasbinMiddlewareForPath("/api/customer/status"), h.CustomerStatusSave)
	pages.POST("/customers/unlock",
		builtin.CasbinMiddlewareForPath("/api/customer/unlock"), h.CustomerUnlock)
	// 批量动作**复用单条动作的权限点**（/api/customer/status、/api/customer/unlock）：
	// 同一条写入路径、同一个授权，页面只是把「一次一个」变成「一次一批」。
	// 不新增权限点也就不需要迁移 —— 多一个权限点只会多一处需要维护的授权真相。
	pages.POST("/customers/bulk-status",
		builtin.CasbinMiddlewareForPath("/api/customer/status"), h.CustomerBulkStatusSave)
	pages.POST("/customers/bulk-unlock",
		builtin.CasbinMiddlewareForPath("/api/customer/unlock"), h.CustomerBulkUnlock)
}
