package analyticshttp

// analytics_router.go — analytics 模块路由自装配。
//
// 公开打点直挂引擎（与 mail 的追踪端点、cart 的支付回调同一位置与同一理由）：
// 访客浏览器不会带后台会话与 CSRF token，把它挂进 /api 三层链只会得到 401/403。
// 后台只读聚合挂 authorizedAPI（Session + CSRF + Casbin，权限点 analytics:view）。

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsmodel "go_wp/internal/module/analytics/model"
	analyticsservice "go_wp/internal/module/analytics/service"
)

// SetupAnalyticsRoutes 装配访问统计模块并注册路由，返回模块契约。
//
// pepper 为匿名 hash 的盐（装配期传会话密钥）：IP 与访客标识只以带盐哈希落库。
// rg 为已挂 Session + CSRF + Casbin 的业务 API 组；router 为引擎（公开路由挂它）。
func SetupAnalyticsRoutes(rg *gin.RouterGroup, router *gin.Engine, db *gorm.DB,
	pepper string) analyticscontract.AnalyticsService {
	svc := analyticsservice.NewService(analyticsmodel.NewModel(db), pepper)
	handle := NewHandle(svc)

	// 公开打点（访问面）：只写一条浏览记录，没有查询与删除能力。
	if router != nil {
		router.POST("/analytics/collect", handle.Collect)
	}
	// 后台只读聚合（后台统计页与只读 API 共用同一份实现）。
	if rg != nil {
		g := rg.Group("/analytics")
		g.GET("/summary", handle.Summary)
	}
	return svc
}
