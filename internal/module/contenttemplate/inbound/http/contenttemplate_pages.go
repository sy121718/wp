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
}
