// Package orderenums 订单模块的响应与错误消息（BIZ-1 销售侧）。
//
// 错误文案走**声明处**这一条链：每条 Err* 在 errFallbacks 里有中文兜底，
// service 用 Err(key) / 默认用 Err* 常量直接构造，出口只调一次 facing.FactOf。
// 值本身是 sys_i18n 的 item_key，词条在场时出口给译文、缺席时退回这里的兜底 ——
// 所以客户端永远不会看到 `order.err.xxx` 这样的裸 key。
package orderenums

import (
	"strings"

	"go_wp/pkg/facing"
)

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

// 发货（迁移 627 / 625，S2）。
//
// 发货是一条**有单据**的事实：状态、承运商、运单号、库存扣减、流水全部同事务落库。
// 五条拒绝理由对应五类「这一步不该发生」的情形，状态码在声明处给（409 状态 / 超量 /
// 幂等键冲突，400 缺明细 / 缺仓）—— 全部压成 400 的代价是前端与监控分不清
// 「输错了」与「这条路现在不通」。
const (
	ErrOrderNotShippable      = "order.err.orderNotShippable"
	ErrShipQuantityExceeded   = "order.err.shipQuantityExceeded"
	ErrShipLinesRequired      = "order.err.shipLinesRequired"
	ErrShipRequestKeyConflict = "order.err.shipRequestKeyConflict"
	ErrShipWarehouseRequired  = "order.err.shipWarehouseRequired"
	// ErrCarrierNotFound 承运商不存在（发货表单选了一个已被删掉的承运商）。
	ErrCarrierNotFound  = "order.err.carrierNotFound"
	ErrCarrierCodeTaken = "order.err.carrierCodeTaken"
	// ErrCarrierTemplateInvalid 跟踪页模板里有 {tracking_no} 之外的占位符。
	ErrCarrierTemplateInvalid = "order.err.carrierTemplateInvalid"
	ErrCarrierStatusInvalid   = "order.err.carrierStatusInvalid"
)

