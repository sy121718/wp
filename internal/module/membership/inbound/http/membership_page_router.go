package membershiphttp

// membership_page_router.go — 会员等级与归属页（/admin/membership*）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 membership_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面上的写动作走 pages 组 + builtin.CasbinMiddlewareForPath("/api/...")，**复用接口的权限点**：
// 权限点声明的真源是 membership_router.go 的 rg 注册动作，一个字符都不能改（路径换了而权限点没换，
// 结果是页面按钮人人可见但提交必 403，或者更糟 —— 提交成功但用的是另一个权限）。
// 页面 GET 的 Casbin 待补（见 docs/02-Z §4.3）。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	membershipcontract "go_wp/internal/module/membership/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

// SetupMembershipPages 注册会员等级与归属页；pages 为 nil 时整体跳过。
func SetupMembershipPages(pages *gin.RouterGroup, svc membershipcontract.MembershipService,
	projects projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	pageHandle := NewMembershipPageHandle(svc, projects)
	pages.GET("/membership", pageHandle.MembershipPage)
	pages.GET("/membership/assignments", pageHandle.MembershipAssignmentsPage)
	// 写动作复用接口权限点（真源在 membership_router.go 的 rg 注册动作）。
	// CasbinMiddlewareForPath 的参数就是那条 api 路由的路径，与注册动作算出来的绝对路径逐字一致。
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
