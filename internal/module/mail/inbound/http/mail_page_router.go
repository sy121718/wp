package mailhttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	mailcontract "go_wp/internal/module/mail/contract"
	"go_wp/internal/permission"
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
	declareMailPageObjects()

	pages.GET("/mail",
		adminui.PageAuthz("/api/mail/account/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailPage)
	pages.POST("/mail/account/save", builtin.CasbinMiddlewareForPath("/api/mail/account/save"), mailPage.MailAccountSave)
	pages.POST("/mail/account/delete", builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountDelete)
	pages.POST("/mail/account/default", builtin.CasbinMiddlewareForPath("/api/mail/account/default"), mailPage.MailAccountDefault)
	pages.POST("/mail/account/test", builtin.CasbinMiddlewareForPath("/api/mail/account/test"), mailPage.MailAccountTest)

	pages.GET("/mail/templates",
		adminui.PageAuthz("/api/mail/template/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailTemplatesPage)

	pages.GET("/mail/templates/editor",
		adminui.PageAuthz("/api/mail/template/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailTemplateEditor)
	pages.POST("/mail/template/save", builtin.CasbinMiddlewareForPath("/api/mail/template/save"), mailPage.MailTemplateSave)
	pages.POST("/mail/template/delete", builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplateDelete)

	pages.POST("/mail/accounts/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountsBulkDelete)
	pages.POST("/mail/templates/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplatesBulkDelete)

	pages.GET("/mail/contacts",
		adminui.PageAuthz("/api/mail/contact/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailContactsPage)
	pages.POST("/mail/contact/import", builtin.CasbinMiddlewareForPath("/api/mail/contact/import"), mailPage.MailContactImport)
	pages.POST("/mail/contact/status", builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactStatus)
	pages.POST("/mail/contacts/bulk-status", builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactsBulkStatus)

	pages.GET("/mail/campaigns",
		adminui.PageAuthz("/api/mail/campaign/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailCampaignsPage)
	pages.POST("/mail/campaign/save", builtin.CasbinMiddlewareForPath("/api/mail/campaign/save"), mailPage.MailCampaignSave)
	pages.POST("/mail/campaign/start", builtin.CasbinMiddlewareForPath("/api/mail/campaign/start"), mailPage.MailCampaignStart)
	pages.POST("/mail/campaign/delete", builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignDelete)
	pages.POST("/mail/campaigns/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignsBulkDelete)

	pages.GET("/mail/campaign",
		adminui.PageAuthz("/api/mail/campaign/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailCampaignPage)

	pages.GET("/mail/marketing", mailPage.MailMarketingRedirect)

	pages.GET("/mail/automation",
		adminui.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailAutomationPage)

	pages.GET("/mail/automation/runs",
		adminui.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailAutomationRunsPage)
	pages.GET("/mail/automation/run",
		adminui.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailAutomationRunDetail)

	pages.GET("/mail/automation/canvas",
		adminui.PageAuthz("/api/mail/automation/list"),
		pageview.UseRenderer(adminui.RenderPage),
		mailPage.MailAutomationCanvas)
	pages.POST("/mail/automation/status", builtin.CasbinMiddlewareForPath("/api/mail/automation/status"), mailPage.MailAutomationStatus)
	pages.POST("/mail/automation/delete", builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationDelete)
	pages.POST("/mail/automations/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationsBulkDelete)
	pages.POST("/mail/automation/tick", builtin.CasbinMiddlewareForPath("/api/mail/automation/tick"), mailPage.MailAutomationTick)
}

func declareMailPageObjects() {
	permission.Declare("POST", "/api/mail/account/save", permission.MailAccountSave)
	permission.Declare("POST", "/api/mail/account/delete", permission.MailAccountDelete)
	permission.Declare("POST", "/api/mail/account/default", permission.MailAccountDefault)
	permission.Declare("POST", "/api/mail/account/test", permission.MailAccountTest)
	permission.Declare("POST", "/api/mail/template/save", permission.MailTemplateSave)
	permission.Declare("POST", "/api/mail/template/delete", permission.MailTemplateDelete)
	permission.Declare("POST", "/api/mail/contact/import", permission.MailContactImport)
	permission.Declare("POST", "/api/mail/contact/status", permission.MailContactStatus)
	permission.Declare("POST", "/api/mail/contact/save", permission.MailContactSave)
	permission.Declare("POST", "/api/mail/contact/delete", permission.MailContactDelete)
	permission.Declare("POST", "/api/mail/contact/tag", permission.MailContactTag)
	permission.Declare("POST", "/api/mail/campaign/save", permission.MailCampaignSave)
	permission.Declare("POST", "/api/mail/campaign/start", permission.MailCampaignStart)
	permission.Declare("POST", "/api/mail/campaign/delete", permission.MailCampaignDelete)
	permission.Declare("POST", "/api/mail/automation/save", permission.MailAutomationSave)
	permission.Declare("POST", "/api/mail/automation/status", permission.MailAutomationStatus)
	permission.Declare("POST", "/api/mail/automation/delete", permission.MailAutomationDelete)
	permission.Declare("POST", "/api/mail/automation/tick", permission.MailAutomationTick)
}