// ErrInternal 未归类的系统错误对外统一文案。
//
// 存在的理由同 user 模块：基础设施错误（数据库 / Redis）的原文可能带表名、列名甚至
// SQL 片段，那是给运维看的，不是给访客看的。展示层据此把非本模块业务文案全部落到这一条。
const ErrInternal = "操作失败，请稍后重试"

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。为什么要这张表：
// 词条库读不到时（迁移没跑 / 库不可用），businessText 只能退回声明处给的那句话 ——
// 没有它，客户端拿到的就是 `order.err.orderNotFound` 这样的裸 key。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
// 对账由 order_enums_test.go 的双向断言守（新增 Err* 没给兜底要红；表里留着已删的 key 也要红）。
var errFallbacks = map[string]string{
	// 发货与承运商（迁移 627-629，S1-S3）：这些 key 由本批引入，兜底必须同批给 ——
	// 缺兜底时 facing.FactOf 取不到原文，界面上会出现裸 key。
	ErrCarrierCodeTaken:           "该承运商编码已被占用",
	ErrCarrierNotFound:            "承运商不存在",
	ErrCarrierStatusInvalid:       "承运商状态不合法",
	ErrCarrierTemplateInvalid:     "承运商模板配置不合法",
	ErrShipWarehouseRequired:      "发货必须指定发货仓",
	ErrShipLinesRequired:          "发货单至少需要一行商品",
	ErrShipQuantityExceeded:       "发货数量超出可发数量",
	ErrShipRequestKeyConflict:     "该发货请求已处理过（请求键冲突）",
	ErrOrderNotShippable:          "当前订单状态不允许发货",
	ErrInternal:                   "操作失败，请稍后重试",
	ErrInvalidParam:               "参数不合法",
	ErrProjectRequired:            "缺少站点工程",
	ErrItemsRequired:              "订单至少需要一个商品项",
	ErrItemLimitExceeded:          "单笔订单的商品项超出上限",
	ErrQuantityInvalid:            "商品数量必须为正整数",
	ErrCustomerEmailRequired:      "请填写客户邮箱",
	ErrCustomerEmailInvalid:       "客户邮箱格式不正确",
	ErrOrderNoInvalid:             "订单号格式不合法",
	ErrStatusInvalid:              "订单状态取值不合法",
	ErrNoteTooLong:                "备注内容过长",
	ErrOrderNotFound:              "订单不存在",
	ErrOrderNoTaken:               "订单号已被占用",
	ErrOrderHasNoItems:            "订单缺少商品项，数据不完整",
	ErrStatusTransition:           "订单当前状态不支持该操作",
	ErrOrderNotCancellable:        "只有未发货的订单可以取消",
	ErrOrderNotRefundable:         "只有已付款的订单可以退款",
	ErrAlreadyCancelled:           "订单已取消，不能再操作",
	ErrAlreadyRefunded:            "订单已退款，不能再操作",
	ErrCancelReasonRequired:       "请填写取消原因",
	ErrPaymentMethodRequired:      "缺少支付方式",
	ErrPaymentChannelFailed:       "支付未成功，请稍后重试",
	ErrReturnNotFound:             "退货申请不存在",
	ErrReturnItemsRequired:        "请至少选择一件要退的商品",
	ErrReturnQuantityInvalid:      "退货数量必须是正整数",
	ErrReturnQuantityExceeded:     "退货数量超过可退数量",
	ErrReturnReasonRequired:       "请说明退货原因",
	ErrReturnNotCancellable:       "该申请已进入处理流程，无法撤销",
	ErrReturnOrderNotReturnable:   "该订单当前状态不支持退货",
	ErrReturnNotReviewable:        "该申请不在待审核状态",
	ErrReturnNotReceivable:        "该申请不在待收货状态（或已完成）",
	ErrReturnRejectReasonRequired: "请填写拒绝原因",
	ErrVariantNotFound:            "商品规格不存在或已下架",
	ErrStockInsufficient:          "库存不足，无法下单",
	ErrStockUnavailable:           "库存服务暂时不可用，请稍后重试",
	ErrCouponNotFound:             "优惠码不存在",
	ErrCouponCodeRequired:         "请填写优惠码",
	ErrCouponCodeTaken:            "这个优惠码已经存在",
	ErrCouponTypeInvalid:          "优惠类型只支持按比例折扣或固定金额",
	ErrCouponValueInvalid:         "优惠值不合法",
	ErrCouponWindowInvalid:        "生效时间不合法",
	ErrCouponDisabled:             "优惠码已停用",
	ErrCouponNotStarted:           "优惠码尚未开始",
	ErrCouponExpired:              "优惠码已过期",
	ErrCouponExhausted:            "优惠码已被用完",
	ErrCouponUserLimit:            "你已使用过该优惠码",
	ErrCouponMinSubtotal:          "订单金额未达到该优惠码的使用门槛",
	ErrCouponInUse:                "该优惠码已有核销记录，不能删除",
}

// Err 声明一条自带对外文案的订单业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

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
	// 发货：后台发货表单与「待发货」页会把它们直接展示给操作者。
	ErrOrderNotShippable, ErrShipQuantityExceeded, ErrShipLinesRequired,
	ErrShipWarehouseRequired, ErrShipRequestKeyConflict,
	ErrCarrierNotFound, ErrCarrierCodeTaken, ErrCarrierTemplateInvalid, ErrCarrierStatusInvalid,
	// 优惠码：**后台代客建单页与 /api/order/create 会把它们直接透出给操作者**。
	//
	// 口径修正（BIZ-10）：这批文案此前写着「结算链路会把它们直接显示给访客」，
	// 而事实是**访客结算链路没有券入口** —— CartCheckoutReq 不带 couponCode、
	// 结算片段也不解析它，因此 resolveCoupon 恒返回 (nil, 0)，这些文案在访客侧
	// 到不了任何页面。补上券入口时它们已经是可以直接展示的（无需新增文案）。
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

