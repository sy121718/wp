package orderenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	OrderCancel = "order:cancel"
	OrderCarrierCreate = "order:carrier_create"
	OrderCarrierList = "order:carrier_list"
	OrderCarrierManage = "order:carrier_manage"
	OrderCouponCountAudit = "order:coupon_count_audit"
	OrderCouponCreate = "order:coupon_create"
	OrderCouponDelete = "order:coupon_delete"
	OrderCouponGet = "order:coupon_get"
	OrderCouponList = "order:coupon_list"
	OrderCouponRedemption = "order:coupon_redemption"
	OrderCouponUpdate = "order:coupon_update"
	OrderCouponValidate = "order:coupon_validate"
	OrderCreate = "order:create"
	OrderCurrencyRateGet = "order:currency_rate_get"
	OrderCurrencyRateSet = "order:currency_rate_set"
	OrderGet = "order:get"
	OrderItemList = "order:item_list"
	OrderList = "order:list"
	OrderLogList = "order:log_list"
	OrderNote = "order:note"
	OrderOverview = "order:overview"
	OrderPendingShip = "order:pending_ship"
	OrderRefund = "order:refund"
	OrderReturnApprove = "order:return_approve"
	OrderReturnGet = "order:return_get"
	OrderReturnList = "order:return_list"
	OrderReturnReceive = "order:return_receive"
	OrderReturnReject = "order:return_reject"
	OrderShip = "order:ship"
	OrderShipForm = "order:ship_form"
	OrderShipmentList = "order:shipment_list"
	OrderStatus = "order:status"
)
