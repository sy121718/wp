package mailhttp

import (
	"github.com/gin-gonic/gin"

	mw "go_wp/pkg/middleware"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	mailcontract "go_wp/internal/module/mail/contract"
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

func setupMailPageRoutes(pages *gin.RouterGroup, svc mailcontract.MailService) {
	if pages == nil || svc == nil {
		return
	}
	mailPage := NewMailPageHandle(svc)

	pages.GET("/mail",
		mw.PageAuthz("/api/mail/account/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailPage)
	pages.POST("/mail/account/save", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/save"), mailPage.MailAccountSave)
	pages.POST("/mail/account/delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountDelete)
	pages.POST("/mail/account/default", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/default"), mailPage.MailAccountDefault)
	pages.POST("/mail/account/test", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/test"), mailPage.MailAccountTest)

	pages.GET("/mail/templates",
		mw.PageAuthz("/api/mail/template/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailTemplatesPage)

	pages.GET("/mail/templates/editor",
		mw.PageAuthz("/api/mail/template/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailTemplateEditor)
	pages.POST("/mail/template/save", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/template/save"), mailPage.MailTemplateSave)
	pages.POST("/mail/template/delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplateDelete)

	pages.POST("/mail/accounts/bulk-delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountsBulkDelete)
	pages.POST("/mail/templates/bulk-delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplatesBulkDelete)

	pages.GET("/mail/contacts",
		mw.PageAuthz("/api/mail/contact/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailContactsPage)
	pages.POST("/mail/contact/import", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/contact/import"), mailPage.MailContactImport)
	pages.POST("/mail/contact/status", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactStatus)
	pages.POST("/mail/contacts/bulk-status", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactsBulkStatus)

	pages.GET("/mail/campaigns",
		mw.PageAuthz("/api/mail/campaign/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailCampaignsPage)
	pages.POST("/mail/campaign/save", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/campaign/save"), mailPage.MailCampaignSave)
	pages.POST("/mail/campaign/start", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/campaign/start"), mailPage.MailCampaignStart)
	pages.POST("/mail/campaign/delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignDelete)
	pages.POST("/mail/campaigns/bulk-delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignsBulkDelete)

	pages.GET("/mail/campaign",
		mw.PageAuthz("/api/mail/campaign/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailCampaignPage)

	pages.GET("/mail/marketing", mailPage.MailMarketingRedirect)

	pages.GET("/mail/automation",
		mw.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailAutomationPage)

	pages.GET("/mail/automation/runs",
		mw.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailAutomationRunsPage)
	pages.GET("/mail/automation/run",
		mw.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailAutomationRunDetail)

	pages.GET("/mail/automation/canvas",
		mw.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailAutomationCanvas)
	pages.POST("/mail/automation/status", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/automation/status"), mailPage.MailAutomationStatus)
	pages.POST("/mail/automation/delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationDelete)
	pages.POST("/mail/automations/bulk-delete", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationsBulkDelete)
	pages.POST("/mail/automation/tick", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/mail/automation/tick"), mailPage.MailAutomationTick)
}
