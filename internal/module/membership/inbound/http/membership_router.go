package membershiphttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	membershipservice "go_wp/internal/module/membership/service"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

func SetupMembershipRoutes(rg *mw.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	projects projectcontract.ProjectService) membershipcontract.MembershipService {
	svc := membershipservice.NewService(membershipmodel.NewModel(db), projects)
	handle := NewHandle(svc)

	g := rg.Group("/membership")

	g.GET("/tier/list", membershipenums.MembershipTierList, handle.ListTiers)
	g.GET("/tier/get", membershipenums.MembershipTierGet, handle.GetTier)
	g.POST("/tier/create", membershipenums.MembershipTierCreate, pageview.HTMLFragment, handle.CreateTier)
	g.POST("/tier/update", membershipenums.MembershipTierUpdate, pageview.HTMLFragment, handle.UpdateTier)
	g.POST("/tier/delete", membershipenums.MembershipTierDelete, pageview.HTMLFragment, handle.DeleteTier)
	g.POST("/entitlement/save", membershipenums.MembershipEntitlementSave, pageview.HTMLFragment, handle.SaveEntitlements)

	g.GET("/assign/list", membershipenums.MembershipAssignList, handle.ListAssignments)
	g.POST("/assign/set", membershipenums.MembershipAssignSet, pageview.HTMLFragment, handle.AssignSet)
	g.POST("/assign/unlock", membershipenums.MembershipAssignUnlock, pageview.HTMLFragment, handle.AssignUnlock)

	g.GET("/resolve", membershipenums.MembershipResolve, handle.Resolve)

	g.POST("/recalc", membershipenums.MembershipRecalc, pageview.HTMLFragment, handle.Recalc)

	SetupMembershipPages(rg.Registry(), pages, svc, projects)

	return svc
}
