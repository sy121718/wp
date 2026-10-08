package projecthttp

import (
	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

// SetupProjectRoutes 自装配 project 模块的 API 并注册路由，返回契约供 page/build 等模块依赖。
//
// 主题的 /api/theme/* 路由由 SetupThemeRoutes 注册（本文件下方）；后台页面
// （主题管理 / 站点设置 / 编辑器面板）在 project_page_router.go 的 SetupProjectPages，
// 依赖 page / block 契约，故与 API 分开、由装配层在契约齐备后调用。
func SetupProjectRoutes(rg *permission.RouteGroup, db *gorm.DB, dict sysconfigcontract.DictReader) projectcontract.ProjectService {
	model := projectmodel.NewProjectModel(db)
	svc := projectservice.NewService(model)
	handle := NewHandle(svc)
	// 语言 URL 方案不再做「启动恢复到进程级变量」：它按工程读
	// （projects.settings.langURLMode，唯一解析入口 pipeline.SiteLangURLModeOf），
	// 装配期没有需要恢复的进程状态。原先的 restoreLangURLMode 取「第一个配置了该键的
	// 工程」写进 pkg/i18n 的包级变量，是多工程数据污染的源头，已删除。

	SetupThemeRoutes(rg, db)
	g := rg.Group("/project", builtin.SessionAuthMiddleware())
	g.POST("/create", permission.ProjectCreate, handle.Create)
	g.GET("/list", permission.ProjectList, handle.List)
	g.GET("/detail", permission.ProjectDetail, handle.Detail)
	g.POST("/update", permission.ProjectUpdate, handle.Update)
	return svc
}

// SetupThemeRoutes 注册主题路由（挂 /api 前缀之下——与 035 seed 权限点 /api/theme/* 一致，内部再分 /theme 组）。
func SetupThemeRoutes(rg *permission.RouteGroup, db *gorm.DB) {
	model := projectmodel.NewProjectModel(db)
	svc := projectservice.NewService(model)
	h := &ThemeHandle{svc: svc}

	g := rg.Group("/theme", builtin.SessionAuthMiddleware())
	g.GET("/list", permission.ProjectThemeList, h.List)
	g.POST("/create", permission.ProjectThemeCreate, h.Create)
	g.POST("/update", permission.ProjectThemeUpdate, h.Update)
	g.POST("/activate", permission.ProjectThemeActivate, h.Activate)
	g.POST("/delete", permission.ProjectThemeDelete, h.Delete)
	g.GET("/active", permission.ProjectThemeActive, h.Active)
}
