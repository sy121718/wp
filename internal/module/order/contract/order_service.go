// Package ordercontract 订单模块对外契约（BIZ-1 销售侧）。
//
// 文件分三段，改之前先看清自己在哪一段：
//
//	· 对外能力 —— 别的模块能用订单做什么（OrderService 与它嵌的收窄子接口）。
//	  收窄是越权防护手段：片段层只拿得到 VisitorReturnPort，抄不走审核 / 入库 / 退款。
//	· 跨模块形状 —— 调用方要传进来、要收回去的类型：dto 重导出 + 展示标签。
//	  重导出是为了让调用方只依赖本包（取舍见该段注释）。
//	· 索要的端口 —— 订单需要外部给什么（库存扣减、退货仓库下拉），**由对方实现**
//	  （库存侧 outbound/orderstock）；入参形状在这里自有，订单不 import 库存的 dto ——
//	  否则订单就认识了库存的绑定层（审计 CQ-004）。
package ordercontract

import (
	"context"

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
)

// ==========================================================================
// 对外能力：别的模块能用订单做什么
// ==========================================================================

// OrderService 订单模块对外能力。
type OrderService interface {
	// VisitorOrderReader 访客自助查询（只有两条只读方法，user_id 钉死在契约里）。
	VisitorOrderReader
	// OrderNoReader 按商户单号取单（支付回调链路的第一步）。
	OrderNoReader
	// CouponService 优惠码：后台管理 + 结算试算。
	CouponService
	// ReturnService 退货入库（RMA）：客户申请 → 审核 → 先入库后退款。
	ReturnService
	// CustomerOrderSummaryReader 后台客户管理页的订单摘要（只读，一条方法）。
	CustomerOrderSummaryReader
	// CustomerGrowthReader 区间客户增长（只读，一条方法）。
	CustomerGrowthReader
	// CustomerSegmentReader 按分段取客户 id（只读，一条方法）。
	CustomerSegmentReader
	// OrderRangeSummaryReader 概览页 KPI 与只读聚合的区间摘要（只读，一条方法）。
	OrderRangeSummaryReader
	// OrderOverviewReader 概览页的其余只读聚合：按天趋势 / 热销榜 / 状态计数。
	OrderOverviewReader

	// CreateOrder 访客结算建单，来源固定 checkout。
	CreateOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)
	// CreateAPIOrder 站点 API 建单，来源固定 api。
	CreateAPIOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)
	// CreateAdminOrder 后台代客建单，来源固定 admin。
	CreateAdminOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)
	// GetOrder 订单详情（头 + 订单项 + 状态流转链）。
	GetOrder(ctx context.Context, req *orderdto.GetOrderReq) (res *orderdto.OrderDetailResp, err error)
	// ListOrders 订单列表 + 各状态计数。
	ListOrders(ctx context.Context, req *orderdto.ListOrderReq) (res *orderdto.OrderListResp, err error)
	// ChangeStatus 状态流转（哪条边合法由状态机判定）。
	ChangeStatus(ctx context.Context, req *orderdto.ChangeStatusReq) (err error)
	// PayOrder 支付落账：pending 推进到 paid，写 paid_at / 支付通道 / 支付流水号。
	//
	// 幂等：已经是 paid / shipped / completed 时原样返回成功且**不改任何列** ——
	// 网关重复通知、访客连点两次、失败重试都会走到这条路径上，
	// 把重复当错误会让对方无限重试。
	PayOrder(ctx context.Context, req *orderdto.PayOrderReq) (res *orderdto.PayOrderResp, err error)
	// CancelOrder 取消订单：改状态 + 记流转 + **同一事务内**归还库存 + 释放券核销。
	//
	// 全有或全无：任一步（含库存归还）失败则整体回滚，订单仍是原状态。
	// 不再有「状态已取消、库存没回来 → 返回 Warnings 留痕给人工」这条半截路径 ——
	// 那正是 AGENTS.md 禁止的跨模块补偿（同库跨模块必须事务透传）。
	CancelOrder(ctx context.Context, req *orderdto.CancelOrderReq) (res *orderdto.CancelOrderResp, err error)
	// UpdateOrderNote 改订单的后台备注（adminNote）。
	//
	// 备注不是状态流转：它不改变订单处在哪一步，因此不写 status_logs ——
	// 混进流转链会让「这单什么时候发的货」变成要翻记录才能看出来。
	UpdateOrderNote(ctx context.Context, req *orderdto.UpdateOrderNoteReq) (res *orderdto.OrderResp, err error)
	// RefundOrder 退款：改状态 + 记流水号；**未发货时归还库存**（BIZ-07 方案 A）。
	//
	// 「退钱」与「退货」是两件事：退款不强制退货（只退运费、质量问题直接退款不退货
	// 都是正常诉求），所以这条路径仍然不碰退货单。
	//
	// 但**未发货（paid）**的订单是例外：货从未出库，钱退了货自然还在仓库里 ——
	// 同一状态上「取消」把货还回去、「退款」不还，两个按钮给出不同的库存结果，
	// 运营点哪个按钮决定了库存对不对，而页面上看不出区别。所以未发货退款按
	// 「订购数量 − 已实际退货入库数量」归还库存（与取消同一本账）。
	//
	// 已发货（shipped / completed）的退款仍然**不归还**：货已经出库，
	// 要回来必须经退货入库（有实物验收环节），凭空加回来等于把系统里的数当成仓库里的货。
	RefundOrder(ctx context.Context, req *orderdto.RefundOrderReq) (err error)
}

