// Package orderenums 订单模块的响应与错误消息（BIZ-1 销售侧）。
//
// 与 user 模块同口径：文案直接是中文常量（未接 i18n），展示层拿 UserFacingMessages
// 做白名单，不在表里的一律收口到 ErrInternal。
package orderenums

const (
	MsgCreateSuccess = "order.msg.createSuccess"
	MsgStatusChanged = "order.msg.statusChanged"
	MsgCancelled     = "order.msg.cancelled"
	// MsgCancelledStockWarning 已彻底删除（2026-09 事务收口 + 词条收口两批完成）。
	//
	// 它曾是「先提交状态、再动库存、失败写一条 Warnings 留痕」那条跨模块补偿路径的对外文案。
	// 事务收口后，取消订单的状态 / 流转 / 库存归还 / 券释放全在**同一个事务**里，归还失败即整体
	// 回滚并返回 error，不存在「订单已取消、库存没回来」这个中间态 —— 它没有任何可渲染的场景。
	//
	// 当时它只被废弃、没连词条一起删，原因是那个词条的 seed 在迁移 180，而 180 的存在性判定
	// 按「本批 key 计数且包含本 key」—— 单删词条行会让判定为假、下次启动又灌回来。
	// 后来 180_i18n_seed_order.sql 的两行、register_admin_i18n.go 的判定 key 列表与门槛
	//（>=62 → >=61）已同批改掉，常量与词条这才一起删净。
	// 教训：**删能力要连 seed 的判定一起收口**，否则「删了」只是看起来删了。
	// 同类回归由 public/migrations/register_retired_permission_test.go 在权限点维度兜底。
	MsgRefunded    = "order.msg.refunded"
	MsgPaid        = "order.msg.paid"
	MsgNoteUpdated = "order.msg.noteUpdated"
)

// 参数与校验。
const (
	ErrInvalidParam          = "order.err.invalidParam"
	ErrProjectRequired       = "order.err.projectRequired"
	ErrItemsRequired         = "order.err.itemsRequired"
	ErrItemLimitExceeded     = "order.err.itemLimitExceeded"
	ErrQuantityInvalid       = "order.err.quantityInvalid"
	ErrCustomerEmailRequired = "order.err.customerEmailRequired"
	ErrCustomerEmailInvalid  = "order.err.customerEmailInvalid"
	ErrOrderNoInvalid        = "order.err.orderNoInvalid"
	ErrStatusInvalid         = "order.err.statusInvalid"
	// ErrNoteTooLong 备注超长：直接拒绝而不是静默截断 —— 截断会让运营以为写进去了。
	ErrNoteTooLong = "order.err.noteTooLong"
)

// 订单本体。
const (
	ErrOrderNotFound = "order.err.orderNotFound"
	ErrOrderNoTaken  = "order.err.orderNoTaken"
	// ErrOrderHasNoItems 「有头无项」的订单是脏数据：头能建出来说明事务写坏了。
	ErrOrderHasNoItems = "order.err.orderHasNoItems"
)

// 状态机。
const (
	// ErrStatusTransition 通用非法流转（具体方向由 ErrXxxNotYyy 给更准的话）。
	ErrStatusTransition     = "order.err.statusTransition"
	ErrOrderNotCancellable  = "order.err.orderNotCancellable"
	ErrOrderNotRefundable   = "order.err.orderNotRefundable"
	ErrAlreadyCancelled     = "order.err.alreadyCancelled"
	ErrAlreadyRefunded      = "order.err.alreadyRefunded"
	ErrCancelReasonRequired = "order.err.cancelReasonRequired"
)

// 支付。
const (
	// ErrPaymentMethodRequired 没有支付方式的「已付款」订单无法对账 ——
	// 账上多了一笔钱，却不知道它从哪条通道进来。
	ErrPaymentMethodRequired = "order.err.paymentMethodRequired"
	// ErrPaymentChannelFailed 支付通道失败：钱没扣成，订单留在待付款，可重试。
	ErrPaymentChannelFailed = "order.err.paymentChannelFailed"
)

