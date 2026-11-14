package masterdatahttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
)

func SetupMasterDataRoutes(rg *mw.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	project projectcontract.ProjectService) masterdatacontract.MasterDataService {
	svc := masterdataservice.NewService(masterdatamodel.NewModel(db), project)
	handle := NewHandle(svc)

	g := rg.Group("/masterdata")

	g.GET("/change/list", masterdataenums.MasterdataChangeList, handle.ListChanges)
	g.GET("/change/count", masterdataenums.MasterdataChangeCount, handle.CountChanges)
	g.GET("/change/entities", masterdataenums.MasterdataChangeEntities, handle.ListEntities)
	g.GET("/change/entity", masterdataenums.MasterdataChangeEntity, handle.EntityTimeline)

	SetupMasterDataPages(rg.Registry(), pages, svc, project)

	return svc
}
