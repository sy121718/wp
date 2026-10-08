// masterdata_router.go — masterdata 模块的 **API** 装配（issue #19）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）；
// 后台「变更记录」页的注册在 masterdata_page_router.go。
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

// SetupMasterDataRoutes 装配主数据变更记录模块路由，返回模块契约。
//
// rg 是 API 组（前缀 /api，三层链），pages 是后台页面组（前缀 /admin，Session + CSRF +
// 权限上下文 / 侧栏菜单树由装配层统一挂在组上）—— pages 为 nil 时跳过页面注册，
// 与 rg 为 nil 的早退同构。
//
// project 用于把「未指定工程」解析为唯一工程（变更记录按工程隔离，project_id 为 NOT NULL 外键）。
//
// 返回的契约在装配期注入 product / inventory 两个模块：它们在写关键主数据时
// 把「改前 / 改后」的字段快照递进来（依赖方向 product / inventory → masterdata）。
func SetupMasterDataRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	project projectcontract.ProjectService) masterdatacontract.MasterDataService {
	svc := masterdataservice.NewService(masterdatamodel.NewModel(db), project)
	handle := NewHandle(svc)

	g := rg.Group("/masterdata")
	// 只读接口：变更记录由业务模块在写操作里追加，对外没有写入口。
	g.GET("/change/list", permission.MasterdataChangeList, handle.ListChanges)
	g.GET("/change/count", permission.MasterdataChangeCount, handle.CountChanges)
	g.GET("/change/entities", permission.MasterdataChangeEntities, handle.ListEntities)
	g.GET("/change/entity", permission.MasterdataChangeEntity, handle.EntityTimeline)

	// 后台「变更记录」页：注册在 masterdata_page_router.go（同一入口调用，装配顺序不变）。
	SetupMasterDataPages(pages, svc, project)

	return svc
}
