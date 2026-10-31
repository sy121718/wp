package ordercontract

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
)

type ShipmentService interface {
	ShipOrder(ctx context.Context, req *orderdto.ShipOrderReq) (res *orderdto.ShipmentResp, err error)

	ListShipments(ctx context.Context, req *orderdto.ListShipmentReq) (list []*orderdto.ShipmentResp, err error)

	ListPendingShip(ctx context.Context, req *orderdto.PendingShipReq) (res *orderdto.PendingShipPageResp, err error)

	BuildShipForm(ctx context.Context, req *orderdto.ListShipmentReq) (form *orderdto.ShipForm, err error)

	ListCarriers(ctx context.Context, req *orderdto.ListCarrierReq) (list []*orderdto.CarrierResp, err error)

	CreateCarrier(ctx context.Context, req *orderdto.CarrierCreateReq) (res *orderdto.CarrierResp, err error)

	UpdateCarrier(ctx context.Context, req *orderdto.CarrierUpdateReq) (res *orderdto.CarrierResp, err error)
}

type ShipProbeLine struct {
	OrderItemID uint64
	VariantID   string

	WarehouseID string

	Quantity int
}

type ShipAvailability struct {
	NoWarehouse bool

	Infinite bool

	Unmatched bool

	Available int
}

type ShipStockProbe interface {
	ShipAvailabilityByLine(ctx context.Context, projectID string, lines []ShipProbeLine) (out map[uint64]ShipAvailability, err error)
}
