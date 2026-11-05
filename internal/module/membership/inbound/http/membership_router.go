package membershiphttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipmodel "go_wp/internal/module/membership/model"
	membershipservice "go_wp/internal/module/membership/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
	pageview "go_wp/pkg/view"
)

func SetupMembershipRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	projects projectcontract.ProjectService) membershipcontract.MembershipService {
	svc := membershipservice.NewService(membershipmodel.NewModel(db), projects)
	handle := NewHandle(svc)

	g := rg.Group("/membership")

	g.GET("/tier/list", permission.MembershipTierList, handle.ListTiers)
	g.GET("/tier/get", permission.MembershipTierGet, handle.GetTier)
	g.POST("/tier/create", permission.MembershipTierCreate, pageview.HTMLFragment, handle.CreateTier)
	g.POST("/tier/update", permission.MembershipTierUpdate, pageview.HTMLFragment, handle.UpdateTier)
	g.POST("/tier/delete", permission.MembershipTierDelete, pageview.HTMLFragment, handle.DeleteTier)
	g.POST("/entitlement/save", permission.MembershipEntitlementSave, pageview.HTMLFragment, handle.SaveEntitlements)

	g.GET("/assign/list", permission.MembershipAssignList, handle.ListAssignments)
	g.POST("/assign/set", permission.MembershipAssignSet, pageview.HTMLFragment, handle.AssignSet)
	g.POST("/assign/unlock", permission.MembershipAssignUnlock, pageview.HTMLFragment, handle.AssignUnlock)

	g.GET("/resolve", permission.MembershipResolve, handle.Resolve)

	g.POST("/recalc", permission.MembershipRecalc, pageview.HTMLFragment, handle.Recalc)

	SetupMembershipPages(pages, svc, projects)

	return svc
}
