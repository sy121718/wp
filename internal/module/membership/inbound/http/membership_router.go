// membership_router.go — membership 模块路由自装配（BIZ-3）。
//
// 两组路由：
//
//	· rg（authorizedAPI，前缀 /api，三层链 SessionAuth + CSRF + Casbin）—— JSON 接口；
//	· pages（后台页面组，前缀 /admin，Session + CSRF + 权限上下文由装配层统一挂）—— 整页渲染。
//
// 页面上的写动作走 pages 组 + builtin.CasbinMiddlewareForPath("/api/...")，**复用接口的权限点**：
// 权限点声明的真源是 rg 那边的注册动作，一个字符都不能改（路径换了而权限点没换，
// 结果是页面按钮人人可见但提交必 403，或者更糟 —— 提交成功但用的是另一个权限）。
package membershiphttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/middleware/builtin"
	membershipcontract "go_wp/internal/module/membership/contract"
	membershipmodel "go_wp/internal/module/membership/model"
	membershipservice "go_wp/internal/module/membership/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
)

// SetupMembershipRoutes 装配 membership 模块路由，返回模块契约（BIZ-3）。
//
// rg 为 nil 时早退（与本仓其它模块的 Setup 同构）；pages 为 nil 时跳过页面注册。
// projects 是站点工程契约：等级按工程建键，工程存在性校验与日结遍历工程清单都要它。
//
// **不在此处启动日结调度器**：消费额批量端口（membershipcontract.PurchaseSource）由订单侧
// 在装配期后置注入，装配层在注入之后调 svc.StartRecalcScheduler()。在这里启动的话，
// 端口必然还没注入，调度器会被永久禁用（启动日志里只有一条 Warn，之后没人再调它）。
func SetupMembershipRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	projects projectcontract.ProjectService) membershipcontract.MembershipService {
	svc := membershipservice.NewService(membershipmodel.NewModel(db), projects)
	handle := NewHandle(svc)

	g := rg.Group("/membership")
	// 等级与权益（后台配置面）。
	g.GET("/tier/list", permission.MembershipTierList, handle.ListTiers)
	g.GET("/tier/get", permission.MembershipTierGet, handle.GetTier)
	g.POST("/tier/create", permission.MembershipTierCreate, handle.CreateTier)
	g.POST("/tier/update", permission.MembershipTierUpdate, handle.UpdateTier)
	g.POST("/tier/delete", permission.MembershipTierDelete, handle.DeleteTier)
	g.POST("/entitlement/save", permission.MembershipEntitlementSave, handle.SaveEntitlements)
	// 归属（手工面）。
	g.GET("/assign/list", permission.MembershipAssignList, handle.ListAssignments)
	g.POST("/assign/set", permission.MembershipAssignSet, handle.AssignSet)
	g.POST("/assign/unlock", permission.MembershipAssignUnlock, handle.AssignUnlock)
	// 会员身份解析（消费侧读接口；order / cart / runtimefragment 都走契约，这个接口是给它
	// 做联调与排障用的 —— 有了它，问「这个人在这个工程是什么等级」不必打开数据库）。
	g.GET("/resolve", permission.MembershipResolve, handle.Resolve)
	// 立即重算（排障口）：端口未接入时返回 503 + 归口文案，不是「Scanned = 0 的成功」。
	g.POST("/recalc", permission.MembershipRecalc, handle.Recalc)

	if pages != nil {
		pageHandle := NewMembershipPageHandle(svc, projects)
		pages.GET("/membership", pageHandle.MembershipPage)
		pages.GET("/membership/assignments", pageHandle.MembershipAssignmentsPage)
		// 写动作复用接口权限点（真源在上一段）。CasbinMiddlewareForPath 的参数
		// 就是那条 api 路由的路径，与注册动作算出来的绝对路径逐字一致。
		pages.POST("/membership/tier/create",
			builtin.CasbinMiddlewareForPath("/api/membership/tier/create"), pageHandle.TierCreate)
		pages.POST("/membership/tier/update",
			builtin.CasbinMiddlewareForPath("/api/membership/tier/update"), pageHandle.TierUpdate)
		pages.POST("/membership/tier/default",
			builtin.CasbinMiddlewareForPath("/api/membership/tier/update"), pageHandle.TierDefault)
		pages.POST("/membership/tier/delete",
			builtin.CasbinMiddlewareForPath("/api/membership/tier/delete"), pageHandle.TierDelete)
		pages.POST("/membership/entitlement/save",
			builtin.CasbinMiddlewareForPath("/api/membership/entitlement/save"), pageHandle.EntitlementSave)
		pages.POST("/membership/assign/set",
			builtin.CasbinMiddlewareForPath("/api/membership/assign/set"), pageHandle.AssignSet)
		pages.POST("/membership/assign/unlock",
			builtin.CasbinMiddlewareForPath("/api/membership/assign/unlock"), pageHandle.AssignUnlock)
	}

	return svc
}
