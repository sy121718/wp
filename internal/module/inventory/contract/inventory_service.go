package inventorycontract

import (
	"context"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
)

type InventoryService interface {
	TransferService

	CreateWarehouse(ctx context.Context, req *inventorydto.CreateWarehouseReq) (res *inventorydto.WarehouseResp, err error)

	UpdateWarehouse(ctx context.Context, req *inventorydto.UpdateWarehouseReq) (res *inventorydto.WarehouseResp, err error)
	GetWarehouse(ctx context.Context, req *inventorydto.GetWarehouseReq) (res *inventorydto.WarehouseResp, err error)

	ListWarehouses(ctx context.Context, req *inventorydto.ListWarehouseReq) (list []*inventorydto.WarehouseResp, err error)

	DeleteWarehouse(ctx context.Context, req *inventorydto.DeleteWarehouseReq) (err error)

	EnsureStock(ctx context.Context, req *inventorydto.EnsureStockReq) (res *inventorydto.StockResp, err error)

	GetStock(ctx context.Context, req *inventorydto.GetStockReq) (res *inventorydto.StockResp, err error)

	ListStocksBySKU(ctx context.Context, req *inventorydto.ListStockBySKUReq) (list []*inventorydto.StockResp, err error)

	ListStocks(ctx context.Context, req *inventorydto.ListStockReq) (list []*inventorydto.StockResp, err error)

	BareSKUsByVariant(ctx context.Context, projectID string) (map[string]string, error)

	WarehouseStocksByProducts(ctx context.Context, projectID string, productIDs []string) (out []inventorydto.ProductWarehouseStock, err error)

	UpdateStockTracking(ctx context.Context, req *inventorydto.UpdateStockTrackingReq) (res *inventorydto.StockResp, err error)

	ListWarehouseSKUs(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (list []*inventorydto.WarehouseSKUResp, err error)

	GetWarehouseSKU(ctx context.Context, req *inventorydto.GetWarehouseSKUReq) (res *inventorydto.WarehouseSKUResp, err error)

	CreateStock(ctx context.Context, req *inventorydto.CreateStockReq) (res *inventorydto.StockResp, err error)

	UpdateStock(ctx context.Context, req *inventorydto.UpdateStockReq) (res *inventorydto.StockResp, err error)

	UpdateStockStatus(ctx context.Context, req *inventorydto.UpdateStockStatusReq) (res *inventorydto.StockResp, err error)

	CountWarehouseSKUsByFilter(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (total int64, err error)

	ListStockSKURows(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (rows []inventorydto.StockSKURowResp, err error)

	BindExternalSKU(ctx context.Context, req *inventorydto.BindExternalSKUReq) (res *inventorydto.StockResp, err error)

	ReserveStock(ctx context.Context, req *inventorydto.ReserveStockReq) (res *inventorydto.StockReserveResp, err error)

	ReleaseStock(ctx context.Context, req *inventorydto.ReserveStockReq) (res *inventorydto.StockReserveResp, err error)

	ReserveStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ReserveStockReq) (err error)
	ReleaseStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ReserveStockReq) (err error)

	StockAlert(ctx context.Context, projectID string) (res *inventorydto.StockAlertResp, err error)

	ChangeStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error)

	DeductStock(ctx context.Context, req *inventorydto.DeductStockReq) (res *inventorydto.StockChangeResp, err error)

	ChangeStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq) (err error)
	DeductStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.DeductStockReq) (err error)

	ListMovements(ctx context.Context, req *inventorydto.ListMovementReq) (list []*inventorydto.MovementResp, err error)

	CountMovements(ctx context.Context, req *inventorydto.ListMovementReq) (n int64, err error)

	ListReasons(ctx context.Context, req *inventorydto.ListReasonReq) (list []*inventorydto.ReasonResp, err error)
	CreateReason(ctx context.Context, req *inventorydto.CreateReasonReq) (res *inventorydto.ReasonResp, err error)
	UpdateReason(ctx context.Context, req *inventorydto.UpdateReasonReq) (res *inventorydto.ReasonResp, err error)

	ResolveVariantWarehouseCosts(ctx context.Context, projectID string, refs []inventorydto.VariantWarehouseCostRef) (out []inventorydto.VariantWarehouseCost, err error)

	VariantHasStockMovement(ctx context.Context, projectID, variantID string) (exists bool, err error)
}

type (
	ProductWarehouseStock = inventorydto.ProductWarehouseStock

	VariantWarehouseCostRef = inventorydto.VariantWarehouseCostRef

	ListWarehouseSKUReq = inventorydto.ListWarehouseSKUReq
	GetWarehouseSKUReq  = inventorydto.GetWarehouseSKUReq
	WarehouseSKUResp    = inventorydto.WarehouseSKUResp

	ListWarehouseReq = inventorydto.ListWarehouseReq
)

type StockReader interface {
	ListStocks(ctx context.Context, req *inventorydto.ListStockReq) (list []*inventorydto.StockResp, err error)

	ListWarehouses(ctx context.Context, req *inventorydto.ListWarehouseReq) (list []*inventorydto.WarehouseResp, err error)

	ListReasons(ctx context.Context, req *inventorydto.ListReasonReq) (list []*inventorydto.ReasonResp, err error)
}

type StockWriter interface {
	ChangeStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error)
}

type ReasonWriter interface {
	CreateReason(ctx context.Context, req *inventorydto.CreateReasonReq) (res *inventorydto.ReasonResp, err error)

	UpdateReason(ctx context.Context, req *inventorydto.UpdateReasonReq) (res *inventorydto.ReasonResp, err error)
}

type TransferService interface {
	CreateTransfer(ctx context.Context, req *inventorydto.CreateTransferReq) (res *inventorydto.TransferResp, err error)

	ApproveTransfer(ctx context.Context, req *inventorydto.TransferApproveReq) (res *inventorydto.TransferResp, err error)

	RejectTransfer(ctx context.Context, req *inventorydto.TransferRejectReq) (res *inventorydto.TransferResp, err error)

	ShipTransfer(ctx context.Context, req *inventorydto.TransferShipReq) (res *inventorydto.TransferResp, err error)

	ReceiveTransfer(ctx context.Context, req *inventorydto.TransferReceiveReq) (res *inventorydto.TransferResp, err error)

	CancelTransfer(ctx context.Context, req *inventorydto.TransferCancelReq) (res *inventorydto.TransferResp, err error)

	ListTransferStockOptions(ctx context.Context, req *inventorydto.TransferStockOptionReq) (list []*inventorydto.TransferStockOptionResp, err error)

	ListTransfers(ctx context.Context, req *inventorydto.ListTransferReq) (list []*inventorydto.TransferRowResp, err error)

	CountTransfers(ctx context.Context, req *inventorydto.ListTransferReq) (n int64, err error)

	GetTransfer(ctx context.Context, req *inventorydto.GetTransferReq) (res *inventorydto.TransferDetailResp, err error)
}

type StockAlertReader interface {
	StockAlert(ctx context.Context, projectID string) (res *inventorydto.StockAlertResp, err error)
}
