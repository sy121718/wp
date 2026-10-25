package ordercontract

// order_shipment.go — 发货与承运商的契约（迁移 627 / 625，S2）。

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
)

// ShipmentService 发货用例与承运商管理。
//
// 与订单主体分开一个接口：发货是一个独立的**单据动作**（有单号、有承运商、有明细），
// 而不只是「改订单状态」的另一种写法 —— 两者混在一起会让读者以为
// ChangeStatus(shipped) 与 ShipOrder 等价（前者不扣库存、不建单据）。
type ShipmentService interface {
	// ShipOrder 发货：建发货单 + 扣库存（同时释放预占）+ 置 shipped，全在一个事务里。
	//
	// request_id 幂等：同一 (工程, request_id) 只会产生一张发货单，重复提交直接返回那张。
	ShipOrder(ctx context.Context, req *orderdto.ShipOrderReq) (res *orderdto.ShipmentResp, err error)
	// ListShipments 某订单的发货记录（客户与后台看同一份）。
	ListShipments(ctx context.Context, req *orderdto.ListShipmentReq) (list []*orderdto.ShipmentResp, err error)
	// ListPendingShip 待发货列表：只列 paid 且还有未发数量的订单。
	ListPendingShip(ctx context.Context, projectID string) (rows []*orderdto.PendingShipRow, err error)
	// BuildShipForm 发货表单数据（订单头 + 每行的「订单数量 / 已发 / 剩余 / 默认仓」+ 承运商下拉）。
	BuildShipForm(ctx context.Context, req *orderdto.ListShipmentReq) (form *orderdto.ShipForm, err error)

	// ListCarriers 承运商列表（默认只列启用中的）。
	ListCarriers(ctx context.Context, req *orderdto.ListCarrierReq) (list []*orderdto.CarrierResp, err error)
	// CreateCarrier 新建承运商（代码工程内唯一，大小写不敏感）。
	CreateCarrier(ctx context.Context, req *orderdto.CarrierCreateReq) (res *orderdto.CarrierResp, err error)
	// UpdateCarrier 修改承运商（停用走 Status，不物理删：历史发货单还指着它）。
	UpdateCarrier(ctx context.Context, req *orderdto.CarrierUpdateReq) (res *orderdto.CarrierResp, err error)
}
