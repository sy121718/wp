package masterdatahttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"

	mw "go_wp/pkg/middleware"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

const MasterDataChangesTemplate = "admin/masterdata/masterdata_changes.html"

func SetupMasterDataPages(pages *gin.RouterGroup, svc masterdatacontract.MasterDataService,
	project projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	changePages := NewMasterDataChangePageHandle(svc, project)

	pages.GET("/masterdata/changes",
		mw.PageAuthz("/api/masterdata/change/list"),
		pageview.Slot(MasterDataChangesTemplate, pageview.RenderPage),
		changePages.MasterDataChangesPage)
}
