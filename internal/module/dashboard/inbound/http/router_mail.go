package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_mail.go - 邮箱与营销自动化页路由（账号/模板/联系人/活动/自动化）。

func setupMailRoutes(adminPages *gin.RouterGroup, d *routeDeps) {
	mailPage := &mailPageHandle{mail: d.mail}

	// 邮箱（issue #37）：配置页（账号 / 模板）与营销页（联系人 / 群发）分成两页 ——
	// 日常操作营销的人不需要看到 SMTP 配置。页面路由的鉴权沿用对应 API 的权限点。
	adminPages.GET("/mail", mailPage.MailPage)
	adminPages.POST("/mail/account/save", builtin.CasbinMiddlewareForPath("/api/mail/account/save"), mailPage.MailAccountSave)
	adminPages.POST("/mail/account/delete", builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountDelete)
	adminPages.POST("/mail/account/default", builtin.CasbinMiddlewareForPath("/api/mail/account/default"), mailPage.MailAccountDefault)
	adminPages.POST("/mail/account/test", builtin.CasbinMiddlewareForPath("/api/mail/account/test"), mailPage.MailAccountTest)
	adminPages.POST("/mail/template/save", builtin.CasbinMiddlewareForPath("/api/mail/template/save"), mailPage.MailTemplateSave)
	adminPages.POST("/mail/template/delete", builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplateDelete)
	adminPages.GET("/mail/marketing", mailPage.MailMarketingPage)
	adminPages.POST("/mail/contact/import", builtin.CasbinMiddlewareForPath("/api/mail/contact/import"), mailPage.MailContactImport)
	adminPages.POST("/mail/contact/status", builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactStatus)
	adminPages.POST("/mail/campaign/save", builtin.CasbinMiddlewareForPath("/api/mail/campaign/save"), mailPage.MailCampaignSave)
	adminPages.POST("/mail/campaign/start", builtin.CasbinMiddlewareForPath("/api/mail/campaign/start"), mailPage.MailCampaignStart)
	adminPages.POST("/mail/campaign/delete", builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignDelete)
	// 活动报表（#38 P1）：打开 / 点击 / 退订与收件人明细。报表是只读，权限沿用活动列表。
	adminPages.GET("/mail/campaign", mailPage.MailCampaignPage)
	// 自动化（#38 P3，目标 ⑦）：表单式流程编辑 + 实例排障。
	// 页面路由在 adminPages 组（SessionAuth + CSRF），写操作额外走 Casbin 权限点。
	adminPages.GET("/mail/automation", mailPage.MailAutomationPage)
	adminPages.GET("/mail/automation/edit", mailPage.MailAutomationEdit)
	adminPages.GET("/mail/automation/run", mailPage.MailAutomationRunDetail)
	// 画布（P4）：可视化摆放节点。连线仍在侧栏下拉里改（触屏 / 键盘都能用）。
	adminPages.GET("/mail/automation/canvas", mailPage.MailAutomationCanvas)
	adminPages.POST("/mail/automation/save", builtin.CasbinMiddlewareForPath("/api/mail/automation/save"), mailPage.MailAutomationSave)
	adminPages.POST("/mail/automation/status", builtin.CasbinMiddlewareForPath("/api/mail/automation/status"), mailPage.MailAutomationStatus)
	adminPages.POST("/mail/automation/delete", builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationDelete)
	adminPages.POST("/mail/automation/tick", builtin.CasbinMiddlewareForPath("/api/mail/automation/tick"), mailPage.MailAutomationTick)
}
