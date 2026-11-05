package masterdatahttp

import (
	pageview "go_wp/pkg/view"
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
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
		adminui.PageAuthz("/api/masterdata/change/list"),
		pageview.Slot(MasterDataChangesTemplate, adminui.RenderPage),
		changePages.MasterDataChangesPage)
}