// VisitorReturnPort 访客侧的退货能力（访问面片段层消费的**收窄面**）。
//
// 为什么单独成接口：片段层不该拿到「后台审核 / 入库 / 退款」那几条 ——
// 越权防护靠接口形状，而不是靠调用方自觉。三条方法都以 userID 收口，
// 调用方没有「不传身份」这个选项。
type VisitorReturnPort interface {
	// RequestReturn 提交退货申请（UserID 由片段层从会话写入）。
	RequestReturn(ctx context.Context, req *orderdto.ReturnRequestReq) (res *orderdto.ReturnResp, err error)
	// ReturnableOfOrder 该订单各订单项的当前可退数量（渲染表单用）。
	ReturnableOfOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.ReturnableResp, err error)
	// ListVisitorReturns 访客查自己的退货申请。
	ListVisitorReturns(ctx context.Context, req *orderdto.VisitorReturnListReq) (res *orderdto.ReturnListResp, err error)
}

// ReturnService 退货申请（RMA）：客户申请 → 管理员审核 → **先入库、后退款**。
//
// 与 CancelOrder / RefundOrder 的分工：
//
//	· CancelOrder 是「货还没出去」，它直接归还库存；
//	· RefundOrder 只处理钱（契约里明写「不归还库存」）；
//	· 本接口把两者串起来，**但顺序不能反**：先入库（货真的回来了）再退款 ——
//	  反过来就是「钱退了、货没回来」，而这正是退货流程最容易被薅的地方。
//
// 部分退货是一等公民：按订单项记数量，「已退多少」由明细聚合算出（不存冗余计数 ——
// 冗余计数总有一天会与明细对不上，而对不上的时候没人知道该信哪一个）。
type ReturnService interface {
	// VisitorReturnPort 客户侧的三条（片段层只拿得到这些）。
	VisitorReturnPort

	// CancelReturn 客户撤销自己**尚未审核**的申请。
	CancelReturn(ctx context.Context, req *orderdto.ReturnCancelReq) (err error)

	// ListReturns 后台列表（含各状态计数）。
	ListReturns(ctx context.Context, req *orderdto.ReturnListReq) (res *orderdto.ReturnListResp, err error)
	// GetReturn 详情（含订单摘要与逐行**可退数量**）。
	GetReturn(ctx context.Context, returnID uint64) (res *orderdto.ReturnDetailResp, err error)
	// ApproveReturn 同意（AutoReceive=true 时一步完成入库 + 退款）。
	ApproveReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error)
	// RejectReturn 拒绝（必须给理由 —— 客户要知道为什么，否则他会再申请一次）。
	RejectReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error)
	// ReceiveReturn 确认收货：入库 + 退款。
	//
	// 幂等且**可重入**：入库成功后网络断了、再点一次只会补做没完成的那一步
	//（入库里有唯一来源引用与状态守卫，退款走幂等的 PayOrder/RefundOrder 口径）。
	ReceiveReturn(ctx context.Context, req *orderdto.ReturnReceiveReq) (res *orderdto.ReturnResp, err error)
}

