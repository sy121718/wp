package membershiphttp

import (
	pageview "go_wp/pkg/view"
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	membershipcontract "go_wp/internal/module/membership/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

const (
	MembershipTiersPageTemplate       = "admin/membership/membership.html"
	MembershipAssignmentsPageTemplate = "admin/membership/membership_assignments.html"
)

func SetupMembershipPages(pages *gin.RouterGroup, svc membershipcontract.MembershipService,
	projects projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	pageHandle := NewMembershipPageHandle(svc, projects)
	pages.GET("/membership",
		adminui.PageAuthz("/api/membership/tier/list"),
		pageview.Slot(MembershipTiersPageTemplate, adminui.RenderPage),
		pageHandle.MembershipPage)
	pages.GET("/membership/assignments",
		adminui.PageAuthz("/api/membership/assign/list"),
		pageview.Slot(MembershipAssignmentsPageTemplate, adminui.RenderPage),
		pageHandle.MembershipAssignmentsPage)

}
