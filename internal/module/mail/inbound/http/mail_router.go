package mailhttp

import (
	"context"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	mailcontract "go_wp/internal/module/mail/contract"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

// SetupMailRoutes 装配邮件路由；返回的 stop 取消本模块启动的后台循环并等待在途一轮结束，
// 调用方必须在关闭数据库之前调用它（装配层登记到停止表）。
func SetupMailRoutes(ctx context.Context, rg *mw.RouteGroup, db *gorm.DB, pages *gin.RouterGroup) (mailcontract.MailService, func()) {
	secret := ""
	if v, err := config.GetViper(); err == nil && v != nil {
		secret = v.GetString("app.secret")
	}

	svc := mailservice.NewService(mailmodel.NewMailModel(db))
	svc.SetCipherSecret(secret)

	mailservice.RegisterMailTaskHandler(db, secret)

	mailservice.RegisterMailTrackTaskHandler(db)

	mailservice.RegisterMailAutomationTaskHandler(db, secret)

	stop := startMailBackgroundTasks(ctx, svc)

	handle := NewHandle(svc)
	g := rg.Group("/mail")
	g.GET("/account/list", mailenums.MailAccountList, handle.AccountList)
	g.POST("/account/save", mailenums.MailAccountSave, handle.AccountSave)
	g.POST("/account/delete", mailenums.MailAccountDelete, handle.AccountDelete)
	g.POST("/account/default", mailenums.MailAccountDefault, handle.AccountSetDefault)
	g.POST("/account/test", mailenums.MailAccountTest, handle.AccountTestSend)
	g.GET("/template/list", mailenums.MailTemplateList, handle.TemplateList)
	g.POST("/template/save", mailenums.MailTemplateSave, handle.TemplateSave)
	g.POST("/template/delete", mailenums.MailTemplateDelete, handle.TemplateDelete)
	g.GET("/contact/list", mailenums.MailContactList, handle.ContactList)
	g.POST("/contact/import", mailenums.MailContactImport, handle.ContactImport)
	g.POST("/contact/status", mailenums.MailContactStatus, handle.ContactStatus)

	g.POST("/contact/save", mailenums.MailContactSave, pageview.HTMLFragment, handle.ContactSave)
	g.POST("/contact/delete", mailenums.MailContactDelete, pageview.HTMLFragment, handle.ContactDelete)
	g.POST("/contact/tag", mailenums.MailContactTag, pageview.HTMLFragment, handle.ContactTag)

	g.GET("/campaign/list", mailenums.MailCampaignList, handle.CampaignList)
	g.GET("/campaign/get", mailenums.MailCampaignGet, handle.CampaignGet)
	g.POST("/campaign/save", mailenums.MailCampaignSave, handle.CampaignSave)
	g.POST("/campaign/delete", mailenums.MailCampaignDelete, handle.CampaignDelete)
	g.POST("/campaign/start", mailenums.MailCampaignStart, handle.CampaignStart)

	g.GET("/automation/list", mailenums.MailAutomationList, handle.AutomationList)
	g.GET("/automation/get", mailenums.MailAutomationGet, handle.AutomationGet)
	g.POST("/automation/save", mailenums.MailAutomationSave, handle.AutomationSave)
	g.POST("/automation/status", mailenums.MailAutomationStatus, handle.AutomationStatus)

	g.POST("/automation/layout", mailenums.MailAutomationLayout, handle.AutomationLayout)
	g.POST("/automation/delete", mailenums.MailAutomationDelete, handle.AutomationDelete)
	g.POST("/automation/start", mailenums.MailAutomationStart, handle.AutomationStartRun)
	g.GET("/automation/run/list", mailenums.MailAutomationRunList, handle.AutomationRunList)
	g.GET("/automation/run/detail", mailenums.MailAutomationRunDetail, handle.AutomationRunDetail)

	g.POST("/automation/tick", mailenums.MailAutomationTick, handle.AutomationTick)

	setupMailPageRoutes(rg.Registry(), pages, svc)

	return svc, stop
}

// startMailBackgroundTasks 启动邮件模块的两个周期任务并合并停止句柄。
// 两者都接装配级 ctx：收尾时先 cancel、再等这一轮退出，最后才关数据库。
func startMailBackgroundTasks(ctx context.Context, svc *mailservice.Service) func() {
	stopRetention := mailservice.StartMailRetentionScheduler(ctx, svc)
	stopAutomation := mailservice.StartMailAutomationScheduler(ctx, svc)
	return func() {
		stopRetention()
		stopAutomation()
	}
}