// CustomerOrderSummaryReader 按客户取订单聚合事实（只读，一条方法）。
//
// 后台客户管理页要用它 —— 而客户页需要的东西只有一件：这个客户在本工程里
// 下过几单、累计消费多少、最近一单是什么时候。所以它既不是 ListOrders
// （那会顺带给出全站状态计数与客户列表），也不是任何写能力。
// 与 VisitorOrderReader 同一条思路：越权防护靠接口形状，不靠调用方自觉。
type CustomerOrderSummaryReader interface {
	// CustomerOrderSummaryOf 累计口径（哪些状态算消费）由订单模块决定，
	// 调用方只拿到结论，不参与计算。
	CustomerOrderSummaryOf(ctx context.Context, req *orderdto.CustomerOrderSummaryReq) (res *orderdto.CustomerOrderSummaryResp, err error)
}

// CustomerGrowthReader 按「工程 + 时间区间」取客户增长事实（只读，一条方法）。
//
// 与 CustomerOrderSummaryReader 的分工：那个回答「**这个**客户下过几单、花了多少」，
// 本接口回答「**这段时间**来了多少新客、多少人回来复购」。前者按客户查、后者按区间查，
// 合成一个接口只会让两个调用方各自拿到一半用不上的东西。
//
// 口径（谁是新人、什么算复购）出自 docs/17 §4.4，实现落在订单模块 ——
// 客户模块手里只有账号（注册时间、状态），读不到订单表。
type CustomerGrowthReader interface {
	// CustomerGrowthByRange 复购率、新客与回头客的划分都由订单模块算好，
	// 调用方拿到的是结论（含展示用串），不重算任何比例。
	CustomerGrowthByRange(ctx context.Context, req *orderdto.CustomerGrowthReq) (res *orderdto.CustomerGrowthResp, err error)
}

// CustomerSegmentReader 按「工程 + 区间 + 分段」取客户 id 列表（只读，一条方法）。
//
// 与 CustomerGrowthReader 的分工：那个回答「有多少人」（概览页要的数字），
// 这个回答「是哪些人」（列表页要的行）。两者在订单模块内共用同一段 SQL，
// 所以「概览说 12、列表筛出 13」这种自相矛盾不会有地方长出来。
//
// 返回的是 id 而不是客户行：客户行归客户模块（表归属），这里只交出「谁在这段里」。
type CustomerSegmentReader interface {
	// CustomerSegmentIDsByRange 分段名取白名单外的值一律报错，不静默回落到「全部」——
	// 静默回落会让 UI 上一个拼错的分段显示成「全部客户」，而看起来是对的。
	CustomerSegmentIDsByRange(ctx context.Context, req *orderdto.CustomerSegmentIDsReq) (res *orderdto.CustomerSegmentIDsResp, err error)
}

// OrderRangeSummaryReader 按「工程 + 时间区间」取订单聚合事实（只读，一条方法）。
//
// 与 CustomerOrderSummaryReader 同一条思路：越权防护靠接口形状 —— 工程与区间都必填，
// 调用方没有「忘了传」的选项。它比 ListOrders 更适合只想要数字的调用方
// （概览页 KPI、只读聚合）：后者会顺带给出全站状态计数与一整页列表。
type OrderRangeSummaryReader interface {
	// SummaryByRange 区间口径（哪些状态算消费、时间窗怎么取）由订单模块决定，
	// 调用方只拿到结论，不参与计算。
	SummaryByRange(ctx context.Context, req *orderdto.OrderRangeSummaryReq) (res *orderdto.OrderRangeSummaryResp, err error)
}

