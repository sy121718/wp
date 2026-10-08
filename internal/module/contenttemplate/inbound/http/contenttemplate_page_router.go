package contenttemplatehttp

// contenttemplate_page_router.go — 内容模板后台页面的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 contenttemplate_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面 GET 的 Casbin 待补（见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3）；
// 写动作已按 /api/contenttemplate/* 的权限点 enforce。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/content/contract"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
	"go_wp/internal/shell"
)

// SetupContentTemplatePages 注册内容模板后台页面（/admin 组，中间件链由装配层统一挂好）。
//
// 为什么不像其它模块那样在 SetupContentTemplateRoutes 里顺带注册：列表页要按实体类型
// 取样例实体（product / article），而 product 契约与 content 契约里属于「商品列表」的那部分
// **晚于 contenttemplate 装配**（contenttemplate 在 content 之后、product 之前）。
// 装配层在全部契约就绪后调用本函数，装配点与 dashboard 页面并排。
//
// pages 为 nil 时整体跳过（与既有 rg == nil 的早退同构）。
func SetupContentTemplatePages(pages *gin.RouterGroup,
	templates contenttemplatecontract.ContentTemplateService,
	projects projectcontract.ProjectService, products productcontract.ProductService,
	contents contentcontract.ContentService) {
	if pages == nil {
		return
	}
	contentTemplatePages := newContentTemplatePageHandle(templates, projects, products, contents)
	pages.GET("/content-templates", shell.PageAuthz("/api/contenttemplate/list"), contentTemplatePages.ContentTemplatesPage)
	pages.GET("/content-templates/edit", shell.PageAuthz("/api/contenttemplate/list"), contentTemplatePages.ContentTemplateEditPage)
	// 切换生效：权限点 contenttemplate:activate（迁移 288 seed）。必须用独立权限点映射 ——
	// Casbin 中间件按**实际请求路径** enforce，复用 update 时这条路径没有策略匹配 → 全员 403
	//（含超管）。这里把页面入口映射到 JSON 接口的授权路径，两个入口共用同一条策略。
	pages.POST("/content-templates/activate",
		builtin.CasbinMiddlewareForPath("/api/contenttemplate/activate"),
		contentTemplatePages.ContentTemplatesActivate)
	// 批量删除：权限点 contenttemplate:delete（迁移 234 seed）。本模块此前没有任何删除能力，
	// 这条权限点与 contract 的 Delete、路由的 enforce 路径同批补上。
	//
	// /api/contenttemplate/delete 没有对应的 API 路由（删除只有页面入口），必须显式声明 ——
	// 否则 permission.RoutesOf 查不到它，AI 工具按 fail closed 一律 forbidden。
	permission.Declare(http.MethodPost, "/api/contenttemplate/delete", permission.ContenttemplateDelete)
	pages.POST("/content-templates/bulk-delete",
		builtin.CasbinMiddlewareForPath("/api/contenttemplate/delete"),
		contentTemplatePages.ContentTemplatesBulkDelete)
}
