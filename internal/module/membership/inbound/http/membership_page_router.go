package membershiphttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"

	membershipcontract "go_wp/internal/module/membership/contract"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
)

const (
	MembershipTiersPageTemplate       = "admin/membership/membership.html"
	MembershipAssignmentsPageTemplate = "admin/membership/membership_assignments.html"
)

func SetupMembershipPages(reg *mw.Registry, pages *gin.RouterGroup, svc membershipcontract.MembershipService,
	projects projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	pageHandle := NewMembershipPageHandle(svc, projects)
	pages.GET("/membership",
		mw.PageAuthz("/api/membership/tier/list"),
		pageview.Slot(MembershipTiersPageTemplate, pageview.RenderPage),
		pageHandle.MembershipPage)
	mw.DeclarePageGetOn(reg, "/admin/membership", "/api/membership/tier/list")
	pages.GET("/membership/assignments",
		mw.PageAuthz("/api/membership/assign/list"),
		pageview.Slot(MembershipAssignmentsPageTemplate, pageview.RenderPage),
		pageHandle.MembershipAssignmentsPage)
	mw.DeclarePageGetOn(reg, "/admin/membership/assignments", "/api/membership/assign/list")

}
