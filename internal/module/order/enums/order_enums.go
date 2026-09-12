// Package orderenums 订单模块的响应与错误消息（BIZ-1 销售侧）。
//
// 与 user 模块同口径：文案直接是中文常量（未接 i18n），展示层拿 UserFacingMessages
// 做白名单，不在表里的一律收口到 ErrInternal。
package orderenums

const (
	MsgCreateSuccess = "订单已创建"
	MsgStatusChanged = "订单状态已更新"
	MsgCancelled     = "订单已取消"
	MsgRefunded      = "订单已退款"
)

// 参数与校验。
const (
	ErrInvalidParam          = "参数不合法"
	ErrProjectRequired       = "缺少站点工程"
	ErrItemsRequired         = "订单至少需要一个商品项"
	ErrItemLimitExceeded     = "单笔订单的商品项超出上限"
	ErrQuantityInvalid       = "商品数量必须为正整数"
	ErrCustomerEmailRequired = "请填写客户邮箱"
	ErrCustomerEmailInvalid  = "客户邮箱格式不正确"
	ErrOrderNoInvalid        = "订单号格式不合法"
	ErrStatusInvalid         = "订单状态取值不合法"
)

// 订单本体。
const (
	ErrOrderNotFound = "订单不存在"
	ErrOrderNoTaken  = "订单号已被占用"
	// ErrOrderHasNoItems 「有头无项」的订单是脏数据：头能建出来说明事务写坏了。
	ErrOrderHasNoItems = "订单缺少商品项，数据不完整"
)

// 状态机。
const (
	// ErrStatusTransition 通用非法流转（具体方向由 ErrXxxNotYyy 给更准的话）。
	ErrStatusTransition     = "订单当前状态不支持该操作"
	ErrOrderNotCancellable  = "只有未发货的订单可以取消"
	ErrOrderNotRefundable   = "只有已付款的订单可以退款"
	ErrAlreadyCancelled     = "订单已取消，不能再操作"
	ErrAlreadyRefunded      = "订单已退款，不能再操作"
	ErrCancelReasonRequired = "请填写取消原因"
)

// 商品与库存。
const (
	ErrVariantNotFound   = "商品规格不存在或已下架"
	ErrStockInsufficient = "库存不足，无法下单"
	ErrStockUnavailable  = "库存服务暂时不可用，请稍后重试"
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
	MsgCreateSuccess, MsgStatusChanged, MsgCancelled, MsgRefunded,
	ErrInvalidParam, ErrProjectRequired, ErrItemsRequired, ErrItemLimitExceeded,
	ErrQuantityInvalid, ErrCustomerEmailRequired, ErrCustomerEmailInvalid,
	ErrOrderNoInvalid, ErrStatusInvalid,
	ErrOrderNotFound, ErrOrderNoTaken, ErrOrderHasNoItems,
	ErrStatusTransition, ErrOrderNotCancellable, ErrOrderNotRefundable,
	ErrAlreadyCancelled, ErrAlreadyRefunded, ErrCancelReasonRequired,
	ErrVariantNotFound, ErrStockInsufficient, ErrStockUnavailable,
	ErrInternal,
}