// OrderOverviewReader 概览页的其余只读聚合（趋势 / 榜单 / 状态计数）。
//
// 三个方法与 OrderRangeSummaryReader 分开成两个接口而不是合成一个：它们回答的问题不同 ——
// 区间摘要是「这段时间一共多少」，本接口是「每天各多少」「卖得最好的是谁」「现在有多少单等着处理」。
// 合成一个会让只想画趋势的调用方（将来 analytics 或报表）也被迫依赖榜单与状态计数。
//
// 越权防护同样靠接口形状：趋势与榜单都强制「工程 + 区间」，状态计数强制工程。
type OrderOverviewReader interface {
	// DailySeries 区间内逐日连续的订单数据（空天已补零）。
	DailySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error)
	// TopProducts 区间内销量最高的若干商品（含名次，已按销量降序）。
	TopProducts(ctx context.Context, req *orderdto.OrderTopProductsReq) (res *orderdto.OrderTopProductsResp, err error)
	// StatusCounts 各状态的订单条数与几个已解释过的口径（待付款 / 待发货 / 总数）。
	StatusCounts(ctx context.Context, req *orderdto.OrderStatusCountsReq) (res *orderdto.OrderStatusCountsResp, err error)
	// SoldQuantityByRange 区间内售出的商品总件数与贡献订单数。
	//
	// 与 TopProducts 同一个筛选条件（只算计入消费的订单）：一个回答「一共卖了多少件」，
	// 一个回答「哪些商品卖得多」，两者必须自洽。
	SoldQuantityByRange(ctx context.Context, req *orderdto.OrderSoldQuantityReq) (res *orderdto.OrderSoldQuantityResp, err error)
}

// OrderNoReader 按商户单号取订单。
//
// 支付通道的异步回调只有商户单号（它不认识我们的自增 id），而 PayOrder 只接受内部 id ——
// 这是刻意的（见 PayOrderReq 注释），两者之间需要这一层翻译。
type OrderNoReader interface {
	GetOrderByNo(ctx context.Context, req *orderdto.GetOrderByNoReq) (res *orderdto.OrderResp, err error)
}

// VisitorOrderReader 访客自助查询自己订单的能力。
//
// 为什么不让访客直接调 ListOrders：那条路径返回全站状态计数、且 UserID 只是个可选过滤项 ——
// 「必须限定在自己名下」这件事交给调用方记得传，总有一天会有人忘。
// 这里两个方法把 user_id 钉进契约：调用方没有不传的选项，归属过滤写在 SQL 条件里。
type VisitorOrderReader interface {
	ListVisitorOrders(ctx context.Context, req *orderdto.VisitorOrderListReq) (res *orderdto.VisitorOrderListResp, err error)
	GetVisitorOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.OrderDetailResp, err error)
}

// CouponService 优惠码能力。
//
// 试算（ValidateCoupon）是纯读、不占次数，供结算页在提交前先告诉访客能减多少；
// 真正的核销发生在 CreateOrder 的事务里（见 CreateOrderReq.CouponCode），**不单独暴露核销入口** ——
// 允许「先核销、后建单」的接口一定会被用出「券没了但没下单」这种状态。
type CouponService interface {
	CreateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error)
	UpdateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error)
	ListCoupons(ctx context.Context, req *orderdto.CouponListReq) (res *orderdto.CouponListResp, err error)
	GetCoupon(ctx context.Context, couponID uint64) (res *orderdto.CouponResp, err error)
	DeleteCoupon(ctx context.Context, couponID uint64) (err error)
	ValidateCoupon(ctx context.Context, req *orderdto.CouponValidateReq) (res *orderdto.CouponValidateResp, err error)
	ListCouponRedemptions(ctx context.Context, req *orderdto.CouponRedemptionListReq) (res *orderdto.CouponRedemptionListResp, err error)
	// AuditCouponCounts 对账 coupons.used_count 与核销明细行数（DB-021）。
	//
	// 只读、不修正：used_count 是并发守卫（`WHERE used_count < max_uses`）依赖的投影，
	// 明细才是真源，偏差该往哪边修取决于原因（手工改库 / 早期逻辑缺口 / 守卫未命中），
	// 自动修可能把真源也改错。
	AuditCouponCounts(ctx context.Context, req *orderdto.CouponCountAuditReq) (res *orderdto.CouponCountAuditResp, err error)
}

// ==========================================================================
// 跨模块形状：调用方要传进来、要收回去的类型
// ==========================================================================

