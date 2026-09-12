// mail_router.go — 邮箱模块路由自装配（issue #37）。
package mailhttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	mailcontract "go_wp/internal/module/mail/contract"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
)

// SetupMailRoutes 装配邮箱模块路由，返回模块契约。
//
// 加密密钥（config.yaml 的 app.secret）在这里从配置读入并注入 service，
// 同时交给队列 handler —— worker 要解密账号密码才能发信。
// service 自己不读 config（模块不直接碰配置读取，装配层负责注入）。
func SetupMailRoutes(rg *gin.RouterGroup, db *gorm.DB) mailcontract.MailService {
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

	handle := NewHandle(svc)
	g := rg.Group("/mail")
	g.GET("/account/list", handle.AccountList)
	g.POST("/account/save", handle.AccountSave)
	g.POST("/account/delete", handle.AccountDelete)
	g.POST("/account/default", handle.AccountSetDefault)
	g.POST("/account/test", handle.AccountTestSend)
	g.GET("/template/list", handle.TemplateList)
	g.POST("/template/save", handle.TemplateSave)
	g.POST("/template/delete", handle.TemplateDelete)
	g.GET("/contact/list", handle.ContactList)
	g.POST("/contact/import", handle.ContactImport)
	g.POST("/contact/status", handle.ContactStatus)
	// 群发活动：启动只受理（统计人数 + 改状态 + 入队展开任务），收件人展开在后台分批完成。
	g.GET("/campaign/list", handle.CampaignList)
	g.GET("/campaign/get", handle.CampaignGet)
	g.POST("/campaign/save", handle.CampaignSave)
	g.POST("/campaign/delete", handle.CampaignDelete)
	g.POST("/campaign/start", handle.CampaignStart)

	// 自动化（#38 P3）：流程定义 CRUD + 实例排障。
	// 保存与启用都会校验图（无环 / 可达 / 形状）—— 这是引擎正确性的第一道关。
	g.GET("/automation/list", handle.AutomationList)
	g.GET("/automation/get", handle.AutomationGet)
	g.POST("/automation/save", handle.AutomationSave)
	g.POST("/automation/status", handle.AutomationStatus)
	g.POST("/automation/delete", handle.AutomationDelete)
	g.POST("/automation/start", handle.AutomationStartRun)
	g.GET("/automation/run/list", handle.AutomationRunList)
	g.GET("/automation/run/detail", handle.AutomationRunDetail)
	// 延时兜底的手工触发：队列延时任务失效时，运维可立刻补投一轮。
	g.POST("/automation/tick", handle.AutomationTick)

	return svc
}
