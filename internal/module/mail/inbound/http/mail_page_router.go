package mailhttp

// mail_page_router.go — 邮箱后台页路由（原 dashboard 的 router_mail.go）。
//
// 页面挂在装配层传入的 /admin 组上：该组已有 SessionAuth + CSRF + 权限上下文中间件
// （见 internal/routers/assembly.go 的 adminPages）。写操作额外按**对应 API 的路径**
// 走 Casbin 权限点，权限点路径与 API 完全同源（一个字符都不改）。
// pages 为 nil 时跳过页面注册 —— 与 rg == nil 早退同构：模块装配不因缺少页面组而失败。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	mailcontract "go_wp/internal/module/mail/contract"
)

// setupMailPageRoutes 注册邮箱后台页（pages = /admin 页面组）。
func setupMailPageRoutes(pages *gin.RouterGroup, svc mailcontract.MailService) {
	if pages == nil || svc == nil {
		return
	}
	mailPage := NewMailPageHandle(svc)

	// 邮箱（issue #37）：配置页（账号 / 模板）与营销页（联系人 / 群发）分成两页 ——
	// 日常操作营销的人不需要看到 SMTP 配置。页面路由的鉴权沿用对应 API 的权限点。
	pages.GET("/mail", mailPage.MailPage)
	pages.POST("/mail/account/save", builtin.CasbinMiddlewareForPath("/api/mail/account/save"), mailPage.MailAccountSave)
	pages.POST("/mail/account/delete", builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountDelete)
	pages.POST("/mail/account/default", builtin.CasbinMiddlewareForPath("/api/mail/account/default"), mailPage.MailAccountDefault)
	pages.POST("/mail/account/test", builtin.CasbinMiddlewareForPath("/api/mail/account/test"), mailPage.MailAccountTest)
	pages.POST("/mail/template/save", builtin.CasbinMiddlewareForPath("/api/mail/template/save"), mailPage.MailTemplateSave)
	pages.POST("/mail/template/delete", builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplateDelete)
	// 批量动作（评审规则 admin-ui-logic §7：列表首列勾选 + 批量条）。
	// 权限点与对应单条动作**完全同源**（同一个 CasbinMiddlewareForPath、同一组 API 路径），
	// 不新增权限点、不写迁移 —— 单列一条策略就等于把「能删一个」的人挡在批量外。
	pages.POST("/mail/accounts/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/account/delete"), mailPage.MailAccountsBulkDelete)
	pages.POST("/mail/templates/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/template/delete"), mailPage.MailTemplatesBulkDelete)
	pages.GET("/mail/marketing", mailPage.MailMarketingPage)
	pages.POST("/mail/contact/import", builtin.CasbinMiddlewareForPath("/api/mail/contact/import"), mailPage.MailContactImport)
	pages.POST("/mail/contact/status", builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactStatus)
	pages.POST("/mail/contacts/bulk-status", builtin.CasbinMiddlewareForPath("/api/mail/contact/status"), mailPage.MailContactsBulkStatus)
	pages.POST("/mail/campaign/save", builtin.CasbinMiddlewareForPath("/api/mail/campaign/save"), mailPage.MailCampaignSave)
	pages.POST("/mail/campaign/start", builtin.CasbinMiddlewareForPath("/api/mail/campaign/start"), mailPage.MailCampaignStart)
	pages.POST("/mail/campaign/delete", builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignDelete)
	pages.POST("/mail/campaigns/bulk-delete", builtin.CasbinMiddlewareForPath("/api/mail/campaign/delete"), mailPage.MailCampaignsBulkDelete)
	// 活动报表（#38 P1）：打开 / 点击 / 退订与收件人明细。报表是只读，权限沿用活动列表。
	pages.GET("/mail/campaign", mailPage.MailCampaignPage)
	// 自动化（#38 P3，目标 ⑦）：表单式流程编辑 + 实例排障。
	// 页面路由在 /admin 组（SessionAuth + CSRF），写操作额外走 Casbin 权限点。
	pages.GET("/mail/automation", mailPage.MailAutomationPage)
	pages.GET("/mail/automation/edit", mailPage.MailAutomationEdit)
	pages.GET("/mail/automation/run", mailPage.MailAutomationRunDetail)
	// 画布（P4）：可视化摆放节点。连线仍在侧栏下拉里改（触屏 / 键盘都能用）。
	pages.GET("/mail/automation/canvas", mailPage.MailAutomationCanvas)
	pages.POST("/mail/automation/save", builtin.CasbinMiddlewareForPath("/api/mail/automation/save"), mailPage.MailAutomationSave)
	pages.POST("/mail/automation/status", builtin.CasbinMiddlewareForPath("/api/mail/automation/status"), mailPage.MailAutomationStatus)
	pages.POST("/mail/automation/delete", builtin.CasbinMiddlewareForPath("/api/mail/automation/delete"), mailPage.MailAutomationDelete)
	pages.POST("/mail/automation/tick", builtin.CasbinMiddlewareForPath("/api/mail/automation/tick"), mailPage.MailAutomationTick)
}
