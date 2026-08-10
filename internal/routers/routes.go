package routers

import (
	"log"
	"net/http"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	datarulehttp "go_wp/internal/module/datarule/inbound/http"
	depthttp "go_wp/internal/module/dept/inbound/http"
	mediahttp "go_wp/internal/module/media/inbound/http"
	menuhttp "go_wp/internal/module/menu/inbound/http"
	permissionhttp "go_wp/internal/module/permission/inbound/http"
	permissionservice "go_wp/internal/module/permission/service"
	rolehttp "go_wp/internal/module/role/inbound/http"
	roleservice "go_wp/internal/module/role/service"
	"go_wp/internal/templates"
	"go_wp/pkg/database"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// SetupRoutes 注册全部路由。
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

	// 页面路由
	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/dashboard", gin.H{
			"title": "仪表盘",
			"menu":  "dashboard",
		})
	})
	router.GET("/admin", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/dashboard", gin.H{
			"title": "仪表盘",
			"menu":  "dashboard",
		})
	})
	router.GET("/builder", func(c *gin.Context) {
		c.HTML(http.StatusOK, "builder/builder", gin.H{})
	})

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

	perm := permissionhttp.SetupPermissionRoutes(api, db)
	menu := menuhttp.SetupMenuRoutes(api, db, perm.Service)

	if ps, ok := perm.Service.(*permissionservice.Service); ok {
		ps.SetMenuService(menu.Service)
	}

	role := rolehttp.SetupRoleRoutes(api, db, perm.Service, menu.Service)
	adminResult := adminhttp.SetupAdminRoutes(api, db, menu.Service, role.Service, perm.Service)

	if rs, ok := role.Service.(*roleservice.Service); ok {
		rs.SetAdminService(adminResult.Service)
	}

	dept := depthttp.SetupDeptRoutes(api, db, adminResult.Service)
	datarulehttp.SetupDataRuleRoutes(api, db, role.Service, dept)

	// 未匹配路由返回 404
	router.NoRoute(func(c *gin.Context) {
		response.NotFound(c, "请求的资源不存在")
	})
}