package ordercontract

import (
	"context"

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
)

type OrderService interface {
	VisitorOrderReader

	OrderNoReader

	CouponService

	ReturnService

	CustomerOrderSummaryReader

	CustomerGrowthReader

	CustomerSegmentReader

	CustomerRfmReader

	CustomerCohortReader

	OrderRangeSummaryReader

	OrderSalesOverviewReader

	OrderOverviewReader

	ShipmentService

	CurrencyService

	OrderQueryReader

	CreateOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)

	CreateAPIOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)

	CreateAdminOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)

	GetOrder(ctx context.Context, req *orderdto.GetOrderReq) (res *orderdto.OrderDetailResp, err error)

	ListOrders(ctx context.Context, req *orderdto.ListOrderReq) (res *orderdto.OrderListResp, err error)

	ChangeStatus(ctx context.Context, req *orderdto.ChangeStatusReq) (err error)

	ChangeStatusBatch(ctx context.Context, reqs []*orderdto.ChangeStatusReq) (err error)

	PayOrder(ctx context.Context, req *orderdto.PayOrderReq) (res *orderdto.PayOrderResp, err error)

	CancelOrder(ctx context.Context, req *orderdto.CancelOrderReq) (res *orderdto.CancelOrderResp, err error)

	CancelOrderBatch(ctx context.Context, reqs []*orderdto.CancelOrderReq) (err error)

	UpdateOrderNote(ctx context.Context, req *orderdto.UpdateOrderNoteReq) (res *orderdto.OrderResp, err error)

	RefundOrder(ctx context.Context, req *orderdto.RefundOrderReq) (err error)
}

type VisitorOrderPort interface {
	VisitorOrderReader
	VisitorReturnPort
}

type VisitorReturnPort interface {
	RequestReturn(ctx context.Context, req *orderdto.ReturnRequestReq) (res *orderdto.VisitorReturnResp, err error)

	ReturnableOfOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.ReturnableResp, err error)

	ListVisitorReturns(ctx context.Context, req *orderdto.VisitorReturnListReq) (res *orderdto.VisitorReturnListResp, err error)
}

type ReturnService interface {
	VisitorReturnPort

	CancelReturn(ctx context.Context, req *orderdto.ReturnCancelReq) (err error)

	ListReturns(ctx context.Context, req *orderdto.ReturnListReq) (res *orderdto.ReturnListResp, err error)

	GetReturn(ctx context.Context, returnID uint64) (res *orderdto.ReturnDetailResp, err error)

	ApproveReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error)

	ApproveReturnBatch(ctx context.Context, reqs []*orderdto.ReturnReviewReq) (err error)

	RejectReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error)

	RejectReturnBatch(ctx context.Context, reqs []*orderdto.ReturnReviewReq) (err error)

	ReceiveReturn(ctx context.Context, req *orderdto.ReturnReceiveReq) (res *orderdto.ReturnResp, err error)
}

type CustomerOrderSummaryReader interface {
	CustomerOrderSummaryOf(ctx context.Context, req *orderdto.CustomerOrderSummaryReq) (res *orderdto.CustomerOrderSummaryResp, err error)
}

type CustomerGrowthReader interface {
	CustomerGrowthByRange(ctx context.Context, req *orderdto.CustomerGrowthReq) (res *orderdto.CustomerGrowthResp, err error)
}

type CustomerSegmentReader interface {
	CustomerSegmentIDsByRange(ctx context.Context, req *orderdto.CustomerSegmentIDsReq) (res *orderdto.CustomerSegmentIDsResp, err error)
}

type CustomerRfmReader interface {
	CustomerRfmByRange(ctx context.Context, req *orderdto.CustomerRfmReq) (res *orderdto.CustomerRfmResp, err error)

	CustomerRfmSegmentIDsByRange(ctx context.Context, req *orderdto.CustomerRfmSegmentIDsReq) (res *orderdto.CustomerRfmSegmentIDsResp, err error)
}

type CustomerCohortReader interface {
	CustomerCohortByRange(ctx context.Context, req *orderdto.CustomerCohortReq) (res *orderdto.CustomerCohortResp, err error)
}

type OrderRangeSummaryReader interface {
	SummaryByRange(ctx context.Context, req *orderdto.OrderRangeSummaryReq) (res *orderdto.OrderRangeSummaryResp, err error)
}

type OrderSalesOverviewReader interface {
	SalesOverview(ctx context.Context, req *orderdto.SalesOverviewReq) (res *orderdto.SalesOverviewResp, err error)
}

type OrderOverviewReader interface {
	DailySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error)

	HourlySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error)

	TopProducts(ctx context.Context, req *orderdto.OrderTopProductsReq) (res *orderdto.OrderTopProductsResp, err error)

	StatusCounts(ctx context.Context, req *orderdto.OrderStatusCountsReq) (res *orderdto.OrderStatusCountsResp, err error)

	SoldQuantityByRange(ctx context.Context, req *orderdto.OrderSoldQuantityReq) (res *orderdto.OrderSoldQuantityResp, err error)
}

