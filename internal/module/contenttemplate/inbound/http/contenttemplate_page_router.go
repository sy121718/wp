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
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
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
// ContentTemplatesPageTemplate 内容模板列表整页的模板名（页面出口由 PageSlot 带进上下文）。
const ContentTemplatesPageTemplate = "admin/contenttemplate/content_templates.html"

func SetupContentTemplatePages(pages *gin.RouterGroup,
	templates contenttemplatecontract.ContentTemplateService,
	projects projectcontract.ProjectService, products productcontract.ProductService,
	contents contentcontract.ContentService) {
	if pages == nil {
		return
	}
	contentTemplatePages := newContentTemplatePageHandle(templates, projects, products, contents)
	// 页面出口走 adminui.PageSlot（模板名与渲染实现在装配点声明），handler 只调
	// pageview.Render —— 与 product / masterdata 同一形态（控制器不持模板名、不 import 渲染层）。
	pages.GET("/content-templates",
		adminui.PageAuthz("/api/contenttemplate/list"),
		adminui.PageSlot(ContentTemplatesPageTemplate),
		contentTemplatePages.ContentTemplatesPage)
	pages.GET("/content-templates/edit", adminui.PageAuthz("/api/contenttemplate/list"), contentTemplatePages.ContentTemplateEditPage)
}
