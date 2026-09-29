// Package orderenums 订单模块的响应与错误消息（BIZ-1 销售侧）。
//
// 与 user 模块同口径：文案直接是中文常量（未接 i18n），展示层拿 UserFacingMessages
// 做白名单，不在表里的一律收口到 ErrInternal。
package orderenums

import "strings"

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

// 批量操作的结论文案（页面回执）。
//
// 与 admin 的 Bulk* 同口径：值 = sys_i18n 的 item_key，**不带 Err / Msg 前缀** ——
// 这几句不是 service 错误（不进 UserFacingMessages 错误白名单），而是 handler 按计数
// 拼出的整句回执（?done= 通道）。动词与名词也各自是一条词条：中文模板里它们是 %s 占位，
// 英文语序与中文不同，把动词/名词焊进模板就等于按「3 名词 × 7 动词 × 4 分支」抄一份句子表，
// 既没法复用也无法逐条翻译。
//
// 中文原文留在 inbound/http/order_page_query.go（与写读共用的那份结构体绑在一起）。
const (
	BulkNoneSelected = "order.bulk.noneSelected" // 没有勾选任何%s。
	BulkAllDone      = "order.bulk.allDone"      // %s %s 个%s。
	BulkAllSkipped   = "order.bulk.allSkipped"   // 0 个%s%s，%s 个被跳过（…）。
	BulkPartial      = "order.bulk.partial"      // %s %s 个%s，跳过 %s 个（…）。

	BulkVerbFlowed    = "order.bulk.verb.flowed"    // 已流转
	BulkVerbCancelled = "order.bulk.verb.cancelled" // 已取消
	BulkVerbApproved  = "order.bulk.verb.approved"  // 已同意
	BulkVerbRejected  = "order.bulk.verb.rejected"  // 已拒绝
	BulkVerbDeleted   = "order.bulk.verb.deleted"   // 已删除
	BulkVerbDisabled  = "order.bulk.verb.disabled"  // 已停用
	BulkVerbEnabled   = "order.bulk.verb.enabled"   // 已启用

	BulkNounOrder  = "order.bulk.noun.order"  // 订单
	BulkNounReturn = "order.bulk.noun.return" // 退货申请
	BulkNounCoupon = "order.bulk.noun.coupon" // 优惠码

	// BulkCouponTargetInvalid 批量启停优惠码时目标状态非法（走 ?done= 的参数级回执）。
	BulkCouponTargetInvalid = "order.bulk.couponTargetInvalid"
	// BulkCancelReasonRequired 批量取消缺原因：整批不处理，回一句可展示的提示。
	BulkCancelReasonRequired = "order.bulk.cancelReasonRequired"
	// BulkReturnRejectReasonRequired 批量拒绝退货缺理由（与订单侧同口径）。
	BulkReturnRejectReasonRequired = "order.bulk.returnRejectReasonRequired"
)

// —— 后台页面展示用的枚举标签（枚举 → 展示名的唯一真源）——
//
// 形态统一是 (key, fallback) 两个返回值，调用点 tr(key, fallback)：
// 只给中文 → 英文界面恒中文；只给 key → 词条缺失时页面显示裸 key
// （`admin.orders.status.paid`），两个都给 → 命中出译文、未命中出中文兜底。
//
// 为什么放 enums 而不是 inbound/http：同一份映射有三个消费方（订单页、后台客户页的
// 「最近一单」、访客片段），各写一份的结果是「改一处、另两处静默留在旧说法上」。
// enums 零依赖，任何层都能 import。

// OrderStatusKeyPrefix 订单状态的词条 key 前缀。
//
// **复用访客面片段那套 `site.fragment.order.status.*`**（迁移 157 seed，六个状态中英成对）：
// 后台没有独立的订单状态词条，而 sys_i18n 是一张全局语言表（不存在「哪个面不能用哪个 key」）。
// 代价是这层跨面耦合：访客面若删除该词条，后台会静默回落到中文兜底（页面仍可用）。
const OrderStatusKeyPrefix = "site.fragment.order.status."

