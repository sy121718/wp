package purchasecontract

import (
	"context"

	"go_wp/internal/module/purchase/dto"
)

type PurchaseService interface {
	CreateSource(ctx context.Context, req *purchasedto.CreateSourceReq) (res *purchasedto.SourceResp, err error)
	UpdateSource(ctx context.Context, req *purchasedto.UpdateSourceReq) (res *purchasedto.SourceResp, err error)
	GetSource(ctx context.Context, req *purchasedto.GetSourceReq) (res *purchasedto.SourceResp, err error)
	ListSources(ctx context.Context, req *purchasedto.ListSourceReq) (list []*purchasedto.SourceResp, err error)
	CountSources(ctx context.Context, req *purchasedto.ListSourceReq) (n int64, err error)
	DeleteSource(ctx context.Context, req *purchasedto.DeleteSourceReq) (err error)

	// BulkDeleteSources 在一个事务里删除多个货源：任一失败整批回滚。
	BulkDeleteSources(ctx context.Context, req *purchasedto.BulkDeleteSourceReq) error
	SourceSummary(ctx context.Context, req *purchasedto.SourceSummaryReq) (res *purchasedto.SourceSummaryResp, err error)

	CreatePurchaseOrder(ctx context.Context, req *purchasedto.CreatePurchaseOrderReq) (res *purchasedto.PurchaseOrderResp, err error)
	UpdatePurchaseOrder(ctx context.Context, req *purchasedto.UpdatePurchaseOrderReq) (res *purchasedto.PurchaseOrderResp, err error)
	GetPurchaseOrder(ctx context.Context, req *purchasedto.GetPurchaseOrderReq) (res *purchasedto.PurchaseOrderResp, err error)
	ListPurchaseOrders(ctx context.Context, req *purchasedto.ListPurchaseOrderReq) (list []*purchasedto.PurchaseOrderResp, err error)
	CountPurchaseOrders(ctx context.Context, req *purchasedto.ListPurchaseOrderReq) (n int64, err error)
	RegisterReceipt(ctx context.Context, req *purchasedto.RegisterReceiptReq) (res *purchasedto.ReceiptResp, err error)

	PlacePurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error)
	MarkPurchaseOrderException(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error)
	ResumePurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error)
	FinishPurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error)
	CancelPurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error)
	RegisterProductionInbound(ctx context.Context, req *purchasedto.ProductionInboundReq) (res *purchasedto.ReceiptResp, err error)
	ListPurchaseHistory(ctx context.Context, req *purchasedto.ListPurchaseHistoryReq) (list []*purchasedto.PurchaseHistoryResp, err error)
}