// 跨模块调用方使用的**形状重导出**（与 publication 契约同一手法）：调用方只依赖 contract，
// 不直接 import order/dto 或 order/enums。
//
// 为什么是重导出而不是另造一组「契约自有入参类型」：本模块的 dto 与对外契约形状是同一件事
// （建单入参 / 支付入参 / 订单视图就是它对外的语义），dto 上的 json/form 标签只影响 HTTP 绑定，
// 不改变语义。另造一组形状意味着两份必须逐字段保持等价的定义 —— 那是把「一处改、调用方编译错」
// 换成「一处改、另一处静默分叉」：耦合没有减少，出错面反而变大。
//
// 边界：本模块内部（service / model / inbound）继续用 orderdto 作为实现形状；
// 重导出只服务于跨模块调用方。
type (
	// 建单（cart 结算链路）。
	CreateOrderReq  = orderdto.CreateOrderReq
	CreateOrderResp = orderdto.CreateOrderResp
	OrderItemReq    = orderdto.OrderItemReq
	OrderAddress    = orderdto.OrderAddress
	// 支付落账（cart 结算与支付回调链路）。
	PayOrderReq  = orderdto.PayOrderReq
	PayOrderResp = orderdto.PayOrderResp
	// 按商户单号取单（支付回调链路）。
	GetOrderByNoReq = orderdto.GetOrderByNoReq
	OrderResp       = orderdto.OrderResp
	// 归因与轨迹快照（cart 在下单那一刻从追踪 cookie 定格后交进来）。
	Attribution = orderdto.Attribution
	FirstTouch  = orderdto.FirstTouch
	UTMInfo     = orderdto.UTMInfo
	AdInfo      = orderdto.AdInfo
	SessionInfo = orderdto.SessionInfo
	DeviceInfo  = orderdto.DeviceInfo
	TrailPage   = orderdto.TrailPage

	// 客户维度订单聚合（后台客户管理页的「下过几单 / 累计消费 / 最近一单」）。
	CustomerOrderSummaryReq  = orderdto.CustomerOrderSummaryReq
	CustomerOrderSummaryResp = orderdto.CustomerOrderSummaryResp
)

// ErrOrderNotFound 订单不存在（错误文案取自 enums，供调用方做错误判定而不 import enums）。
const ErrOrderNotFound = orderenums.ErrOrderNotFound

// OrderStatusLabel 订单状态 → (词条 key, 中文兜底)，真源在 order/enums。
//
// 为什么由 contract 转出而不是让调用方自己映射：跨模块只允许依赖 contract 与不可变 dto，
// 而订单状态 → 展示名的映射必须与订单页**共用同一份** —— 客户详情页的「最近一单」各写一张
// 中文表的结果是「改一处、另一处静默留在旧说法上」（不报错、测试也不红）。
// 形态与其它展示标签一致：调用点 tr(key, fallback)，词条缺失时回落中文。
func OrderStatusLabel(status string) (key, fallback string) {
	return orderenums.OrderStatusLabel(status)
}

// ==========================================================================
// 索要的端口：订单需要外部给什么（由对方实现）
// ==========================================================================

