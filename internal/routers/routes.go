package routers

import (
	"log"
	"net/http"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	mediahttp "go_wp/internal/module/media/inbound/http"
	"go_wp/internal/templates"
	"go_wp/pkg/database"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// SetupRoutes 注册全部路由。
//
// 装配说明：管理面六领域（管理员/角色/权限/菜单/部门/数据权限）合并为 admin 大模块，
// 模块内部同包直调、自包含装配；media、dashboard、captcha 为独立模块。
func SetupRoutes(router *gin.Engine, ready func() error) {
	if router == nil {
		return
	}

	// Jet 模板渲染器（根目录 internal/templates，开发模式即时生效）
	router.HTMLRender = templates.NewJetHTMLRender("internal/templates", true)

	// 静态文件服务（admin CSS + builder JS/CSS 统一在此）
	router.Static("/static", "internal/templates/static")

	// 媒体上传存储（pkg/upload local provider 默认 public/storage）
	router.Static("/storage", "public/storage")

	// 后台页面路由（需要后端逻辑的页面入口，归 dashboard 模块）
	dashboardhttp.SetupDashboardRoutes(router)

	// 健康检查
	router.GET("/livez", func(c *gin.Context) {
		c.JSON(http.StatusOK, response.Response{
			Code:    http.StatusOK,
			Message: "ok",
			Data:    gin.H{"status": "alive"},
		})
	})

	router.GET("/readyz", func(c *gin.Context) {
		if ready != nil {
			if err := ready(); err != nil {
				c.JSON(http.StatusServiceUnavailable, response.Response{
					Code:    http.StatusServiceUnavailable,
					Message: err.Error(),
					Data:    gin.H{"status": "not_ready"},
				})
				return
			}
		}
		c.JSON(http.StatusOK, response.Response{
			Code:    http.StatusOK,
			Message: "ok",
			Data:    gin.H{"status": "ready"},
		})
	})

	// 通用依赖
	db, err := database.GetDB()
	if err != nil {
		log.Printf("数据库未就绪: %v", err)
		return
	}

	// 业务 API 路由
	api := router.Group("/api")
	captcharouter.SetupCaptchaRoutes(api)
	mediahttp.SetupMediaRoutes(api, db)
	adminhttp.SetupAdminRoutes(api, db)

	// 未匹配路由返回 404
	router.NoRoute(func(c *gin.Context) {
		response.NotFound(c, "请求的资源不存在")
	})
}