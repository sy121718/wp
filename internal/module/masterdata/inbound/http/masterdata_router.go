// masterdata_router.go — masterdata 模块路由自装配（issue #19）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。
package masterdatahttp

import (
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	projectcontract "go_wp/internal/module/project/contract"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupMasterDataRoutes 装配主数据变更记录模块路由，返回模块契约。
//
// project 用于把「未指定工程」解析为唯一工程（变更记录按工程隔离，project_id 为 NOT NULL 外键）。
//
// 返回的契约在装配期注入 product / inventory 两个模块：它们在写关键主数据时
// 把「改前 / 改后」的字段快照递进来（依赖方向 product / inventory → masterdata）。
func SetupMasterDataRoutes(rg *gin.RouterGroup, db *gorm.DB,
	project projectcontract.ProjectService) masterdatacontract.MasterDataService {
	svc := masterdataservice.NewService(masterdatamodel.NewModel(db), project)
	handle := NewHandle(svc)

	g := rg.Group("/masterdata")
	// 只读接口：变更记录由业务模块在写操作里追加，对外没有写入口。
	g.GET("/change/list", handle.ListChanges)
	g.GET("/change/count", handle.CountChanges)
	g.GET("/change/entities", handle.ListEntities)
	g.GET("/change/entity", handle.EntityTimeline)
	return svc
}
