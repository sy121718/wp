package mailhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	mailcontact "go_wp/internal/module/mail/contract"
	mailenums "go_wp/internal/module/mail/enums"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

const (
	CampaignsPageTemplate = "admin/mail/mail_campaigns.html"

	AccountsPageTemplate       = "admin/mail/mail.html"
	TemplatesPageTemplate      = "admin/mail/mail_templates.html"
	ContactsPageTemplate       = "admin/mail/mail_contacts.html"
	AutomationRunsPageTemplate = "admin/mail/mail_automation_runs.html"
	AutomationCanvasTemplate   = "admin/mail/mail_automation_canvas.html"
	AutomationRunPageTemplate  = "admin/mail/mail_automation_run.html"
	CampaignPageTemplate       = "admin/mail/mail_campaign.html"
	TemplateEditorPageTemplate = "admin/mail/mail_template_editor.html"
	AutomationPageTemplate     = "admin/mail/mail_automation.html"
)

// setupMailPageRoutes 装配邮件后台页面与 HTML 写入口。
//
// 除了挂路由，它还把每个页面/写入口登记到本次装配的权限索引：页面与其复用的
// API 权限对象都要能在启动核对里被看到，不能只靠 PageAuthz/Casbin 中间件存在。
func setupMailPageRoutes(reg *mw.Registry, pages *gin.RouterGroup, svc mailcontact.MailService) {
	if pages == nil || svc == nil {
		return
	}
	// declare 登记的是**真实入口**：pages 挂在应用装配的 /admin 组下，
	// 组内相对路径 /mail 对应的运行时路由是 /admin/mail。登记若写成 /mail，
	// 启动核对会拿一个不存在的路径去比目录，页面事实上没有进入同源登记。
	declare := func(method, path, perm, authObject string) {
		if reg == nil {
			return
		}
		reg.DeclareRoute(mw.RouteSpec{
			Method: method, Path: "/admin" + path, Perm: perm, Kind: mw.RoutePage,
			AuthObject: authObject, AuthMethod: method,
		})
	}
	mailPage := NewMailPageHandle(svc)

	pages.GET("/mail",
		mw.PageAuthz("/api/mail/account/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailPage)
	declare(http.MethodGet, "/mail", mailenums.MailAccountList, "/api/mail/account/list")
	pages.POST("/mail/account/save", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/save"), mailPage.MailAccountSave)
	declare(http.MethodPost, "/mail/account/save", mailenums.MailAccountSave, "/api/mail/account/save")
	pages.POST("/mail/account/delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountDelete)
	declare(http.MethodPost, "/mail/account/delete", mailenums.MailAccountDelete, "/api/mail/account/delete")
	pages.POST("/mail/account/default", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/default"), mailPage.MailAccountDefault)
	declare(http.MethodPost, "/mail/account/default", mailenums.MailAccountDefault, "/api/mail/account/default")
	pages.POST("/mail/account/test", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/test"), mailPage.MailAccountTest)
	declare(http.MethodPost, "/mail/account/test", mailenums.MailAccountTest, "/api/mail/account/test")

	pages.GET("/mail/templates",
		mw.PageAuthz("/api/mail/template/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailTemplatesPage)
	declare(http.MethodGet, "/mail/templates", mailenums.MailTemplateList, "/api/mail/template/list")

	pages.GET("/mail/templates/editor",
		mw.PageAuthz("/api/mail/template/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailTemplateEditor)
	declare(http.MethodGet, "/mail/templates/editor", mailenums.MailTemplateList, "/api/mail/template/list")
	pages.POST("/mail/template/save", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/template/save"), mailPage.MailTemplateSave)
	declare(http.MethodPost, "/mail/template/save", mailenums.MailTemplateSave, "/api/mail/template/save")
	pages.POST("/mail/template/delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplateDelete)
	declare(http.MethodPost, "/mail/template/delete", mailenums.MailTemplateDelete, "/api/mail/template/delete")

	pages.POST("/mail/accounts/bulk-delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountsBulkDelete)
	declare(http.MethodPost, "/mail/accounts/bulk-delete", mailenums.MailAccountDelete, "/api/mail/account/delete")
	pages.POST("/mail/templates/bulk-delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplatesBulkDelete)
	declare(http.MethodPost, "/mail/templates/bulk-delete", mailenums.MailTemplateDelete, "/api/mail/template/delete")

	pages.GET("/mail/contacts",
		mw.PageAuthz("/api/mail/contact/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailContactsPage)
	declare(http.MethodGet, "/mail/contacts", mailenums.MailContactList, "/api/mail/contact/list")
	pages.POST("/mail/contact/import", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/contact/import"), mailPage.MailContactImport)
	declare(http.MethodPost, "/mail/contact/import", mailenums.MailContactImport, "/api/mail/contact/import")
	pages.POST("/mail/contact/status", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactStatus)
	declare(http.MethodPost, "/mail/contact/status", mailenums.MailContactStatus, "/api/mail/contact/status")
	pages.POST("/mail/contacts/bulk-status", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactsBulkStatus)
	declare(http.MethodPost, "/mail/contacts/bulk-status", mailenums.MailContactStatus, "/api/mail/contact/status")

	pages.GET("/mail/campaigns",
		mw.PageAuthz("/api/mail/campaign/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailCampaignsPage)
	declare(http.MethodGet, "/mail/campaigns", mailenums.MailCampaignList, "/api/mail/campaign/list")
	pages.POST("/mail/campaign/save", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/campaign/save"), mailPage.MailCampaignSave)
	declare(http.MethodPost, "/mail/campaign/save", mailenums.MailCampaignSave, "/api/mail/campaign/save")
	pages.POST("/mail/campaign/start", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/campaign/start"), mailPage.MailCampaignStart)
	declare(http.MethodPost, "/mail/campaign/start", mailenums.MailCampaignStart, "/api/mail/campaign/start")
	pages.POST("/mail/campaign/delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignDelete)
	declare(http.MethodPost, "/mail/campaign/delete", mailenums.MailCampaignDelete, "/api/mail/campaign/delete")
	pages.POST("/mail/campaigns/bulk-delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignsBulkDelete)
	declare(http.MethodPost, "/mail/campaigns/bulk-delete", mailenums.MailCampaignDelete, "/api/mail/campaign/delete")

	pages.GET("/mail/campaign",
		mw.PageAuthz("/api/mail/campaign/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailCampaignPage)
	declare(http.MethodGet, "/mail/campaign", mailenums.MailCampaignList, "/api/mail/campaign/list")

	// 历史入口 /admin/mail/marketing 跳到联系人页：鉴权对象跟目标页一致（联系人列表），
	// 不能只靠组级会话/CSRF 就留下一条未登记的页面入口。
	pages.GET("/mail/marketing",
		mw.PageAuthz("/api/mail/contact/list"),
		mailPage.MailMarketingRedirect)
	declare(http.MethodGet, "/mail/marketing", mailenums.MailContactList, "/api/mail/contact/list")

	pages.GET("/mail/automation",
		mw.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailAutomationPage)
	declare(http.MethodGet, "/mail/automation", mailenums.MailAutomationList, "/api/mail/automation/list")

	pages.GET("/mail/automation/runs",
		mw.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailAutomationRunsPage)
	declare(http.MethodGet, "/mail/automation/runs", mailenums.MailAutomationList, "/api/mail/automation/list")
	pages.GET("/mail/automation/run",
		mw.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailAutomationRunDetail)
	declare(http.MethodGet, "/mail/automation/run", mailenums.MailAutomationList, "/api/mail/automation/list")

	pages.GET("/mail/automation/canvas",
		mw.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(pageview.RenderPage),
		mailPage.MailAutomationCanvas)
	declare(http.MethodGet, "/mail/automation/canvas", mailenums.MailAutomationList, "/api/mail/automation/list")
	pages.POST("/mail/automation/status", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/automation/status"), mailPage.MailAutomationStatus)
	declare(http.MethodPost, "/mail/automation/status", mailenums.MailAutomationStatus, "/api/mail/automation/status")
	pages.POST("/mail/automation/delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationDelete)
	declare(http.MethodPost, "/mail/automation/delete", mailenums.MailAutomationDelete, "/api/mail/automation/delete")
	pages.POST("/mail/automations/bulk-delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationsBulkDelete)
	declare(http.MethodPost, "/mail/automations/bulk-delete", mailenums.MailAutomationDelete, "/api/mail/automation/delete")
	pages.POST("/mail/automation/tick", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/automation/tick"), mailPage.MailAutomationTick)
	declare(http.MethodPost, "/mail/automation/tick", mailenums.MailAutomationTick, "/api/mail/automation/tick")
}
