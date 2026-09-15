package buildhttp

// build_router.go — build 模块路由自装配。
//
// 挂在 Session 组（与 /page 组同形）：这一组是后台运维接口，
// 读的是队列深度与失败原因、写的是「把失败任务退回队列」——
// 没有跨工程的越权面（队列行只带来源模块的 id，不含业务数据）。

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/middleware/builtin"

	buildcontract "go_wp/internal/module/build/contract"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
)

// SetupBuildRoutes 装配构建任务队列并注册路由，返回模块契约。
//
// worker 不在这里启动：执行器要由来源模块（page / presentation）在装配后注册，
// 先启动 worker 会有一小段「任务没有执行器」的窗口，那段时间进来的任务会被判失败。
// 启动时机由顶层装配决定（见 routers.SetupRoutes）。
func SetupBuildRoutes(rg *gin.RouterGroup, db *gorm.DB) buildcontract.BuildService {
	svc := buildservice.NewService(buildmodel.NewModel(db))
	handle := NewHandle(svc)

	g := rg.Group("/build", builtin.SessionAuthMiddleware())
	g.GET("/queue", handle.Queue)
	g.GET("/jobs", handle.List)
	g.POST("/retry", handle.Retry)
	return svc
}