// OrderStatusTone 订单状态 → 徽标分档（ok / warn / danger / mute / info）。
//
// 放在 enums、与 OrderStatusLabel 并列：**同一个状态在列表行、详情头与筛选徽章上必须是同一档**——
// 原先这套映射在 orders.html 里逐行写了三遍（筛选行 / 列表行 / 详情头），
// 加一个状态要改三处，漏一处就是「同一单在两处颜色不一样」。
//
// 分档口径：待付款 → warn（在等钱）；已付款 / 已发货 → info（在流程里、不需要人工动作）；
// 已完成 → ok；已退款 → danger（钱退回去了，是异常收尾）；已取消 / 认不出的值 → mute。
func OrderStatusTone(status string) string {
	switch strings.TrimSpace(status) {
	case "pending":
		return "warn"
	case "paid", "shipped":
		return "info"
	case "completed":
		return "ok"
	case "refunded":
		return "danger"
	default:
		return "mute"
	}
}

// CreatedViaKeyPrefix 订单来源的词条 key 前缀。
//
// 复用迁移 448 seed 的三个词条（admin.orders.created_via.*）—— 详情块里
// orders.html:218 早就在用它们，这里只是把「三分支写死在模板里」收敛成一张事实表，
// 让筛选条与列表行用同一份文案。
const CreatedViaKeyPrefix = "admin.orders.created_via."

// CreatedViaLabel 订单来源 → (词条 key, 中文兜底)。
//
// checkout = 访客在站点结算下单；admin = 后台代客建单；api = 外部系统经接口建单。
func CreatedViaLabel(v string) (key, fallback string) {
	switch strings.TrimSpace(v) {
	case "checkout":
		return CreatedViaKeyPrefix + "checkout", "访客结算"
	case "admin":
		return CreatedViaKeyPrefix + "admin", "后台自建"
	case "api":
		return CreatedViaKeyPrefix + "api", "接口"
	case "":
		return "", "—"
	default:
		return "", v
	}
}

// FulfillmentKeyPrefix 履约状态的词条 key 前缀（迁移 654 seed，12 个状态中英成对）。
//
// 与订单状态（site.fragment.order.status.*）分开：履约状态只在后台出现，
// 访客面拿不到它，也没有「货到哪了」的公开口径可复用。
const FulfillmentKeyPrefix = "admin.orders.fulfillment."

// FulfillmentStatuses 履约状态的有序清单（顺序 = 筛选行徽章从左到右的顺序）。
//
// 放在 enums 而不是 model：控制器只能 import contract/dto/enums（分层判据禁止
// inbound 依赖 model），而筛选行、计数补零、事实表补键三处都要这份清单。
// 取值本身与 ordermodel 的 Fulfillment* 常量一一对应，错一个会在列表上显示生值。
var FulfillmentStatuses = []string{
	"pending", "paid", "partial", "shipped", "received", "returned",
	"return_done", "reship", "exception", "failed", "cancelled", "refunded",
}

