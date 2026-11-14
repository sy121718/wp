package masterdatahttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
)

const MasterDataChangesTemplate = "admin/masterdata/masterdata_changes.html"

func SetupMasterDataPages(reg *mw.Registry, pages *gin.RouterGroup, svc masterdatacontract.MasterDataService,
	project projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	changePages := NewMasterDataChangePageHandle(svc, project)

	mw.DeclarePageGetOn(reg, "/admin/masterdata/changes", "/api/masterdata/change/list")
	pages.GET("/masterdata/changes",
		mw.PageAuthz("/api/masterdata/change/list"),
		pageview.Slot(MasterDataChangesTemplate, pageview.RenderPage),
		changePages.MasterDataChangesPage)
}
