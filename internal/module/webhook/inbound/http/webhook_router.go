package webhookhttp

import (
	"gorm.io/gorm"

	"go_wp/config"
	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookmodel "go_wp/internal/module/webhook/model"
	webhookservice "go_wp/internal/module/webhook/service"
	"go_wp/internal/permission"
)

func SetupWebhookRoutes(rg *permission.RouteGroup, db *gorm.DB) webhookcontract.EndpointService {
	secret := ""
	if v, err := config.GetViper(); err == nil && v != nil {
		secret = v.GetString("app.secret")
	}

	svc := webhookservice.NewService(webhookmodel.NewWebhookModel(db))
	svc.SetCipherSecret(secret)

	webhookservice.RegisterWebhookTaskHandler(db, secret)

	webhookservice.StartWebhookReplayScheduler(svc)

	h := NewHandle(svc)
	g := rg.Group("/webhook")

	g.GET("/endpoint/list", permission.WebhookEndpointList, h.EndpointList)
	g.POST("/endpoint/save", permission.WebhookEndpointSave, h.EndpointSave)
	g.POST("/endpoint/delete", permission.WebhookEndpointDelete, h.EndpointDelete)
	g.POST("/endpoint/status", permission.WebhookEndpointStatus, h.EndpointStatus)

	g.GET("/delivery/list", permission.WebhookDeliveryList, h.DeliveryList)
	g.POST("/delivery/retry", permission.WebhookDeliveryRetry, h.DeliveryRetry)

	return svc
}