type OrderNoReader interface {
	GetOrderByNo(ctx context.Context, req *orderdto.GetOrderByNoReq) (res *orderdto.OrderResp, err error)
}

type OrderQueryReader interface {
	FindOrders(ctx context.Context, req *orderdto.FindOrderReq) (res *orderdto.FindOrderResp, err error)

	GetOrderDetailByNo(ctx context.Context, req *orderdto.GetOrderByNoReq) (res *orderdto.OrderDetailResp, err error)
}

type VisitorOrderReader interface {
	ListVisitorOrders(ctx context.Context, req *orderdto.VisitorOrderListReq) (res *orderdto.VisitorOrderListResp, err error)
	GetVisitorOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.VisitorOrderDetailResp, err error)
}

type CouponService interface {
	CreateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error)
	UpdateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error)
	ListCoupons(ctx context.Context, req *orderdto.CouponListReq) (res *orderdto.CouponListResp, err error)
	GetCoupon(ctx context.Context, couponID uint64) (res *orderdto.CouponResp, err error)
	DeleteCoupon(ctx context.Context, couponID uint64) (err error)
	ValidateCoupon(ctx context.Context, req *orderdto.CouponValidateReq) (res *orderdto.CouponValidateResp, err error)
	ListCouponRedemptions(ctx context.Context, req *orderdto.CouponRedemptionListReq) (res *orderdto.CouponRedemptionListResp, err error)

	AuditCouponCounts(ctx context.Context, req *orderdto.CouponCountAuditReq) (res *orderdto.CouponCountAuditResp, err error)
}

type (
	CreateOrderReq  = orderdto.CreateOrderReq
	CreateOrderResp = orderdto.CreateOrderResp
	OrderItemReq    = orderdto.OrderItemReq
	OrderAddress    = orderdto.OrderAddress

	PayOrderReq  = orderdto.PayOrderReq
	PayOrderResp = orderdto.PayOrderResp

	GetOrderByNoReq = orderdto.GetOrderByNoReq
	OrderResp       = orderdto.OrderResp

	Attribution = orderdto.Attribution
	FirstTouch  = orderdto.FirstTouch
	UTMInfo     = orderdto.UTMInfo
	AdInfo      = orderdto.AdInfo
	SessionInfo = orderdto.SessionInfo
	DeviceInfo  = orderdto.DeviceInfo
	TrailPage   = orderdto.TrailPage

	CustomerOrderSummaryReq  = orderdto.CustomerOrderSummaryReq
	CustomerOrderSummaryResp = orderdto.CustomerOrderSummaryResp
)

const ErrOrderNotFound = orderenums.ErrOrderNotFound

func OrderStatusLabel(status string) (key, fallback string) {
	return orderenums.OrderStatusLabel(status)
}

type StockOperator interface {
	DeductStock(ctx context.Context, in *StockDeduction) (err error)

	ChangeStock(ctx context.Context, in *StockAdjustment) (err error)

	DeductStockTx(ctx context.Context, tx *gorm.DB, in *StockDeduction) (err error)
	ChangeStockTx(ctx context.Context, tx *gorm.DB, in *StockAdjustment) (err error)

	ReserveStockTx(ctx context.Context, tx *gorm.DB, in *StockReservation) (err error)
	ReleaseStockTx(ctx context.Context, tx *gorm.DB, in *StockReservation) (err error)
}

type StockReservation struct {
	OrderID    int64
	ProjectID  string
	SourceType string
	SourceRef  string
	Remark     string

	Lines []StockLine
}

type StockLine struct {
	ProductID string

	VariantID string

	SKUCode string

	Quantity int

	WarehouseID string
}

type StockDeduction struct {
	ProjectID  string
	ReasonCode string

	SourceType string
	SourceRef  string
	Remark     string
	Lines      []StockLine
}

type StockAdjustment struct {
	ProjectID  string
	ReasonCode string
	SourceType string
	SourceRef  string
	Remark     string
	Lines      []StockLine
}

type StockRowLocator interface {
	LocateStockRow(ctx context.Context, projectID, variantID, warehouseID string) (stockID string, err error)
}

type CurrencyLister interface {
	ListCurrencyCodes(ctx context.Context) (codes []string, err error)
}

type WarehouseResolver interface {
	ResolveShipWarehouse(ctx context.Context, projectID, requested string) (warehouseID string, err error)
}

type ReturnWarehouseSource interface {
	ListReturnWarehouses(ctx context.Context, projectID string) ([]ReturnWarehouse, error)
}

type ReturnWarehouse struct {
	ID string

	Name string

	Code string

	Status string

	IsDefault bool
}
