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

func SetupMembershipRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	projects projectcontract.ProjectService) membershipcontract.MembershipService {
	svc := membershipservice.NewService(membershipmodel.NewModel(db), projects)
	handle := NewHandle(svc)

	g := rg.Group("/membership")

	g.GET("/tier/list", permission.MembershipTierList, handle.ListTiers)
	g.GET("/tier/get", permission.MembershipTierGet, handle.GetTier)
	g.POST("/tier/create", permission.MembershipTierCreate, handle.CreateTier)
	g.POST("/tier/update", permission.MembershipTierUpdate, handle.UpdateTier)
	g.POST("/tier/delete", permission.MembershipTierDelete, handle.DeleteTier)
	g.POST("/entitlement/save", permission.MembershipEntitlementSave, handle.SaveEntitlements)

	g.GET("/assign/list", permission.MembershipAssignList, handle.ListAssignments)
	g.POST("/assign/set", permission.MembershipAssignSet, handle.AssignSet)
	g.POST("/assign/unlock", permission.MembershipAssignUnlock, handle.AssignUnlock)

	g.GET("/resolve", permission.MembershipResolve, handle.Resolve)

	g.POST("/recalc", permission.MembershipRecalc, handle.Recalc)

	SetupMembershipPages(pages, svc, projects)

	return svc
}
