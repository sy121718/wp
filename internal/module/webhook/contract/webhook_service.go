package webhookcontract

import (
	"context"

	webhookdto "go_wp/internal/module/webhook/dto"
)

const (
	EventOrderPaid = "order.paid"
)

type EndpointService interface {
	CreateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error)

	UpdateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error)
	DeleteEndpoint(ctx context.Context, id uint64) (err error)

	SetEndpointStatus(ctx context.Context, id uint64, status int) (err error)

	ListEndpoints(ctx context.Context, eventType string) (list []*webhookdto.EndpointItem, err error)

	ListDeliveries(ctx context.Context, req *webhookdto.DeliveryListReq) (res *webhookdto.DeliveryListResp, err error)

	RetryDelivery(ctx context.Context, id uint64) (err error)
}

type Dispatcher interface {
	DispatchEvent(ctx context.Context, eventType string, payload any) (queued int, err error)
}
