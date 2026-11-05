package webhookhttp

import (
	"gorm.io/gorm"

	"go_wp/config"
	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	webhookservice "go_wp/internal/module/webhook/service"
	mw "go_wp/pkg/middleware"
)

func SetupWebhookRoutes(rg *mw.RouteGroup, db *gorm.DB) webhookcontract.EndpointService {
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

	g.GET("/endpoint/list", webhookenums.WebhookEndpointList, h.EndpointList)
	g.POST("/endpoint/save", webhookenums.WebhookEndpointSave, h.EndpointSave)
	g.POST("/endpoint/delete", webhookenums.WebhookEndpointDelete, h.EndpointDelete)
	g.POST("/endpoint/status", webhookenums.WebhookEndpointStatus, h.EndpointStatus)

	g.GET("/delivery/list", webhookenums.WebhookDeliveryList, h.DeliveryList)
	g.POST("/delivery/retry", webhookenums.WebhookDeliveryRetry, h.DeliveryRetry)

	return svc
}