// FulfillmentLabel 履约状态 → (词条 key, 中文兜底)。
//
// 取值集合与 ordermodel 的 Fulfillment* 常量一一对应；那边是数据层的真源，
// 这里是**文案**的真源 —— 两处都是 switch，但改一处忘了另一处会立刻在列表上看得见
// （显示生值而不是静默错文案）。
func FulfillmentLabel(v string) (key, fallback string) {
	switch strings.TrimSpace(v) {
	case "pending":
		return FulfillmentKeyPrefix + "pending", "待履约"
	case "paid":
		return FulfillmentKeyPrefix + "paid", "待发货"
	case "partial":
		return FulfillmentKeyPrefix + "partial", "部分发货"
	case "shipped":
		return FulfillmentKeyPrefix + "shipped", "已发货"
	case "received":
		return FulfillmentKeyPrefix + "received", "已收货"
	case "returned":
		return FulfillmentKeyPrefix + "returned", "已退货"
	case "return_done":
		return FulfillmentKeyPrefix + "return_done", "退货完成"
	case "reship":
		return FulfillmentKeyPrefix + "reship", "换货补发"
	case "exception":
		return FulfillmentKeyPrefix + "exception", "履约异常"
	case "failed":
		return FulfillmentKeyPrefix + "failed", "履约失败"
	case "cancelled":
		return FulfillmentKeyPrefix + "cancelled", "已取消"
	case "refunded":
		return FulfillmentKeyPrefix + "refunded", "已退款"
	case "":
		return "", "—"
	default:
		return "", v
	}
}

// FulfillmentTone 履约状态 → 徽标分档（ok / warn / danger / mute / info）。
//
// 分档口径是**仓库视角的待办强度**：
//   - pending / paid → warn：还欠客户货，仓库要动手；
//   - partial / reship → info：在处理中（补发是人为决定，不是故障）；
//   - shipped / received / return_done → ok：这条线走完了；
//   - returned → info：货回仓了，但钱可能还没退；
//   - exception / failed → danger：需要人工介入；
//   - cancelled / refunded → mute：不再需要仓库动作。
func FulfillmentTone(v string) string {
	switch strings.TrimSpace(v) {
	case "pending", "paid":
		return "warn"
	case "partial", "reship", "returned":
		return "info"
	case "shipped", "received", "return_done":
		return "ok"
	case "exception", "failed":
		return "danger"
	default:
		return "mute"
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

// ReturnStatusTone 退货申请状态 → 徽标分档。
//
// 待审核 / 待收货 / 待退款 都是**有人在等着处理**的中间态 → warn（这三档要在长列表里抢到注意力）；
// 已完成 → ok；已拒绝 / 已撤销 / 认不出的值 → mute（终态、不需要动作）。
func ReturnStatusTone(status string) string {
	switch strings.TrimSpace(status) {
	case "requested", "approved", "received":
		return "warn"
	case "completed":
		return "ok"
	default:
		return "mute"
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

// CouponStateTone 优惠码口径状态 → 徽标分档。
//
// 生效中 → ok；已过期 / 已用完 → danger（这两档最容易在客服侧答错：「券还在列表里」不等于能用）；
// 未开始 → warn（还没到时间）；已停用 / 认不出的值 → mute。
func CouponStateTone(state string) string {
	switch strings.TrimSpace(state) {
	case CouponStateEnabled:
		return "ok"
	case CouponStateExpired, CouponStateExhausted:
		return "danger"
	case CouponStateNotStarted:
		return "warn"
	default:
		return "mute"
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

	// 后台页面标题（layout 的 <title>，adminui.Prepare 按当前语言取词）。
	CouponsHeading  = "admin.coupons.heading"   // 优惠码
	OrdersHeading   = "admin.orders.heading"    // 订单
	ReturnsHeading  = "admin.returns.heading"   // 退货
	OrderSalesTitle = "admin.order.sales.title" // 销售概览
	// 发货三屏的页面标题（迁移 627 / 625，S3）。
	PendingShipTitle   = "admin.order.pendingShip.title" // 待发货
	OrderCarriersTitle = "admin.order.carriers.title"    // 承运商

	// 券页表单校验的展示槽文案（key + 中文兜底的事实，交给 adminui.MsgText）。
	CouponFormInvalidID      = "admin.coupons.form.invalid_id"
	CouponFormForeignProject = "admin.coupons.form.foreign_project"

	// 代客建单页国家/地区下拉的「（不填写）」空项。
	OrderNewFieldCountryNone = "admin.order_new.field.country_none"
)

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