// 退货入库（RMA）。
const (
	MsgReturnRequested = "order.msg.returnRequested"
	MsgReturnApproved  = "order.msg.returnApproved"
	MsgReturnRejected  = "order.msg.returnRejected"
	MsgReturnReceived  = "order.msg.returnReceived"
	MsgReturnCancelled = "order.msg.returnCancelled"

	ErrReturnNotFound             = "order.err.returnNotFound"
	ErrReturnItemsRequired        = "order.err.returnItemsRequired"
	ErrReturnQuantityInvalid      = "order.err.returnQuantityInvalid"
	ErrReturnQuantityExceeded     = "order.err.returnQuantityExceeded"
	ErrReturnReasonRequired       = "order.err.returnReasonRequired"
	ErrReturnNotCancellable       = "order.err.returnNotCancellable"
	ErrReturnOrderNotReturnable   = "order.err.returnOrderNotReturnable"
	ErrReturnNotReviewable        = "order.err.returnNotReviewable"
	ErrReturnNotReceivable        = "order.err.returnNotReceivable"
	ErrReturnRejectReasonRequired = "order.err.returnRejectReasonRequired"
)

// 商品与库存。
const (
	ErrVariantNotFound   = "order.err.variantNotFound"
	ErrStockInsufficient = "order.err.stockInsufficient"
	ErrStockUnavailable  = "order.err.stockUnavailable"
)

// ErrInternal 未归类的系统错误对外统一文案。
//
// 存在的理由同 user 模块：基础设施错误（数据库 / Redis）的原文可能带表名、列名甚至
// SQL 片段，那是给运维看的，不是给访客看的。展示层据此把非本模块业务文案全部落到这一条。
const ErrInternal = "操作失败，请稍后重试"

// UserFacingMessages 可以原样展示给访客的全部业务文案（白名单）。
//
// 新增面向访客的订单错误文案时必须同步加到这里，否则页面只会显示 ErrInternal。
var UserFacingMessages = []string{
	MsgCreateSuccess, MsgStatusChanged, MsgCancelled, MsgRefunded, MsgPaid,
	ErrInvalidParam, ErrProjectRequired, ErrItemsRequired, ErrItemLimitExceeded,
	ErrQuantityInvalid, ErrCustomerEmailRequired, ErrCustomerEmailInvalid,
	ErrOrderNoInvalid, ErrStatusInvalid,
	ErrOrderNotFound, ErrOrderNoTaken, ErrOrderHasNoItems,
	ErrStatusTransition, ErrOrderNotCancellable, ErrOrderNotRefundable,
	ErrAlreadyCancelled, ErrAlreadyRefunded, ErrCancelReasonRequired,
	ErrPaymentMethodRequired, ErrPaymentChannelFailed,
	ErrVariantNotFound, ErrStockInsufficient, ErrStockUnavailable,
	// 优惠码：结算链路会把它们直接显示给访客。
	ErrCouponNotFound, ErrCouponCodeRequired, ErrCouponDisabled,
	ErrCouponNotStarted, ErrCouponExpired, ErrCouponExhausted,
	ErrCouponUserLimit, ErrCouponMinSubtotal,
	// 退货申请：访客在订单里点「申请退货」时会看到这些。
	MsgReturnRequested, MsgReturnCancelled,
	ErrReturnItemsRequired, ErrReturnQuantityInvalid, ErrReturnQuantityExceeded,
	ErrReturnReasonRequired, ErrReturnNotCancellable, ErrReturnOrderNotReturnable,
	ErrInternal,
}

// 优惠码。
const (
	MsgCouponCreated = "order.msg.couponCreated"
	MsgCouponUpdated = "order.msg.couponUpdated"
	MsgCouponDeleted = "order.msg.couponDeleted"

	ErrCouponNotFound      = "order.err.couponNotFound"
	ErrCouponCodeRequired  = "order.err.couponCodeRequired"
	ErrCouponCodeTaken     = "order.err.couponCodeTaken"
	ErrCouponTypeInvalid   = "order.err.couponTypeInvalid"
	ErrCouponValueInvalid  = "order.err.couponValueInvalid"
	ErrCouponWindowInvalid = "order.err.couponWindowInvalid"
	ErrCouponDisabled      = "order.err.couponDisabled"
	ErrCouponNotStarted    = "order.err.couponNotStarted"
	ErrCouponExpired       = "order.err.couponExpired"
	ErrCouponExhausted     = "order.err.couponExhausted"
	ErrCouponUserLimit     = "order.err.couponUserLimit"
	ErrCouponMinSubtotal   = "order.err.couponMinSubtotal"
	ErrCouponInUse         = "order.err.couponInUse"
)
