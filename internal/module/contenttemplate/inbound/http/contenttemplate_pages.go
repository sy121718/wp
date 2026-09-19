package contenttemplatehttp

// contenttemplate_pages.go — 内容模板后台页路由（EDT-001）。
//
// 模板 Document 的可视化编辑走 /workbench?template=…（复用页面工作台）；
// 这里只注册后台列表页与带样例实体参数的编辑跳转。
//
// 权限：页面 GET 只走 /admin 组的 Session + CSRF（与其它后台页面一致，
// 模板列表本身不构成新的信息公开面，也不写任何东西）。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
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
	pages.GET("/content-templates", contentTemplatePages.ContentTemplatesPage)
	pages.GET("/content-templates/edit", contentTemplatePages.ContentTemplateEditPage)
	// 切换生效：权限点 contenttemplate:activate（迁移 288 seed）。必须用独立权限点映射 ——
	// Casbin 中间件按**实际请求路径** enforce，复用 update 时这条路径没有策略匹配 → 全员 403
	//（含超管）。这里把页面入口映射到 JSON 接口的授权路径，两个入口共用同一条策略。
	pages.POST("/content-templates/activate",
		builtin.CasbinMiddlewareForPath("/api/contenttemplate/activate"),
		contentTemplatePages.ContentTemplatesActivate)
	// 批量删除：权限点 contenttemplate:delete（迁移 234 seed）。本模块此前没有任何删除能力，
	// 这条权限点与 contract 的 Delete、路由的 enforce 路径同批补上。
	pages.POST("/content-templates/bulk-delete",
		builtin.CasbinMiddlewareForPath("/api/contenttemplate/delete"),
		contentTemplatePages.ContentTemplatesBulkDelete)
}
