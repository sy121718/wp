// Package purchasecontract purchase 模块对外契约（货源 / 采购单 / 入库）。
package purchasecontract

import (
	"context"

	"go_wp/internal/module/purchase/dto"
)

// PurchaseService 货源、采购单与入库的业务出口（页面与接口共用）。
//
// 边界：本模块管**进货来源与进货单据**（inventory_sources 与 inventory_purchase_* 四张表，
// 2026-10 从 inventory 迁入），不碰库存真源 —— 收货引起的数量增减一律经 StockPort 由
// inventory 模块在自己的事务里落账（原因字典 / 流水 / 来源引用齐全），绝不旁路写
// inventory_stocks。
type PurchaseService interface {
	// —— 货源（issue #17）——
	CreateSource(ctx context.Context, req *purchasedto.CreateSourceReq) (res *purchasedto.SourceResp, err error)
	UpdateSource(ctx context.Context, req *purchasedto.UpdateSourceReq) (res *purchasedto.SourceResp, err error)
	GetSource(ctx context.Context, req *purchasedto.GetSourceReq) (res *purchasedto.SourceResp, err error)
	ListSources(ctx context.Context, req *purchasedto.ListSourceReq) (list []*purchasedto.SourceResp, err error)
	CountSources(ctx context.Context, req *purchasedto.ListSourceReq) (n int64, err error)
	DeleteSource(ctx context.Context, req *purchasedto.DeleteSourceReq) (err error)
	SourceSummary(ctx context.Context, req *purchasedto.SourceSummaryReq) (res *purchasedto.SourceSummaryResp, err error)

	// —— 采购单与入库（issue #18）——
	CreatePurchaseOrder(ctx context.Context, req *purchasedto.CreatePurchaseOrderReq) (res *purchasedto.PurchaseOrderResp, err error)
	UpdatePurchaseOrder(ctx context.Context, req *purchasedto.UpdatePurchaseOrderReq) (res *purchasedto.PurchaseOrderResp, err error)
	GetPurchaseOrder(ctx context.Context, req *purchasedto.GetPurchaseOrderReq) (res *purchasedto.PurchaseOrderResp, err error)
	ListPurchaseOrders(ctx context.Context, req *purchasedto.ListPurchaseOrderReq) (list []*purchasedto.PurchaseOrderResp, err error)
	CountPurchaseOrders(ctx context.Context, req *purchasedto.ListPurchaseOrderReq) (n int64, err error)
	RegisterReceipt(ctx context.Context, req *purchasedto.RegisterReceiptReq) (res *purchasedto.ReceiptResp, err error)
	FinishPurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error)
	CancelPurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error)
	RegisterProductionInbound(ctx context.Context, req *purchasedto.ProductionInboundReq) (res *purchasedto.ReceiptResp, err error)
	ListPurchaseHistory(ctx context.Context, req *purchasedto.ListPurchaseHistoryReq) (list []*purchasedto.PurchaseHistoryResp, err error)
}
