// webhook_router.go — 外部集成端点路由自装配（OSS-006 / SEC-015）。
//
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin），与其它后台模块一致。
// 权限点见迁移 213：**新增挂在本组下的接口必须同批 seed**，否则含超管在内全员 403。
package webhookhttp

import (
	"gorm.io/gorm"

	"go_wp/config"
	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookmodel "go_wp/internal/module/webhook/model"
	webhookservice "go_wp/internal/module/webhook/service"
	"go_wp/internal/permission"
)

// SetupWebhookRoutes 装配 webhook 模块路由，返回模块契约。
//
// 加密密钥（config.yaml 的 app.secret）在这里从配置读入并注入 service，
// 同时交给队列 handler —— worker 要解密端点密钥才能对出站请求签名。
// service 自己不读 config（模块不直接碰配置读取，装配层负责注入）。
func SetupWebhookRoutes(rg *permission.RouteGroup, db *gorm.DB) webhookcontract.EndpointService {
	secret := ""
	if v, err := config.GetViper(); err == nil && v != nil {
		secret = v.GetString("app.secret")
	}

	svc := webhookservice.NewService(webhookmodel.NewWebhookModel(db))
	svc.SetCipherSecret(secret)
	// 投递 worker 与 service 用同一份密钥（注册幂等，路由装配期调一次）。
	webhookservice.RegisterWebhookTaskHandler(db, secret)

	h := NewHandle(svc)
	g := rg.Group("/webhook")
	// 端点（白名单）管理。
	g.GET("/endpoint/list", permission.WebhookEndpointList, h.EndpointList)
	g.POST("/endpoint/save", permission.WebhookEndpointSave, h.EndpointSave)
	g.POST("/endpoint/delete", permission.WebhookEndpointDelete, h.EndpointDelete)
	g.POST("/endpoint/status", permission.WebhookEndpointStatus, h.EndpointStatus)
	// 投递日志与重投（排障）。
	g.GET("/delivery/list", permission.WebhookDeliveryList, h.DeliveryList)
	g.POST("/delivery/retry", permission.WebhookDeliveryRetry, h.DeliveryRetry)

	return svc
}
