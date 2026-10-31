package masterdatahttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
)

func SetupMasterDataRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	project projectcontract.ProjectService) masterdatacontract.MasterDataService {
	svc := masterdataservice.NewService(masterdatamodel.NewModel(db), project)
	handle := NewHandle(svc)

	g := rg.Group("/masterdata")

	g.GET("/change/list", permission.MasterdataChangeList, handle.ListChanges)
	g.GET("/change/count", permission.MasterdataChangeCount, handle.CountChanges)
	g.GET("/change/entities", permission.MasterdataChangeEntities, handle.ListEntities)
	g.GET("/change/entity", permission.MasterdataChangeEntity, handle.EntityTimeline)

	SetupMasterDataPages(pages, svc, project)

	return svc
}