// StockOperator 订单需要的库存能力 —— **只有扣减与增加这两条**。
//
// 为什么不直接依赖 inventorycontract.InventoryService：那个接口有二十来个方法
// （仓库 / 库存查询 / 流水 / 原因字典 / 物料清单 / 采购单 / 入库 / 进货历史），
// 订单一条都用不上。收窄的理由同 user 模块的 MailSender：依赖面越大，越容易在
// 不经意间用上不该用的能力；测试造替身时，二十个方法的空实现也会淹没测试意图。
//
// 入参用本契约自有类型（StockDeduction / StockAdjustment + StockLine），
// **不借用 inventory 的 dto**：订单要表达的是「这单出哪几个 SKU、各多少件、什么原因」，
// 而 inventory 的 dto 里还带着它自己的绑定细节（skuCode 冗余列、expandBom 开关、
// operatorId、direction 字符串）。借用那套形状等于让订单认识库存模块的绑定层 ——
// 库存改一次请求字段，订单跟着编译错（审计 CQ-004）。
//
// 由库存模块提供适配实现（见 product/inventory/outbound/orderstock）：
// 实现方依赖调用方契约，方向不会反过来。
type StockOperator interface {
	// DeductStock 建单出库：任一行可用量不足即整体拒绝（不会扣一半）。
	//
	// 不返回批次号等内部标识 —— 订单侧从不使用它（原来就在丢弃），
	// 暴露一个没人看的返回值只会诱使调用方把它当业务引用存下来。
	DeductStock(ctx context.Context, in *StockDeduction) (err error)
	// ChangeStock 把货加回库存（取消订单归还 / 退货入库）。
	//
	// 方向写死在类型里（恒为入库）：订单域没有任何「把货减掉」的场景 ——
	// 出库走 DeductStock。让调用方能传任意方向，等于允许它绕过扣减的可用量守卫。
	ChangeStock(ctx context.Context, in *StockAdjustment) (err error)

	// DeductStockTx / ChangeStockTx —— **事务透传版**：与上面两条同语义，但在**调用方
	// 的事务**里执行（tx 非 nil，由订单侧负责提交 / 回滚）。
	//
	// 为什么必须有它们：库存与订单在同一个库，一次用户可感知的写操作（建单 / 取消 /
	// 退货入库）里任何一步失败都必须整体回滚。原先「订单事务先提交 → 再动库存 →
	// 失败再补偿」会在补偿也失败时留下「有单没扣库存 / 已取消没归还库存」，而错误还被
	// 吞掉。跨模块只传句柄（先例 masterdata.RecordChangesTx），对端不再自己开事务。
	//
	// 约定：不自己开事务；错误原样返回（含库存不足），由调用方决定呈现方式。
	DeductStockTx(ctx context.Context, tx *gorm.DB, in *StockDeduction) (err error)
	ChangeStockTx(ctx context.Context, tx *gorm.DB, in *StockAdjustment) (err error)
}

// StockLine 一次库存变动里的一行：动哪个 SKU、动多少件。
//
// 字段就是订单侧真正掌握的事实。**不含仓库**是最常见的情形 ——
// 订单不知道货在哪个仓，也不该知道（那是库存域的事实）；
// 唯一的例外是退货入库，货该回哪个仓由客户或运营指定，故留 WarehouseID 可选口。
type StockLine struct {
	// ProductID 商品标识（库存流水按它归类）。
	ProductID string
	// VariantID 变体标识（库存真源的维度是 SKU × 仓库）。
	VariantID string
	// SKUCode SKU 编码（流水留痕用；为空时由库存域按变体补齐）。
	SKUCode string
	// Quantity 件数（正整数）。
	Quantity int
	// WarehouseID 指定仓库；为空表示按该 SKU 的归属仓由库存域解析。
	WarehouseID string
}

// StockDeduction 建单出库的入参。
type StockDeduction struct {
	ProjectID  string
	ReasonCode string
	// SourceType / SourceRef 来源引用（订单号），供库存流水回溯到这张单。
	SourceType string
	SourceRef  string
	Remark     string
	Lines      []StockLine
}

// StockAdjustment 把货加回库存的入参（方向恒为入库）。
type StockAdjustment struct {
	ProjectID  string
	ReasonCode string
	SourceType string
	SourceRef  string
	Remark     string
	Lines      []StockLine
}

// ReturnWarehouseSource 退货页「入库仓库」下拉所需的最窄读能力。
//
// 与 StockOperator 同一手法（CQ-004）：订单侧不 import 库存的 dto ——
// 仓库列表以本契约的自有视图类型（ReturnWarehouse）返回，适配器在库存侧
// （product/inventory/outbound/orderstock）装配。
type ReturnWarehouseSource interface {
	// ListReturnWarehouses 某工程的仓库视图列表（默认仓在最前，由库存侧排序保证）。
	ListReturnWarehouses(ctx context.Context, projectID string) ([]ReturnWarehouse, error)
}

// ReturnWarehouse 退货入库下拉里的一行仓库视图。
type ReturnWarehouse struct {
	// ID 仓库标识（表单提交值）。
	ID string
	// Name 仓库名。
	Name string
	// Code 仓库短码（SKU 编码前缀，运营认它比认全名快）。
	Code string
	// Status 启用状态（enabled / disabled）。
	Status string
	// IsDefault 是否默认仓（留空时的兜底目标）。
	IsDefault bool
}