// OrderStatusLabel 订单状态 → (词条 key, 中文兜底)。
//
// 空状态返回空 key + 「—」：那是「没有这一单」，不是一个状态；
// 认不出的取值返回空 key + 原值（宁可显示生值，也不显示空白 —— 手改 URL 带来的怪值照样说得清）。
func OrderStatusLabel(status string) (key, fallback string) {
	switch strings.TrimSpace(status) {
	case "pending":
		return OrderStatusKeyPrefix + "pending", "待付款"
	case "paid":
		return OrderStatusKeyPrefix + "paid", "已付款"
	case "shipped":
		return OrderStatusKeyPrefix + "shipped", "已发货"
	case "completed":
		return OrderStatusKeyPrefix + "completed", "已完成"
	case "cancelled":
		return OrderStatusKeyPrefix + "cancelled", "已取消"
	case "refunded":
		return OrderStatusKeyPrefix + "refunded", "已退款"
	case "":
		return "", "—"
	default:
		return "", status
	}
}

// 退货申请状态的词条 key（后台退货页与访客片段共用）。
const (
	ReturnStatusKeyRequested = "admin.returns.status.requested"
	ReturnStatusKeyApproved  = "admin.returns.status.approved"
	ReturnStatusKeyReceived  = "admin.returns.status.received"
	ReturnStatusKeyCompleted = "admin.returns.status.completed"
	ReturnStatusKeyRejected  = "admin.returns.status.rejected"
	ReturnStatusKeyCancelled = "admin.returns.status.cancelled"
)

// ReturnStatusLabel 退货申请状态 → (词条 key, 中文兜底)。
//
// 取值与 order 模块的 return 状态机一致（requested / approved / received /
// completed / rejected / cancelled），认不出的取值原样回显。
func ReturnStatusLabel(status string) (key, fallback string) {
	switch strings.TrimSpace(status) {
	case "requested":
		return ReturnStatusKeyRequested, "待审核"
	case "approved":
		return ReturnStatusKeyApproved, "待收货"
	case "received":
		return ReturnStatusKeyReceived, "待退款"
	case "completed":
		return ReturnStatusKeyCompleted, "已完成"
	case "rejected":
		return ReturnStatusKeyRejected, "已拒绝"
	case "cancelled":
		return ReturnStatusKeyCancelled, "已撤销"
	case "":
		return "", "—"
	default:
		return "", status
	}
}

// 优惠码的**展示口径状态**（由生效时间与已用次数算出来，不是 coupons.status 列）。
//
// 光看 status 列分不出「启用但是已过期」与「正在生效」：过期、未开始、用尽都是时间的函数，
// 单独存一列必然与真实状态不同步（要靠定时任务去刷，而定时任务总有停的时候）。
// 因此 service 只输出这里的**口径值**，文案由展示层按词条渲染 ——
// 展示层再拿中文标签去反查样式表（旧实现）会让「改一句词条就让徽章静默失效」。
const (
	CouponStateEnabled    = "enabled"
	CouponStateDisabled   = "disabled"
	CouponStateExpired    = "expired"
	CouponStateNotStarted = "not_started"
	CouponStateExhausted  = "exhausted"
)

// CouponStateLabel 优惠码口径状态 → (词条 key, 中文兜底)。
func CouponStateLabel(state string) (key, fallback string) {
	switch strings.TrimSpace(state) {
	case CouponStateEnabled:
		return "admin.coupons.status.enabled", "生效中"
	case CouponStateDisabled:
		return "admin.coupons.status.disabled", "已停用"
	case CouponStateExpired:
		return "admin.coupons.status.expired", "已过期"
	case CouponStateNotStarted:
		return "admin.coupons.status.not_started", "未开始"
	case CouponStateExhausted:
		return "admin.coupons.status.exhausted", "已用完"
	case "":
		return "", "—"
	default:
		return "", state
	}
}

// —— 点分 key 常量（新式）——
//
// 值是 sys_i18n 的 item_key（文案真源在迁移 451），命名按「去掉 `admin.` 模块前缀
// 后的语义路径」：包名 orderenums 已给出模块上下文。
//
// 与上面的 OrderStatusKey* / ReturnStatusKey* 分开成组：那批是「key 前缀常量」
// （调用方自己拼后缀），本组是完整 item_key。中文兜底留在调用点。
const (
	// OrderNewOptionAvailable 代客建单页候选变体行的可用量标注模板（%s = 可用数）。
	OrderNewOptionAvailable = "admin.order_new.option.available" // 可用 %s
)
