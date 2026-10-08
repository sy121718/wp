// membership_router.go — membership 模块的 **API** 装配（BIZ-3），挂 authorizedAPI
// 三层链（SessionAuth + CSRF + Casbin）。
//
// 后台页面（/admin/membership*）的注册在 membership_page_router.go，由本文件的
// SetupMembershipRoutes 在同一位置调用 —— 落点分开、装配顺序不变。
package membershiphttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

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

	// 后台页面：注册在 membership_page_router.go（同一入口调用，装配顺序不变）。
	SetupMembershipPages(pages, svc, projects)

	return svc
}
