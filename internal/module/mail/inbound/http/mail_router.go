package mailhttp

// 后台页面的注册与鉴权对象声明在 mail_page_router.go，由本文件的 SetupMailRoutes
// 在同一位置调用 —— 落点分开、装配顺序不变。pages 为 nil 时跳过页面注册，
// 与 rg == nil 早退同构：模块装配不因缺少页面组而失败。

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	"go_wp/internal/module/mail/contract"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/module/mail/service"
	"go_wp/internal/permission"
)

// SetupMailRoutes 装配邮箱模块路由，返回模块契约。
//
// 加密密钥（config.yaml 的 app.secret）在这里从配置读入并注入 service，
// 同时交给队列 handler —— worker 要解密账号密码才能发信。
// service 自己不读 config（模块不直接碰配置读取，装配层负责注入）。
//
// pages 为装配层传入的后台页面组（/admin，已挂 Session + CSRF + 权限上下文）；
// 页面与 API 在同一处装配，pages 为 nil 时只跳过页面注册。
func SetupMailRoutes(rg *permission.RouteGroup, db *gorm.DB, pages *gin.RouterGroup) mailcontract.MailService {
	secret := ""
	if v, err := config.GetViper(); err == nil && v != nil {
		secret = v.GetString("app.secret")
	}

	svc := mailservice.NewService(mailmodel.NewMailModel(db))
	svc.SetCipherSecret(secret)
	// 队列 handler 与 service 用同一份密钥（注册是幂等的，路由装配期调一次）。
	mailservice.RegisterMailTaskHandler(db, secret)
	// 追踪事件落库也走队列（端点只验签 + 入队，不写库）。
	mailservice.RegisterMailTrackTaskHandler(db)
	// 自动化实例推进（P3）：事件触发立即投递，等待节点按 next_run_at 延时投递。
	mailservice.RegisterMailAutomationTaskHandler(db, secret)

	// 保留期任务（IDX-012）：先固化活动事件汇总，再清理超期的事件明细与发送日志。
	// 没有定时任务时这两张表只增不减 —— 一次大群发就能把事件表撑到不可维护。
	mailservice.StartMailRetentionScheduler(svc)

	// 自动化延时兜底（#38 P3）：等待节点的唤醒主路径是队列延时任务，而 Redis 掉数据 /
	// queue.enabled=false / worker 崩在入队与执行之间时，到点的实例会永远挂着
	//（只能等运营在后台手工点一次「补投一轮」）。这条周期扫描就是那条兜底保证。
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
	// 群发活动：启动只受理（统计人数 + 改状态 + 入队展开任务），收件人展开在后台分批完成。
	g.GET("/campaign/list", permission.MailCampaignList, handle.CampaignList)
	g.GET("/campaign/get", permission.MailCampaignGet, handle.CampaignGet)
	g.POST("/campaign/save", permission.MailCampaignSave, handle.CampaignSave)
	g.POST("/campaign/delete", permission.MailCampaignDelete, handle.CampaignDelete)
	g.POST("/campaign/start", permission.MailCampaignStart, handle.CampaignStart)

	// 自动化（#38 P3）：流程定义 CRUD + 实例排障。
	// 保存与启用都会校验图（无环 / 可达 / 形状）—— 这是引擎正确性的第一道关。
	g.GET("/automation/list", permission.MailAutomationList, handle.AutomationList)
	g.GET("/automation/get", permission.MailAutomationGet, handle.AutomationGet)
	g.POST("/automation/save", permission.MailAutomationSave, handle.AutomationSave)
	g.POST("/automation/status", permission.MailAutomationStatus, handle.AutomationStatus)
	// 画布位置（P4）：与 save 分开，位置不推进版本号。权限点沿用 save。
	g.POST("/automation/layout", permission.MailAutomationLayout, handle.AutomationLayout)
	g.POST("/automation/delete", permission.MailAutomationDelete, handle.AutomationDelete)
	g.POST("/automation/start", permission.MailAutomationStart, handle.AutomationStartRun)
	g.GET("/automation/run/list", permission.MailAutomationRunList, handle.AutomationRunList)
	g.GET("/automation/run/detail", permission.MailAutomationRunDetail, handle.AutomationRunDetail)
	// 延时兜底的手工触发：队列延时任务失效时，运维可立刻补投一轮。
	g.POST("/automation/tick", permission.MailAutomationTick, handle.AutomationTick)

	// 后台页面（/admin/mail*）：壳层与权限点见 mail_page_router.go。
	setupMailPageRoutes(pages, svc)

	return svc
}
