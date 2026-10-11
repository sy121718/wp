package membershiphttp

// membership_page_router.go — 会员等级与归属页（/admin/membership*）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 membership_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 写动作由模板直调 membership_router.go 注册的 JSON API。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
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
	pages.GET("/membership", adminui.PageAuthz("/api/membership/tier/list"), pageHandle.MembershipPage)
	pages.GET("/membership/assignments", adminui.PageAuthz("/api/membership/assign/list"), pageHandle.MembershipAssignmentsPage)

}
