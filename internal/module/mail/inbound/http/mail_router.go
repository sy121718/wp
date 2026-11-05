package mailhttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	mailcontract "go_wp/internal/module/mail/contract"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/internal/permission"
	pageview "go_wp/pkg/view"
)

func SetupMailRoutes(rg *permission.RouteGroup, db *gorm.DB, pages *gin.RouterGroup) mailcontract.MailService {
	secret := ""
	if v, err := config.GetViper(); err == nil && v != nil {
		secret = v.GetString("app.secret")
	}

	svc := mailservice.NewService(mailmodel.NewMailModel(db))
	svc.SetCipherSecret(secret)

	mailservice.RegisterMailTaskHandler(db, secret)

	mailservice.RegisterMailTrackTaskHandler(db)

	mailservice.RegisterMailAutomationTaskHandler(db, secret)

	mailservice.StartMailRetentionScheduler(svc)

	mailservice.StartMailAutomationScheduler(svc)

	handle := NewHandle(svc)
	g := rg.Group("/mail")
	g.GET("/account/list", permission.MailAccountList, handle.AccountList)
	g.POST("/account/save", permission.MailAccountSave, handle.AccountSave)
	g.POST("/account/delete", permission.MailAccountDelete, handle.AccountDelete)
	g.POST("/account/default", permission.MailAccountDefault, handle.AccountSetDefault)
	g.POST("/account/test", permission.MailAccountTest, handle.AccountTestSend)
	g.GET("/template/list", permission.MailTemplateList, handle.TemplateList)
	g.POST("/template/save", permission.MailTemplateSave, handle.TemplateSave)
	g.POST("/template/delete", permission.MailTemplateDelete, handle.TemplateDelete)
	g.GET("/contact/list", permission.MailContactList, handle.ContactList)
	g.POST("/contact/import", permission.MailContactImport, handle.ContactImport)
	g.POST("/contact/status", permission.MailContactStatus, handle.ContactStatus)

	g.POST("/contact/save", permission.MailContactSave, pageview.HTMLFragment, handle.ContactSave)
	g.POST("/contact/delete", permission.MailContactDelete, pageview.HTMLFragment, handle.ContactDelete)
	g.POST("/contact/tag", permission.MailContactTag, pageview.HTMLFragment, handle.ContactTag)

	g.GET("/campaign/list", permission.MailCampaignList, handle.CampaignList)
	g.GET("/campaign/get", permission.MailCampaignGet, handle.CampaignGet)
	g.POST("/campaign/save", permission.MailCampaignSave, handle.CampaignSave)
	g.POST("/campaign/delete", permission.MailCampaignDelete, handle.CampaignDelete)
	g.POST("/campaign/start", permission.MailCampaignStart, handle.CampaignStart)

	g.GET("/automation/list", permission.MailAutomationList, handle.AutomationList)
	g.GET("/automation/get", permission.MailAutomationGet, handle.AutomationGet)
	g.POST("/automation/save", permission.MailAutomationSave, handle.AutomationSave)
	g.POST("/automation/status", permission.MailAutomationStatus, handle.AutomationStatus)

	g.POST("/automation/layout", permission.MailAutomationLayout, handle.AutomationLayout)
	g.POST("/automation/delete", permission.MailAutomationDelete, handle.AutomationDelete)
	g.POST("/automation/start", permission.MailAutomationStart, handle.AutomationStartRun)
	g.GET("/automation/run/list", permission.MailAutomationRunList, handle.AutomationRunList)
	g.GET("/automation/run/detail", permission.MailAutomationRunDetail, handle.AutomationRunDetail)

	g.POST("/automation/tick", permission.MailAutomationTick, handle.AutomationTick)

	setupMailPageRoutes(pages, svc)

	return svc
}
