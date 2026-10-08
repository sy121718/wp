package userhttp

// user_page_router.go — 后台客户管理页（/admin/customers*）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 user_page.go 等文件。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面写动作额外按**对应 API 的路径**走 Casbin 权限点，与 /api/customer/* 完全同源，
// 一个字符都不改；页面 GET 的 Casbin 待补（见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3）。
//
// 为什么页面不并进 SetupUserRoutes：客户详情页要读订单摘要，而订单模块装配在 user
// **之后**（订单依赖 user 的访客开号端口），页面注册只能等订单契约就绪后由装配层调用。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/membership/contract"
	"go_wp/internal/module/order/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/user/contract"
	"go_wp/internal/shell"
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
	cohort ordercontract.CustomerCohortReader,
	membershipAdmin membershipcontract.AssignmentAdminPort,
	membershipTiers membershipcontract.TierAdminPort) {
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
	h.SetMembershipFilters(membershipAdmin, membershipTiers)
	pages.GET("/customers", shell.PageAuthz("/api/customer/list"), h.CustomersPage)
	// 客户概览：目录（客户）下的第一项。只读 —— 只读不等于不鉴权，菜单隐藏不是访问控制
	//（docs/02-Z §4.3）。与列表页同源（同一张 users 表的两种看法），借同一个读权限点。
	pages.GET("/customers/overview", shell.PageAuthz("/api/customer/list"), h.CustomerOverviewPage)
	// RFM 分析：客户目录下的第三项。与列表页同源（同一张表的另一种看法），同一个读权限点。
	pages.GET("/customers/rfm", shell.PageAuthz("/api/customer/list"), h.CustomerRfmPage)
	// 群组留存：客户目录下的第四项。同一批客户按时间的另一种看法，不是另一份数据。
	pages.GET("/customers/cohort", shell.PageAuthz("/api/customer/list"), h.CustomerCohortPage)
	// 客户详情：二级页面（菜单表没有它），但它是整页导航目标 —— 直输 URL 必须同样被拦。
	pages.GET("/customers/detail", shell.PageAuthz("/api/customer/list"), h.CustomerDetailPage)
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
