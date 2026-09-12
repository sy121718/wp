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

	return svc
}
