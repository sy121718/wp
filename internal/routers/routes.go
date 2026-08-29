package routers

import (
	"log"
	"net/http"
	"path/filepath"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	artifacthttp "go_wp/internal/module/artifact/inbound/http"
	blockhttp "go_wp/internal/module/block/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	mediahttp "go_wp/internal/module/media/inbound/http"
	pagehttp "go_wp/internal/module/page/inbound/http"
	projecthttp "go_wp/internal/module/project/inbound/http"
	pubhttp "go_wp/internal/module/publication/inbound/http"
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

	// 静态访问面：已发布站点直出激活产物（只读文件系统，零查库零模板）。
	// ActiveRoot 位于产物根下两级（{root}/public/active），符号链接目标相对可达。
	setupStaticFace(router)

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

	// 业务 API 路由（依赖顺序：media → project → block → artifact → publication → page）
	api := router.Group("/api")
	captcharouter.SetupCaptchaRoutes(api)
	mediaSvc := mediahttp.SetupMediaRoutes(api, db)
	projectService := projecthttp.SetupProjectRoutes(api, db)
	blockSvc := blockhttp.SetupBlockRoutes(api, db, projectService)
	artifactSvc := artifacthttp.SetupArtifactRoutes(api, db)
	publicationSvc := pubhttp.SetupPublicationRoutes(api, db)
	pageService := pagehttp.SetupPageRoutes(api, db, artifactSvc, publicationSvc, projectService, blockSvc, mediaSvc)
	adminhttp.SetupAdminRoutes(api, db)

	// 页面路由（编辑器外壳依赖 page/block/media 契约，置于 API 装配之后）
	dashboardhttp.SetupDashboardRoutes(router, pageService, projectService, blockSvc, mediaSvc)

	// 未匹配路由返回 404
	router.NoRoute(func(c *gin.Context) {
		response.NotFound(c, "请求的资源不存在")
	})
}

// setupStaticFace 挂载静态访问面（docs/03-pipeline.md §5）。
//
// ActiveRoot 位于产物根下两级（{root}/public/active），符号链接目标相对可达。
// 因此访问面根必须是 active 目录本身；文件由 StaticFS 只读直出，
// / 落到 index 入口。
// 注意必须无条件挂载：首次发布发生在启动之后，启动时目录必然不存在，
// 若按目录存在与否跳过挂载，静态访问面将永远无法生效（每次请求动态读盘，
// 目录与产物在首次发布后即时生效）。
func setupStaticFace(router *gin.Engine) {
	activeRoot := filepath.Join("public", "runtime", "artifacts", "public", "active")
	router.StaticFS("/site", http.Dir(activeRoot))
}
